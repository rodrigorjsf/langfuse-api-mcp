package langfuse

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
)

// Sentinels for requests that never got a Langfuse answer. Do wraps the cause
// with one of them, so callers branch with errors.Is. The classification uses
// error types only, never error text.
var (
	// ErrUntrustedCertificate: the server certificate chains to no CA of the
	// trust pool.
	ErrUntrustedCertificate = errors.New("the Langfuse server certificate is signed by an untrusted CA")
	// ErrCertificateRejected: the server certificate failed verification for
	// another reason: host name, validity period or usage.
	ErrCertificateRejected = errors.New("the Langfuse server certificate failed verification")
	// ErrNetwork: the host could not be reached: DNS, connection or proxy
	// failure, or a connection dropped before any answer.
	ErrNetwork = errors.New("the Langfuse host could not be reached")
	// ErrTimeout: the request deadline passed before Langfuse answered.
	ErrTimeout = errors.New("the Langfuse request timed out")
	// ErrCanceled: the caller canceled the request.
	ErrCanceled = errors.New("the Langfuse request was canceled")
)

// classify wraps a transport error with the sentinel of its failure class, or
// returns it unchanged when it belongs to none.
func classify(err error) error {
	return wrapClass(failureClass(err), err)
}

// wrapClass wraps err with the sentinel class; a nil class leaves it unchanged.
func wrapClass(class, err error) error {
	if class == nil {
		return err
	}
	return fmt.Errorf("%w: %w", class, err)
}

// failureClass returns the sentinel of err's failure class, or nil when it
// belongs to none.
func failureClass(err error) error {
	var (
		unknownAuthority x509.UnknownAuthorityError
		verification     *tls.CertificateVerificationError
		dnsErr           *net.DNSError
		opErr            *net.OpError
		proxyConnect     proxyConnectError
		netErr           net.Error
	)
	switch {
	case errors.Is(err, context.Canceled):
		return ErrCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return ErrTimeout
	// Before the generic timeout: a DNS lookup that timed out is a
	// connectivity fault, not a slow query.
	case errors.As(err, &dnsErr):
		return ErrNetwork
	// Before the network errors: a read that timed out is one too.
	case errors.As(err, &netErr) && netErr.Timeout():
		return ErrTimeout
	// Before the verification error, which wraps it.
	case errors.As(err, &unknownAuthority):
		return ErrUntrustedCertificate
	case errors.As(err, &verification):
		return ErrCertificateRejected
	// A refused proxy dial is an OpError ("proxyconnect"); a proxy that
	// answers the CONNECT with a status other than 200 is this one.
	case errors.As(err, &opErr), errors.As(err, &proxyConnect):
		return ErrNetwork
	default:
		return nil
	}
}

// classifySend classifies an error of http.Client.Do, returned before any
// answer exists. Besides the classes of classify, an end-of-stream there is a
// connection the host or proxy dropped before answering: a reset that lands
// after the request write reaches Do as *url.Error{Err: io.EOF} (#91). A body
// read keeps classify alone, so a body cut short after the answer started is
// not taken for a network failure.
func classifySend(err error) error {
	class := failureClass(err)
	if class == nil && errors.Is(err, io.EOF) {
		class = ErrNetwork
	}
	return wrapClass(class, err)
}
