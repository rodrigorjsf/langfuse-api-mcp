package server

import (
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
)

// Tool error codes for requests that never got a Langfuse answer (ADR-0008).
// They are part of the public interface: renaming one is a breaking change.
const (
	errorTLSUntrustedCertificate = "tls_untrusted_certificate"
	errorNetwork                 = "network_error"
	errorTimeout                 = "timeout"
	errorCanceled                = "canceled"
)

// narrowHint tells the agent how to make a slow read cheaper.
const narrowHint = "narrow the query before calling again: a shorter time window, fewer fields or a lower limit"

// caHint tells the user how to trust the CA of the Langfuse server.
const caHint = "set LANGFUSE_CA_CERT " +
	"(a PEM file) or LANGFUSE_CA_CERTS_PATH (a directory of PEM files) to the CA that signed it, " +
	"then check the 'CA sources loaded' line of the server's startup log"

// failure is the tool error for a failed Langfuse request. It is the one place
// where client errors become tool errors; the cause goes to the audit line a
// (stderr), never to the agent.
func failure(operationID string, err error, a *audit) (*mcp.CallToolResult, error) {
	fields := failureFields(err)
	fields.OperationID = truncate(operationID)
	a.cause = err.Error()
	return errorResult(fields)
}

// failureFields classifies err into the tool error fields, without the
// operation ID.
func failureFields(err error) toolErrorFields {
	switch {
	case errors.Is(err, langfuse.ErrUntrustedCertificate):
		return toolErrorFields{
			Code:    errorTLSUntrustedCertificate,
			Message: "the TLS certificate of the Langfuse server is signed by a CA this server does not trust",
			Hint:    "the Langfuse server's certificate is not trusted by this server: " + caHint,
		}
	case errors.Is(err, langfuse.ErrCertificateRejected):
		return toolErrorFields{
			Code: errorTLSUntrustedCertificate,
			Message: "the TLS certificate of the Langfuse server failed verification " +
				"(host name, validity period or usage)",
			Hint: "check that LANGFUSE_BASE_URL names the host the Langfuse server's certificate was issued for; " +
				"if its CA is the problem, " + caHint,
		}
	case errors.Is(err, langfuse.ErrResponseTooLarge):
		return toolErrorFields{
			Code:    errorResponseTooLarge,
			Message: "the Langfuse response is larger than the 5 MiB this server reads; it was not returned",
			Hint:    tooLargeHint,
		}
	case errors.Is(err, langfuse.ErrNetwork):
		return toolErrorFields{
			Code:      errorNetwork,
			Message:   "the Langfuse host could not be reached (DNS, connection or proxy failure)",
			Hint:      "check that the host in LANGFUSE_BASE_URL is right and reachable from this machine; behind a proxy, check HTTPS_PROXY and NO_PROXY",
			Retryable: true,
		}
	case errors.Is(err, langfuse.ErrTimeout):
		return toolErrorFields{
			Code:    errorTimeout,
			Message: "Langfuse did not answer before the request deadline",
			Hint:    narrowHint,
		}
	case errors.Is(err, langfuse.ErrCanceled):
		return toolErrorFields{
			Code:    errorCanceled,
			Message: "the request was canceled before Langfuse answered",
			Hint:    "if the call was canceled because it was slow, " + narrowHint,
		}
	default:
		return toolErrorFields{Code: errorInternal, Message: "the Langfuse request failed"}
	}
}
