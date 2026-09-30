//go:build integration

package server_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

// Seam S3 (spec #44, ticket #33): a Folder name reaches a real Langfuse as
// one %2F-encoded path segment and names the object created under it. The
// prompt and the dataset are created straight through Langfuse (writes are
// test setup); the dataset runs routes are not asserted: they fail upstream
// for Folder names (langfuse/langfuse#13933; see docs/reference/tools.md).

func TestLiveLangfuseServesAPromptReadByItsFolderName(t *testing.T) {
	t.Parallel()
	cs := liveSession(t)
	name := "it-folder-" + randomHex(t, 4) + "/triage/system"
	status, body := langfuseDirect(t.Context(), t, http.MethodPost, "/api/public/v2/prompts", map[string]any{
		"name": name, "type": "text", "prompt": "Classify the ticket.", "labels": []string{"production"},
	})
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("create prompt %q: HTTP %d %s", name, status, body)
	}
	t.Cleanup(func() {
		// t.Context() is already cancelled when cleanups run; a failed delete
		// is reported only: the name is unique to this run.
		status, body := langfuseDirect(context.WithoutCancel(t.Context()), t, http.MethodDelete,
			"/api/public/v2/prompts/"+url.PathEscape(name), nil)
		t.Logf("cleanup: DELETE prompt %q → %d %s", name, status, body)
	})

	var prompt struct {
		Name   string `json:"name"`
		Prompt string `json:"prompt"`
	}
	liveData(t, readLive(t, cs, map[string]any{
		"operationId": "prompts_get", "parameters": map[string]any{"promptName": name},
	}, sleepCtx), &prompt)

	if prompt.Name != name || prompt.Prompt != "Classify the ticket." {
		t.Fatalf("prompts_get %q = %+v, want the prompt created under that Folder name", name, prompt)
	}
}

func TestLiveLangfuseServesADatasetReadByItsFolderName(t *testing.T) {
	t.Parallel()
	cs := liveSession(t)
	// The public API has no dataset delete: the dataset stays, under a name
	// unique to this run, in the throwaway or test project.
	name := "it-folder-" + randomHex(t, 4) + "/qa-dataset"
	status, body := langfuseDirect(t.Context(), t, http.MethodPost, "/api/public/v2/datasets", map[string]any{"name": name})
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("create dataset %q: HTTP %d %s", name, status, body)
	}

	var dataset struct {
		Name string `json:"name"`
	}
	liveData(t, readLive(t, cs, map[string]any{
		"operationId": "datasets_get", "parameters": map[string]any{"datasetName": name},
	}, sleepCtx), &dataset)

	if dataset.Name != name {
		t.Fatalf("datasets_get %q = %+v, want the dataset created under that Folder name", name, dataset)
	}
}
