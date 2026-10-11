// Package transport starts the MCP server on a transport. M1 has stdio only;
// the loopback Streamable HTTP transport is a later milestone.
package transport

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Stdio serves s over the process's stdin and stdout until the client closes
// stdin or ctx is done. Only the protocol is written to stdout; logs go to
// stderr. A client closing stdin is a clean end, not an error.
//
// Nothing is read from stdin until the session has recorded the protocols this
// transport offers (see [legacyStdio]). That is why it connects the session
// and waits for it itself instead of calling [mcp.Server.Run], which gives the
// caller no moment between the connection and the first message handled.
func Stdio(ctx context.Context, s *mcp.Server) error {
	t := newLegacyStdio()
	ss, err := s.Connect(ctx, t, nil)
	if err != nil {
		return err
	}
	t.stdin.release()

	ended := make(chan error, 1)
	go func() { ended <- ss.Wait() }()
	select {
	case err = <-ended:
	case <-ctx.Done():
		_ = ss.Close() // the session ends either way; ctx's error is what the caller needs
		<-ended
		err = ctx.Err()
	}
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// sessionlessProtocol is the first MCP protocol version negotiated without
// the legacy initialize handshake (SEP-2575, `server/discover`).
const sessionlessProtocol = "2026-07-28"

// legacyStdio is the SDK's stdio transport (an [mcp.IOTransport] over stdin
// and stdout) set up so that a `server/discover` probe never negotiates a
// session, however early it arrives. A host that probes first (Claude Code:
// `server/discover`, then, after 3 s without an answer, a legacy initialize
// on the same pipe) leaves the probe waiting in stdin while startup detects
// the deployment profile, and go-sdk v1.8.0 rejects the legacy initialize as a
// duplicate when the probe negotiated protocol 2026-07-28. Two things prevent
// that:
//
//   - It declares no protocol from 2026-07-28 (#145). Once the session has
//     recorded that, the probe is answered with the older versions and stores
//     nothing on the session.
//   - Stdin is not read until the session has recorded it (#191). Server.Connect
//     starts the session's read loop first and records the transport's
//     protocols a few instructions later; a probe handled in between is
//     answered as if stdio offered every protocol, 2026-07-28 included, and
//     stores its parameters as the session's initialize parameters. The SDK's
//     comment there says no message can arrive before Connect returns, which
//     over stdio is false: the bytes are already in the pipe. [Stdio] opens
//     stdin when Connect has returned.
//
// Revert both once the SDK accepts a legacy initialize after a probe and
// records a transport's protocols before it reads the first message (#162).
type legacyStdio struct {
	mcp.IOTransport
	stdin *heldReader // the transport's reader; the session's owner releases it
}

func newLegacyStdio() *legacyStdio {
	stdin := &heldReader{ReadCloser: os.Stdin, open: make(chan struct{}), closed: make(chan struct{})}
	return &legacyStdio{IOTransport: mcp.IOTransport{Reader: stdin, Writer: nopWriteCloser{os.Stdout}}, stdin: stdin}
}

// SupportsProtocolVersion implements [mcp.ProtocolVersionSupporter].
// Protocol versions are dates, so they compare as strings.
func (legacyStdio) SupportsProtocolVersion(version string) bool {
	return version < sessionlessProtocol
}

// heldReader is a reader whose reads wait until release is called, or until
// Close; a Read that waits when Close is called ends with [io.ErrClosedPipe].
type heldReader struct {
	io.ReadCloser

	open        chan struct{} // closed by release
	closed      chan struct{} // closed by Close
	releaseOnce sync.Once
	closeOnce   sync.Once
}

func (h *heldReader) release() { h.releaseOnce.Do(func() { close(h.open) }) }

func (h *heldReader) Read(p []byte) (int, error) {
	select {
	case <-h.open:
		return h.ReadCloser.Read(p)
	case <-h.closed:
		return 0, io.ErrClosedPipe
	}
}

func (h *heldReader) Close() error {
	h.closeOnce.Do(func() { close(h.closed) })
	return h.ReadCloser.Close()
}

// nopWriteCloser is a writer whose Close does nothing: closing stdout is not
// the transport's to do.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
