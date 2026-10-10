//go:build integration

package server_test

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/url"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
)

// Seam S3 (spec #44, ticket #33; spec #172, ticket #181): a Folder name
// reaches a real Langfuse as one %2F-encoded path segment and names the
// object created under it, the runs of a dataset included. The prompt, the
// dataset, its item and its run are created straight through Langfuse (writes
// are test setup).

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

// createdObject is the one field of a Langfuse create answer the live tests
// read back.
type createdObject struct {
	ID string `json:"id"`
}

// createLive creates one object straight through Langfuse (test setup) and
// returns its ID, failing the test on any status but 200 or 201.
func createLive(t *testing.T, path string, body map[string]any) string {
	t.Helper()
	status, text := langfuseDirect(t.Context(), t, http.MethodPost, path, body)
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("POST %s: HTTP %d %s", path, status, text)
	}
	var created createdObject
	if err := json.Unmarshal([]byte(text), &created); err != nil || created.ID == "" {
		t.Fatalf("POST %s answered no ID: %v %s", path, err, text)
	}
	return created.ID
}

// datasetRun is the part of a dataset run, as getRuns lists it and getRun
// returns it, that says which dataset it belongs to.
type datasetRun struct {
	Name           string `json:"name"`
	DatasetID      string `json:"datasetId"`
	DatasetName    string `json:"datasetName"`
	DatasetRunItem []struct {
		DatasetItemID string `json:"datasetItemId"`
	} `json:"datasetRunItems"`
}

// runItemReadBackTimeout bounds how long the test waits for Langfuse to list
// a run item it accepted: later 3.x and 4.x releases ingest run items
// asynchronously (about 6 s on 3.225.11, for a plain dataset name too).
const runItemReadBackTimeout = 90 * time.Second

// datasetRunsServed reports whether the pinned deployment the live Langfuse is
// serves the dataset runs routes, which are in the legacy family. It skips the
// test when the live Langfuse is no pinned deployment (the Cloud test project
// serves the family only until 2026-11-16 and is not written to for this).
func datasetRunsServed(t *testing.T) bool {
	t.Helper()
	deployment := os.Getenv(envTestDeployment)
	if deployment == "" {
		t.Skipf("%s not set: the dataset runs routes are checked on the pinned deployments only", envTestDeployment)
	}
	pin, ok := pinnedDeployments[deployment]
	if !ok {
		t.Fatalf("%s names no pinned deployment; the pins are %v", envTestDeployment, slices.Sorted(maps.Keys(pinnedDeployments)))
	}
	return slices.Contains(pin.Families, catalog.LegacyFamily)
}

// seededRun is a dataset with a Folder name that holds one item and one run.
type seededRun struct{ datasetName, runName, datasetID, itemID string }

// seedRunOnAFolderDataset creates the dataset, its item and its run straight
// through Langfuse (test setup). The public API has no dataset delete: they
// stay, under names unique to this run, in the throwaway project.
func seedRunOnAFolderDataset(t *testing.T) seededRun {
	t.Helper()
	id := randomHex(t, 4)
	s := seededRun{datasetName: "it-folder-" + id + "/runs-dataset", runName: "it-run-" + id}
	s.datasetID = createLive(t, "/api/public/v2/datasets", map[string]any{"name": s.datasetName})
	s.itemID = createLive(t, "/api/public/dataset-items", map[string]any{"datasetName": s.datasetName, "input": map[string]any{"q": id}})
	createLive(t, "/api/public/dataset-run-items", map[string]any{
		"runName": s.runName, "datasetItemId": s.itemID, "traceId": randomHex(t, 16),
	})
	return s
}

// deleteRunRoute is the path of the route datasets_getRun and
// datasets_deleteRun share, with the Folder name encoded as the server sends it.
func (s seededRun) deleteRunRoute() string {
	return "/api/public/datasets/" + url.PathEscape(s.datasetName) + "/runs/" + url.PathEscape(s.runName)
}

// The dataset runs routes with a Folder name (spec #172, ticket #181, #49):
// where a deployment serves them, the runs that come back are the runs of the
// dataset the name asked for. The statement that they fail upstream
// (langfuse/langfuse#13933) rested on one report, not on a deployment.
func TestLiveLangfuseServesTheRunsOfADatasetReadByItsFolderName(t *testing.T) {
	t.Parallel()
	if !datasetRunsServed(t) {
		t.Skipf("%s turns the legacy family off: the dataset runs routes answer operation_unavailable "+
			"(TestLiveLangfuseAnswersOperationUnavailableOnTheDatasetRunsRoutesWhereTheLegacyFamilyIsOff)",
			os.Getenv(envTestDeployment))
	}
	cs := liveSession(t)
	seed := seedRunOnAFolderDataset(t)

	t.Run("datasets_getRuns", func(t *testing.T) {
		var runs struct {
			Data []datasetRun `json:"data"`
		}
		liveData(t, readLive(t, cs, map[string]any{
			"operationId": "datasets_getRuns", "parameters": map[string]any{"datasetName": seed.datasetName},
		}, sleepCtx), &runs)

		if len(runs.Data) != 1 || runs.Data[0].Name != seed.runName ||
			runs.Data[0].DatasetID != seed.datasetID || runs.Data[0].DatasetName != seed.datasetName {
			t.Fatalf("datasets_getRuns %q = %+v, want the one run %q of dataset %q (%s)",
				seed.datasetName, runs.Data, seed.runName, seed.datasetName, seed.datasetID)
		}
	})

	t.Run("datasets_getRun", func(t *testing.T) {
		var run datasetRun
		for deadline := time.Now().Add(runItemReadBackTimeout); ; {
			run = datasetRun{}
			liveData(t, readLive(t, cs, map[string]any{
				"operationId": "datasets_getRun", "parameters": map[string]any{"datasetName": seed.datasetName, "runName": seed.runName},
			}, sleepCtx), &run)
			if len(run.DatasetRunItem) > 0 || time.Now().After(deadline) {
				break
			}
			if err := sleepCtx(t.Context(), 2*time.Second); err != nil {
				t.Fatalf("waiting for the run item: %v", err)
			}
		}

		if run.Name != seed.runName || run.DatasetID != seed.datasetID || run.DatasetName != seed.datasetName ||
			len(run.DatasetRunItem) != 1 || run.DatasetRunItem[0].DatasetItemID != seed.itemID {
			t.Fatalf("datasets_getRun %q %q = %+v, want run %q of dataset %q (%s) holding item %s",
				seed.datasetName, seed.runName, run, seed.runName, seed.datasetName, seed.datasetID, seed.itemID)
		}
	})

	// datasets_deleteRun shares the route of datasets_getRun. Writes through
	// execute_write run on 4.46.0 only (write_integration_test.go), so this
	// sends the DELETE straight, as the server would, to learn whether the
	// route takes a Folder name; it also removes the run. It runs last.
	t.Run("datasets_deleteRun route", func(t *testing.T) {
		status, body := langfuseDirect(t.Context(), t, http.MethodDelete, seed.deleteRunRoute(), nil)
		if status != http.StatusOK {
			t.Fatalf("DELETE the run %q of dataset %q: HTTP %d %s, want 200", seed.runName, seed.datasetName, status, body)
		}
	})
}

// Where Langfuse turns the legacy family off (v4 events_only), the dataset runs
// routes answer 404 "not available in events_only mode" whatever the dataset
// is called: the executor says operation_unavailable, which is no Folder-name
// failure. The server offers neither operation on such a deployment (#174).
func TestLiveLangfuseAnswersOperationUnavailableOnTheDatasetRunsRoutesWhereTheLegacyFamilyIsOff(t *testing.T) {
	t.Parallel()
	if datasetRunsServed(t) {
		t.Skipf("%s serves the dataset runs routes (TestLiveLangfuseServesTheRunsOfADatasetReadByItsFolderName)",
			os.Getenv(envTestDeployment))
	}
	cs := liveSession(t)
	seed := seedRunOnAFolderDataset(t)

	for operationID, parameters := range map[string]map[string]any{
		"datasets_getRuns": {"datasetName": seed.datasetName},
		"datasets_getRun":  {"datasetName": seed.datasetName, "runName": seed.runName},
	} {
		t.Run(operationID, func(t *testing.T) {
			got := toolErrorOf(t, readLive(t, cs, map[string]any{"operationId": operationID, "parameters": parameters}, sleepCtx)).Error

			if got.Code != "operation_unavailable" || got.HTTPStatus != http.StatusNotFound {
				t.Fatalf("%s = %+v, want operation_unavailable from a 404", operationID, got)
			}
		})
	}

	t.Run("datasets_deleteRun route", func(t *testing.T) {
		status, body := langfuseDirect(t.Context(), t, http.MethodDelete, seed.deleteRunRoute(), nil)
		if status != http.StatusNotFound {
			t.Fatalf("DELETE the run %q of dataset %q: HTTP %d %s, want 404", seed.runName, seed.datasetName, status, body)
		}
	})
}
