package mrtrproto_test

// Prototype (M4 grilling, Q7): does a stateless multi round-trip confirmation
// bind the user's accept to the arguments the user saw? Throwaway; kept on the
// prototype/m4-mrtr-confirmation branch as a primary source.

import (
	"context"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type delArgs struct {
	ID string `json:"id"`
}

// statelessServer: confirms only when InputResponses carries accept; no RequestState.
func statelessServer(sent *[]string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "s", Version: "0"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "del"}, func(_ context.Context, req *mcp.CallToolRequest, a delArgs) (*mcp.CallToolResult, any, error) {
		if len(req.Params.InputResponses) == 0 {
			return &mcp.CallToolResult{InputRequests: mcp.InputRequestMap{"confirm": &mcp.ElicitParams{
				Message:         "Delete " + a.ID + "?",
				RequestedSchema: &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{}},
			}}}, nil, nil
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

func connect(t *testing.T, s *mcp.Server, opts *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	ct, st := mcp.NewInMemoryTransports()
	ss, err := s.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, opts).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	t.Logf("negotiated protocol: %s", ss.InitializeParams().ProtocolVersion)
	return cs
}

func accept(action string) *mcp.ClientOptions {
	return &mcp.ClientOptions{ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		return &mcp.ElicitResult{Action: action}, nil
	}}
}

func TestProtoHappyPath(t *testing.T) {
	var sent []string
	cs := connect(t, statelessServer(&sent), accept("accept"))
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "del", Arguments: map[string]any{"id": "t1"}})
	t.Logf("res=%+v err=%v sent=%v", res, err, sent)
}

func TestProtoDecline(t *testing.T) {
	var sent []string
	cs := connect(t, statelessServer(&sent), accept("decline"))
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "del", Arguments: map[string]any{"id": "t1"}})
	t.Logf("isError=%v err=%v sent=%v", res != nil && res.IsError, err, sent)
}

func TestProtoNoElicitationCapability(t *testing.T) {
	var sent []string
	cs := connect(t, statelessServer(&sent), nil)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "del", Arguments: map[string]any{"id": "t1"}})
	t.Logf("res=%+v err=%v sent=%v", res, err, sent)
}

// Forged: client (MRTR middleware off) sends accept on the FIRST call.
func TestProtoForgedFirstCall(t *testing.T) {
	var sent []string
	cs := connect(t, statelessServer(&sent), &mcp.ClientOptions{MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true}})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "del", Arguments: map[string]any{"id": "victim"},
		InputResponses: mcp.InputResponseMap{"confirm": &mcp.ElicitResult{Action: "accept"}}})
	t.Logf("res=%+v err=%v sent=%v", res, err, sent)
}

// Rebind: user saw "a", client re-sends with "b" plus the accept.
func TestProtoRebind(t *testing.T) {
	var sent []string
	cs := connect(t, statelessServer(&sent), &mcp.ClientOptions{MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true}})
	first, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "del", Arguments: map[string]any{"id": "a"}})
	t.Logf("first needsInput=%v state=%q err=%v", first != nil && first.InputRequests != nil, func() string { if first != nil { return first.RequestState }; return "" }(), err)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "del", Arguments: map[string]any{"id": "b"},
		InputResponses: mcp.InputResponseMap{"confirm": &mcp.ElicitResult{Action: "accept"}}})
	t.Logf("res=%+v err=%v sent=%v", res, err, sent)
}
