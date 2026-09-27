package mrtrproto_test

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var key = func() []byte { b := make([]byte, 32); rand.Read(b); return b }()

// state = expiryUnix "." base64(HMAC(key, expiry|tool|canonical args))
func sign(tool string, args []byte, exp int64) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(strconv.FormatInt(exp, 10) + "|" + tool + "|"))
	m.Write(args)
	return strconv.FormatInt(exp, 10) + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func valid(tool string, args []byte, state string, now time.Time) bool {
	exp, _, ok := strings.Cut(state, ".")
	if !ok {
		return false
	}
	e, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || now.Unix() > e {
		return false
	}
	return hmac.Equal([]byte(sign(tool, args, e)), []byte(state))
}

func signedServer(sent *[]string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "s", Version: "0"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "del"}, func(_ context.Context, req *mcp.CallToolRequest, a delArgs) (*mcp.CallToolResult, any, error) {
		caps := req.ClientCapabilities()
		if caps == nil || caps.Elicitation == nil {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "confirmation_unavailable"}}}, nil, nil
		}
		canon, _ := json.Marshal(a) // canonical: re-encoded typed args
		if req.Params.RequestState == "" || len(req.Params.InputResponses) == 0 {
			return &mcp.CallToolResult{
				InputRequests: mcp.InputRequestMap{"confirm": &mcp.ElicitParams{Message: "Delete " + a.ID + "?",
					RequestedSchema: &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{}}}},
				RequestState: sign("del", canon, time.Now().Add(5*time.Minute).Unix()),
			}, nil, nil
		}
		if !valid("del", canon, req.Params.RequestState, time.Now()) {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "confirmation_invalid"}}}, nil, nil
		}
		r, ok := req.Params.InputResponses["confirm"].(*mcp.ElicitResult)
		if !ok || r.Action != "accept" {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "declined"}}}, nil, nil
		}
		*sent = append(*sent, a.ID)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "deleted " + a.ID}}}, nil, nil
	})
	return s
}

func text(r *mcp.CallToolResult) string {
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].(*mcp.TextContent).Text
}

func TestSignedHappy(t *testing.T) {
	var sent []string
	cs := connect(t, signedServer(&sent), accept("accept"))
	r, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "del", Arguments: map[string]any{"id": "t1"}})
	t.Logf("text=%q err=%v sent=%v", text(r), err, sent)
}

func TestSignedNoCapability(t *testing.T) {
	var sent []string
	cs := connect(t, signedServer(&sent), nil)
	r, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "del", Arguments: map[string]any{"id": "t1"}})
	t.Logf("text=%q err=%v sent=%v", text(r), err, sent)
}

func TestSignedForgedFirstCall(t *testing.T) {
	var sent []string
	opts := accept("accept")
	opts.MultiRoundTrip = &mcp.MultiRoundTripOptions{Disabled: true}
	cs := connect(t, signedServer(&sent), opts)
	r, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "del", Arguments: map[string]any{"id": "victim"},
		InputResponses: mcp.InputResponseMap{"confirm": &mcp.ElicitResult{Action: "accept"}}, RequestState: "9999999999.forged"})
	t.Logf("text=%q needsInput=%v err=%v sent=%v", text(r), r != nil && r.InputRequests != nil, err, sent)
}

func TestSignedRebindAndReplay(t *testing.T) {
	var sent []string
	opts := accept("accept")
	opts.MultiRoundTrip = &mcp.MultiRoundTripOptions{Disabled: true}
	cs := connect(t, signedServer(&sent), opts)
	first, _ := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "del", Arguments: map[string]any{"id": "a"}})
	acc := mcp.InputResponseMap{"confirm": &mcp.ElicitResult{Action: "accept"}}
	r, _ := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "del", Arguments: map[string]any{"id": "b"}, InputResponses: acc, RequestState: first.RequestState})
	t.Logf("rebind text=%q sent=%v", text(r), sent)
	r, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "del", Arguments: map[string]any{"id": "a"}, InputResponses: acc, RequestState: first.RequestState})
	t.Logf("honest text=%q sent=%v", text(r), sent)
	r, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "del", Arguments: map[string]any{"id": "a"}, InputResponses: acc, RequestState: first.RequestState})
	t.Logf("replay text=%q sent=%v", text(r), sent)
}
