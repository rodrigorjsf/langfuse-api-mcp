package server

import (
	"errors"
	"strconv"

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

// failure is the tool error for a Langfuse request that got no usable answer
// (a transport failure, a refused redirect or an oversized body); a Langfuse
// error answer goes through langfuseErrorFields instead. err itself never
// reaches the agent; the caller records it on the audit line (stderr).
func failure(operationID string, err error) (*mcp.CallToolResult, error) {
	fields := failureFields(err)
	fields.OperationID = truncate(operationID)
	return errorResult(fields)
}

// failureFields classifies err into the tool error fields, without the
// operation ID.
func failureFields(err error) toolErrorFields {
	switch {
	case errors.Is(err, langfuse.ErrRedirectRefused):
		return toolErrorFields{
			Code: errorRedirectRefused,
			Message: "Langfuse answered with a redirect to another scheme, host or port; " +
				"it was not followed, so the key pair was not sent there",
			Hint: "retrying will not help: the user sets LANGFUSE_BASE_URL to the URL the Langfuse host redirects to " +
				"(for example https instead of http) and restarts the server",
		}
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
			Code: errorResponseTooLarge,
			Message: "the Langfuse response is larger than the " + strconv.Itoa(langfuse.MaxResponseBytes>>20) +
				" MiB this server reads; it was not returned",
			Hint: tooLargeHint,
		}
	case errors.Is(err, langfuse.ErrNetwork):
		return toolErrorFields{
			Code:      errorNetwork,
			Message:   "the Langfuse host could not be reached (DNS, connection or proxy failure)",
			Hint:      "check that the host in LANGFUSE_BASE_URL is right and reachable from this machine; behind a proxy, check HTTPS_PROXY and NO_PROXY",
			Retryable: true,
		}
	// Before the timeout, which it wraps: the call never left this server.
	case errors.Is(err, langfuse.ErrThrottled):
		return toolErrorFields{
			Code: errorTimeout,
			Message: "the call waited for this server's rate limit or concurrency cap until its deadline passed; " +
				"it was not sent to Langfuse",
			Hint: "wait a few seconds, then call again, one call at a time; the operator sets the limits with " +
				"LANGFUSE_MCP_RATE_LIMIT and LANGFUSE_MCP_MAX_CONCURRENCY",
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
		return toolErrorFields{
			Code:    errorInternal,
			Message: "the Langfuse request failed",
			Hint: "check that LANGFUSE_BASE_URL points at the Langfuse API itself, not a login page or proxy; " +
				"if it does, report this with the server's stderr log",
		}
	}
}
