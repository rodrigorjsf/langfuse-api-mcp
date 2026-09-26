// Package transport starts the MCP server on a transport. M1 has stdio only;
// the loopback Streamable HTTP transport is a later milestone.
package transport

import (
	"context"
	"errors"
	"io"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Stdio serves s over the process's stdin and stdout until the client closes
// stdin or ctx is done. Only the protocol is written to stdout; logs go to
// stderr. A client closing stdin is a clean end, not an error.
func Stdio(ctx context.Context, s *mcp.Server) error {
	err := s.Run(ctx, &mcp.StdioTransport{})
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
