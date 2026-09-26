package server_test

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
)

// Seam S1: the key pair and the Authorization header never reach the agent or
// the log, wherever they are planted.

// plantedSecrets are the forms of the key pair a leak could take.
func plantedSecrets() map[string]string {
	b64 := base64.StdEncoding.EncodeToString([]byte(testPublicKey + ":" + testSecretKey))
	return map[string]string{
		"public key":           testPublicKey,
		"secret key":           testSecretKey,
		"Authorization value":  b64,
		"Authorization header": "Basic " + b64,
	}
}

func TestTheKeyPairAndAuthorizationHeaderNeverReachResultsErrorsOrLogs(t *testing.T) {
	t.Parallel()
	for form, secret := range plantedSecrets() {
		quoted, err := json.Marshal(secret)
		if err != nil {
			t.Fatal(err)
		}
		tests := map[string]struct {
			status int
			body   string
			args   map[string]any
		}{
			"in a payload string": {status: http.StatusOK,
				body: `{"data":[{"input":` + string(quoted) + `,"note":"key: ` + strings.Trim(string(quoted), `"`) + `."}]}`,
				args: traceList},
			"in a Langfuse error message": {status: http.StatusBadRequest,
				body: `{"message":"Invalid request data","error":[{"path":["auth"],"message":` + string(quoted) + `}]}`,
				args: traceList},
			"at the cut of a long Langfuse error message": {status: http.StatusBadRequest,
				body: `{"message":"` + strings.Repeat("x", 445) + " " + strings.Trim(string(quoted), `"`) + `"}`,
				args: traceList},
			"in the operationId argument": {status: http.StatusOK, body: `{}`,
				args: map[string]any{"operationId": secret}},
			"in a parameter name": {status: http.StatusOK, body: `{}`,
				args: map[string]any{"operationId": "trace_list", "parameters": map[string]any{secret: "x"}}},
		}
		for where, tc := range tests {
			t.Run(form+" "+where, func(t *testing.T) {
				t.Parallel()
				fake, _ := fakeLangfuse(t, tc.status, tc.body)
				var logs syncBuffer
				cs := connectClient(t, langfuse.New(clientOptions(t, fake)), slog.New(slog.NewJSONHandler(&logs, nil)))

				res := callExecuteRead(t, cs, tc.args)

				structured, err := json.Marshal(res.StructuredContent)
				if err != nil {
					t.Fatal(err)
				}
				for channel, text := range map[string]string{
					"result text": resultText(t, res), "structuredContent": string(structured), "log": logs.String(),
				} {
					if leak, ok := partOf(secret, text); ok {
						t.Errorf("the %s carries %q of the %s:\n%s", channel, leak, form, text)
					}
				}
				if !strings.Contains(resultText(t, res), "[REDACTED]") {
					t.Errorf("the result does not mark where the %s was: %s", form, resultText(t, res))
				}
			})
		}
	}
}

// partOf returns a run of 16 bytes of secret found in text, so that a secret
// cut short by truncation still counts as a leak.
func partOf(secret, text string) (string, bool) {
	const run = 16
	for i := 0; i+run <= len(secret); i++ {
		if strings.Contains(text, secret[i:i+run]) {
			return secret[i : i+run], true
		}
	}
	return "", false
}
