package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/catalog"
	"github.com/rodrigorjsf/langfuse-api-mcp/internal/sanitize"
)

// A destructive operation (DELETE, PUT, PATCH) runs only after the user
// confirms that exact call (ADR-0003 amendment). The server asks through
// form elicitation with a multi round-trip request: the first call gets
// InputRequests and a signed RequestState, and the client re-sends the call
// with the user's answer and the state. For a client on an older protocol the
// SDK's server middleware asks the user directly and re-invokes the handler
// the same way, so there is one code path.

// confirmationTTL is how long a confirmation state stays valid.
const confirmationTTL = 5 * time.Minute

// confirmInputID names the confirmation in InputRequests and InputResponses.
const confirmInputID = "confirm"

// confirmer signs and checks confirmation states. The key is 32 random bytes
// drawn when the server is built: it is never logged and never persisted, so
// a state signed by another server process never verifies.
//
//	state = "<expiryUnix>." + base64url(HMAC-SHA256(key, "<expiryUnix>|<operationId>|" + canonicalArgs))
//
// A state is valid iff it is well formed, now <= expiry, and its MAC equals
// the one recomputed over the call being made (hmac.Equal).
type confirmer struct {
	key []byte
	now func() time.Time
}

// newConfirmer returns a confirmer with a fresh random key; now is the clock.
func newConfirmer(now func() time.Time) confirmer {
	key := make([]byte, 32)
	_, _ = rand.Read(key) // crypto/rand.Read never returns an error; it crashes the process instead
	return confirmer{key: key, now: now}
}

// sign returns the state binding operationID and canonical args until the
// confirmation expires.
func (c confirmer) sign(operationID string, canonical []byte) string {
	expiry := strconv.FormatInt(c.now().Add(confirmationTTL).Unix(), 10)
	return expiry + "." + base64.RawURLEncoding.EncodeToString(c.mac(expiry, operationID, canonical))
}

// valid reports whether state is a state this confirmer signed for exactly
// operationID and canonical args, and has not expired.
func (c confirmer) valid(operationID string, canonical []byte, state string) bool {
	expiry, sig, ok := strings.Cut(state, ".")
	if !ok || expiry == "" || strings.TrimLeft(expiry, "0123456789") != "" {
		return false
	}
	at, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil || c.now().Unix() > at {
		return false
	}
	given, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return false
	}
	return hmac.Equal(c.mac(expiry, operationID, canonical), given)
}

func (c confirmer) mac(expiry, operationID string, canonical []byte) []byte {
	m := hmac.New(sha256.New, c.key)
	m.Write([]byte(expiry + "|" + operationID + "|"))
	m.Write(canonical)
	return m.Sum(nil)
}

// canonicalArgs re-encodes the validated call deterministically: the escaped
// path (path parameters included), the query with sorted names, and the body
// with sorted keys. It is built from the request about to be sent, never from
// the raw input bytes, so what is confirmed is what is sent.
func canonicalArgs(request catalog.Request, body json.RawMessage) []byte {
	var decoded any
	if len(body) > 0 {
		decoded, _ = sanitize.Decode(body) // body passed CheckBody: it is valid JSON; numbers keep their literal text
	}
	// Maps marshal with sorted keys. The error is ignored: strings, a
	// url.Values and a decoded JSON value always marshal.
	out, _ := json.Marshal(struct {
		Path  string     `json:"path"`
		Query url.Values `json:"query"`
		Body  any        `json:"body"`
	}{request.Path, request.Query, decoded})
	return out
}

// confirmationOutcome is the confirmation outcome of a write call, as its
// audit line records it; "" until the call's arguments passed their checks.
type confirmationOutcome string

const (
	// confirmationNotRequired: the operation is not destructive.
	confirmationNotRequired confirmationOutcome = "not_required"
	// confirmationUnavailable: the operation is destructive and the client
	// cannot ask the user, so it was refused.
	confirmationUnavailable confirmationOutcome = "unavailable"
	// confirmationRequested: the call returned the confirmation question.
	confirmationRequested confirmationOutcome = "requested"
	// confirmationAccepted: the user accepted; the operation was sent.
	confirmationAccepted confirmationOutcome = "accepted"
	// confirmationDeclined: the user declined or cancelled; nothing was sent.
	confirmationDeclined confirmationOutcome = "declined"
	// confirmationInvalid: the confirmation data was missing, forged,
	// expired or bound to other arguments; nothing was sent.
	confirmationInvalid confirmationOutcome = "invalid"
)

// Hints of the confirmation refusals (ADR-0008): static text.
const (
	confirmationUnavailableHint = "the client cannot confirm destructive operations (DELETE, PUT, PATCH): it " +
		"offers no form elicitation, so they never run; use a client with form elicitation, or make the change " +
		"in the Langfuse UI"
	confirmationDeclinedHint = "the user declined the change and nothing was sent to Langfuse; do not call again " +
		"unless the user asks for the change again"
	confirmationInvalidHint = "nothing was sent to Langfuse; call execute_write again with the same arguments and " +
		"no confirmation data (no inputResponses, no requestState) to ask the user afresh"
)

// formElicitation reports whether caps offer form elicitation: an
// elicitation capability that names form, or names no mode at all (the
// pre-2025-11-25 shape, where form was the only mode).
func formElicitation(caps *mcp.ClientCapabilities) bool {
	if caps == nil || caps.Elicitation == nil {
		return false
	}
	e := caps.Elicitation
	return e.Form != nil || e.URL == nil
}

// confirm runs the confirmation of a destructive call whose parameters and
// body passed their checks. It reports accepted when the user accepted this
// exact call and it may be sent; otherwise res answers the call. Nothing
// is sent to Langfuse in any refusal. The client's capabilities are read per
// request.
func (ex executor) confirm(req *mcp.CallToolRequest, op catalog.Operation, params map[string]any,
	request catalog.Request, body json.RawMessage, a *audit,
) (res *mcp.CallToolResult, accepted bool) {
	if !formElicitation(req.ClientCapabilities()) {
		a.confirmation = confirmationUnavailable
		return refusal(toolError(errorConfirmationUnavailable, "operation "+op.ID+" is a destructive "+op.Method+
			" operation, which runs only after the user confirms it, and the client cannot ask the user",
			confirmationUnavailableHint, op.ID))
	}
	canonical := canonicalArgs(request, body)
	responses := req.Params.InputResponses
	if len(responses) == 0 {
		a.confirmation = confirmationRequested
		return &mcp.CallToolResult{
			InputRequests: mcp.InputRequestMap{confirmInputID: &mcp.ElicitParams{
				Message: ex.redact.Redact(confirmationText(op, params, request, body)),
				// An accept-only form: the answer is the action, no field.
				RequestedSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			}},
			RequestState: ex.confirmer.sign(op.ID, canonical),
		}, false
	}
	answer, ok := responses[confirmInputID].(*mcp.ElicitResult)
	if !ok || !ex.confirmer.valid(op.ID, canonical, req.Params.RequestState) {
		a.confirmation = confirmationInvalid
		return refusal(toolError(errorConfirmationInvalid, "the confirmation of operation "+op.ID+" is missing, "+
			"forged, expired or was given for other arguments", confirmationInvalidHint, op.ID))
	}
	switch answer.Action {
	case "accept":
		a.confirmation = confirmationAccepted
		return nil, true
	case "decline", "cancel":
		a.confirmation = confirmationDeclined
		return refusal(toolError(errorConfirmationDeclined, "the user did not confirm operation "+op.ID,
			confirmationDeclinedHint, op.ID))
	}
	a.confirmation = confirmationInvalid
	return refusal(toolError(errorConfirmationInvalid, "the confirmation of operation "+op.ID+" has no known answer",
		confirmationInvalidHint, op.ID))
}

// refusal is a tool error that refuses the call: it is never accepted.
func refusal(res *mcp.CallToolResult, _ error) (*mcp.CallToolResult, bool) {
	return res, false // toolError never fails: it marshals a fixed struct
}

// maxSummaryRunes is the rune budget of the body summary in the confirmation
// text.
const maxSummaryRunes = 300

// confirmationText is the question the user answers: the operation ID, the
// method, the path and query parameters, and a summary of the body cut at
// maxSummaryRunes, saying so and giving the body size when cut. Every
// argument-derived string is stripped of control, invisible and bidi
// characters and shown quoted, as text: markup is never rendered.
func confirmationText(op catalog.Operation, params map[string]any, request catalog.Request,
	body json.RawMessage,
) string {
	var b strings.Builder
	b.WriteString("Confirm this destructive change to Langfuse. The values below come from the call's " +
		"arguments and are shown as quoted text.\n\n")
	b.WriteString("Operation: " + op.ID + "\nMethod: " + op.Method + "\n")
	var path []string
	for _, p := range op.Params {
		if v, ok := params[p.Name]; ok && p.In == "path" {
			path = append(path, "  "+p.Name+" = "+quoted(v))
		}
	}
	if len(path) > 0 {
		b.WriteString("Path parameters:\n" + strings.Join(path, "\n") + "\n")
	}
	if len(request.Query) > 0 {
		b.WriteString("Query parameters:\n")
		for _, name := range slices.Sorted(maps.Keys(request.Query)) {
			for _, v := range request.Query[name] {
				b.WriteString("  " + quoted(name) + " = " + quoted(v) + "\n")
			}
		}
	}
	if len(body) == 0 {
		b.WriteString("Body: none\n")
		return b.String()
	}
	if n, ok := op.DeletedTraceCount(body); ok {
		b.WriteString("Trace IDs to delete: " + strconv.Itoa(n) + "\n")
	}
	summary, cut := bodySummary(body)
	if cut {
		b.WriteString("Body (" + strconv.Itoa(len(body)) + " bytes; summary cut after " +
			strconv.Itoa(maxSummaryRunes) + " characters): " + summary + " [cut]\n")
	} else {
		b.WriteString("Body (" + strconv.Itoa(len(body)) + " bytes): " + summary + "\n")
	}
	return b.String()
}

// quoted shows an argument value as one quoted line of text: strings are
// stripped of control, invisible and bidi characters; numbers and booleans
// keep their literal text.
func quoted(v any) string {
	s, ok := v.(string)
	if !ok {
		raw, _ := json.Marshal(v) // a validated parameter value: string, number, boolean or a list of those
		return string(raw)
	}
	return jsonText(sanitize.Line(s))
}

// jsonText encodes v as JSON without HTML escaping, so markup reads as the
// characters it is made of.
func jsonText(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v) // strings, json.Number, bools, maps and slices of those always encode
	return strings.TrimSuffix(buf.String(), "\n")
}

// bodySummary returns the body as compact JSON with every string (keys
// included) stripped, cut at maxSummaryRunes; cut reports whether it was.
func bodySummary(body json.RawMessage) (summary string, cut bool) {
	v, _ := sanitize.Decode(body) // body passed CheckBody: it is valid JSON
	s := jsonText(sanitize.LineValue(v))
	if utf8.RuneCountInString(s) <= maxSummaryRunes {
		return s, false
	}
	return string([]rune(s)[:maxSummaryRunes]), true
}
