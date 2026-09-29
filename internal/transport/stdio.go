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
	err := s.Run(ctx, &legacyStdio{})
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// sessionlessProtocol is the first MCP protocol version negotiated without
// the legacy initialize handshake (SEP-2575, `server/discover`).
const sessionlessProtocol = "2026-07-28"

// legacyStdio is the SDK's stdio transport declaring only the protocols
// negotiated through the legacy initialize handshake (#145). Startup reads
// stdin only after deployment profile detection; a host that times out its
// `server/discover` probe (Claude Code: 3 s) queues a legacy initialize
// behind it, and go-sdk v1.8.0 rejects that initialize as a duplicate when
// the probe negotiated protocol 2026-07-28. Revert once the SDK accepts it.
type legacyStdio struct{ mcp.StdioTransport }

// SupportsProtocolVersion implements [mcp.ProtocolVersionSupporter].
// Protocol versions are dates, so they compare as strings.
func (legacyStdio) SupportsProtocolVersion(version string) bool {
	return version < sessionlessProtocol
}
