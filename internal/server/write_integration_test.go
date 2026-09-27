//go:build integration

package server_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/server"
)

// Spec #109, ticket #110: a live write cycle through execute_write, on a
// pinned self-hosted deployment only (#110, #113). The Langfuse Cloud test project has no
// pin (LANGFUSE_TEST_DEPLOYMENT unset) and is never written to: its Hobby
// limits and shared data are not the suite's to spend.

// scoreReadBackTimeout bounds how long the test waits for Langfuse to serve a
// score it accepted: scores are ingested asynchronously.
const scoreReadBackTimeout = 90 * time.Second

func TestLiveExecuteWriteCreatesAScoreThatReadsBack(t *testing.T) {
	t.Parallel()
	if os.Getenv(envTestDeployment) == "" {
		t.Skipf("%s not set: writes run against a pinned self-hosted deployment only, never Langfuse Cloud",
			envTestDeployment)
	}
	client, keys := liveClient(t, langfuse.Options{RateLimit: liveRateLimit})
	cs := connectServer(t, client, slog.New(slog.DiscardHandler), keys, server.WithWriteMode())
	run := randomHex(t, 8)
	scoreID, traceID := "it-score-"+run, "it-trace-"+run
	t.Cleanup(func() {
		// t.Context() is already cancelled when cleanups run.
		status, body := langfuseDirect(context.WithoutCancel(t.Context()), t, http.MethodDelete,
			"/api/public/scores/"+scoreID, nil)
		t.Logf("cleanup: DELETE /api/public/scores/%s → %d %s", scoreID, status, body)
	})

	var created struct {
		ID string `json:"id"`
	}
	liveData(t, callExecuteWrite(t, cs, map[string]any{"operationId": "scores_create", "body": map[string]any{
		"id": scoreID, "traceId": traceID, "name": "it-execute-write", "value": 0.5, "dataType": "NUMERIC",
		"comment": "created by the integration suite through execute_write",
	}}), &created)
	if created.ID != scoreID {
		t.Fatalf("scores_create answered id %q, want %q", created.ID, scoreID)
	}

	deadline := time.Now().Add(scoreReadBackTimeout)
	for {
		if score, ok := readScoreBack(t, cs, scoreID); ok {
			if score.ID != scoreID || score.Name != "it-execute-write" || score.Value != 0.5 {
				t.Fatalf("score read back = %+v, want id %s, name it-execute-write, value 0.5", score, scoreID)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("score %s was not served within %v of its creation", scoreID, scoreReadBackTimeout)
		}
		if err := sleepCtx(t.Context(), 2*time.Second); err != nil {
			t.Fatalf("waiting for the score: %v", err)
		}
	}
}

// liveScore is the part of a score the write cycle checks; the shape of its
// trace reference differs between score versions.
type liveScore struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

// readScoreBack reads the score with the given ID through execute_read: by ID
// where the deployment serves the legacy family, else through scores v3.
func readScoreBack(t *testing.T, cs *mcp.ClientSession, id string) (liveScore, bool) {
	t.Helper()
	res := readLive(t, cs, map[string]any{"operationId": "scores_get-by-id", "parameters": map[string]any{"scoreId": id}}, sleepCtx)
	if !res.IsError {
		var score liveScore
		liveData(t, res, &score)
		return score, true
	}
	switch code := toolErrorOf(t, res).Error.Code; code {
	case "langfuse_not_found":
		return liveScore{}, false
	case "operation_unavailable":
	default:
		t.Fatalf("scores_get-by-id answered %s: %s", code, resultText(t, res))
	}
	var page struct {
		Data []liveScore `json:"data"`
	}
	liveData(t, readLive(t, cs, map[string]any{"operationId": "scoresV3_getManyV3", "parameters": map[string]any{"id": id}}, sleepCtx), &page)
	if len(page.Data) == 0 {
		return liveScore{}, false
	}
	return page.Data[0], true
}

// Ticket #113: a destructive write runs live only after the user confirms it.
// The prompt lives in a folder, so the write Folder-name entries are proven
// on a real Langfuse too. The client's user accepts every confirmation.
func TestLiveExecuteWriteCreatesRelabelsAndDeletesAPromptWithTheUsersConfirmation(t *testing.T) {
	t.Parallel()
	if os.Getenv(envTestDeployment) == "" {
		t.Skipf("%s not set: writes run against a pinned self-hosted deployment only, never Langfuse Cloud",
			envTestDeployment)
	}
	client, keys := liveClient(t, langfuse.Options{RateLimit: liveRateLimit})
	answers, u := answering("accept")
	cs := startConfirm(t, client, keys, confirmSetup{client: answers})
	name := "it-write-" + randomHex(t, 4) + "/cycle"
	t.Cleanup(func() { deletePromptDirect(t, name) }) // after a failure only: the cycle deletes it

	var created livePrompt
	liveData(t, callExecuteWrite(t, cs, map[string]any{"operationId": "prompts_create", "body": map[string]any{
		"name": name, "type": "text", "prompt": "Summarize the trace.", "labels": []any{"staging"},
	}}), &created)
	if created.Name != name || created.Version != 1 {
		t.Fatalf("prompts_create answered %+v, want %q version 1", created, name)
	}

	var relabelled livePrompt
	liveData(t, callExecuteWrite(t, cs, map[string]any{"operationId": "promptVersion_update",
		"parameters": map[string]any{"name": name, "version": 1},
		"body":       map[string]any{"newLabels": []any{"production"}}}), &relabelled)
	if !slices.Contains(relabelled.Labels, "production") {
		t.Fatalf("promptVersion_update answered labels %v, want production among them", relabelled.Labels)
	}

	if res := callExecuteWrite(t, cs, map[string]any{"operationId": "prompts_delete",
		"parameters": map[string]any{"promptName": name}}); res.IsError {
		t.Fatalf("prompts_delete returned a tool error: %s", resultText(t, res))
	}
	if asked := u.questions(); len(asked) != 2 {
		t.Fatalf("the user was asked %d times, want twice (the PATCH and the DELETE, not the POST)", len(asked))
	}
	res := callExecuteRead(t, cs, map[string]any{"operationId": "prompts_get", "parameters": map[string]any{"promptName": name}})
	if code := toolErrorOf(t, res).Error.Code; code != "langfuse_not_found" {
		t.Fatalf("prompts_get after the delete answered %s, want langfuse_not_found", code)
	}
}

func TestLiveADeclinedDeleteLeavesThePromptInPlace(t *testing.T) {
	t.Parallel()
	if os.Getenv(envTestDeployment) == "" {
		t.Skipf("%s not set: writes run against a pinned self-hosted deployment only, never Langfuse Cloud",
			envTestDeployment)
	}
	client, keys := liveClient(t, langfuse.Options{RateLimit: liveRateLimit})
	answers, _ := answering("decline")
	cs := startConfirm(t, client, keys, confirmSetup{client: answers})
	name := "it-write-" + randomHex(t, 4) + "/kept"
	status, body := langfuseDirect(t.Context(), t, http.MethodPost, "/api/public/v2/prompts", map[string]any{
		"name": name, "type": "text", "prompt": "Keep me.", "labels": []string{"production"},
	})
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("create prompt %q: HTTP %d %s", name, status, body)
	}
	t.Cleanup(func() { deletePromptDirect(t, name) })

	res := callExecuteWrite(t, cs, map[string]any{"operationId": "prompts_delete",
		"parameters": map[string]any{"promptName": name}})

	if code := toolErrorOf(t, res).Error.Code; code != "confirmation_declined" {
		t.Fatalf("prompts_delete answered %s, want confirmation_declined", code)
	}
	var kept livePrompt
	liveData(t, readLive(t, cs, map[string]any{"operationId": "prompts_get",
		"parameters": map[string]any{"promptName": name}}, sleepCtx), &kept)
	if kept.Name != name {
		t.Fatalf("prompts_get after the declined delete = %+v, want the prompt %q still in place", kept, name)
	}
}

// livePrompt is the part of a prompt the write cycle checks.
type livePrompt struct {
	Name    string   `json:"name"`
	Version int      `json:"version"`
	Labels  []string `json:"labels"`
}

// deletePromptDirect deletes every version of the prompt straight through
// Langfuse; a failure (e.g. 404, already deleted) is logged only.
func deletePromptDirect(t *testing.T, name string) {
	t.Helper()
	// t.Context() is already cancelled when cleanups run.
	status, body := langfuseDirect(context.WithoutCancel(t.Context()), t, http.MethodDelete,
		"/api/public/v2/prompts/"+url.PathEscape(name), nil)
	t.Logf("cleanup: DELETE prompt %q → %d %s", name, status, body)
}
