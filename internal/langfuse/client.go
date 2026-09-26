// Package langfuse is the HTTP client for the Langfuse public API: Basic auth,
// the shared transport, timeouts and bounded response reads. It knows HTTP,
// not MCP.
package langfuse

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Options configures a Client.
type Options struct {
	// Host is the Langfuse base URL (LANGFUSE_BASE_URL); operation paths
	// such as /api/public/traces are appended to it.
	Host *url.URL
	// PublicKey and SecretKey are the key pair sent as HTTP Basic auth.
	PublicKey string
	SecretKey string
	// TLS is the client TLS configuration built from the trust pool; nil uses
	// Go's defaults.
	TLS *tls.Config
	// RequestTimeout is the deadline of one Do call, retries and their waits
	// included; zero means DefaultRequestTimeout.
	RequestTimeout time.Duration
	// Wait pauses between retries until d has passed or ctx is done; nil
	// uses a real timer. Tests replace it so backoff never sleeps.
	Wait func(ctx context.Context, d time.Duration) error
}

// DefaultRequestTimeout is the deadline of one Do call when Options leaves it zero.
const DefaultRequestTimeout = 60 * time.Second

// redacted replaces the key pair wherever Options or Client are printed.
const redacted = "[REDACTED]"

// String keeps the key pair out of %v and %s.
func (o Options) String() string {
	return "langfuse.Options{Host: " + hostString(o.Host) + ", keys: " + redacted + "}"
}

// GoString keeps the key pair out of %#v.
func (o Options) GoString() string { return o.String() }

// LogValue keeps the key pair out of slog.
func (o Options) LogValue() slog.Value { return slog.StringValue(o.String()) }

// Client sends requests to one Langfuse host. It is safe for concurrent use.
// Printing it never shows the key pair.
type Client struct {
	host       *url.URL
	publicKey  string
	secretKey  string
	httpClient *http.Client
	timeout    time.Duration
	wait       func(ctx context.Context, d time.Duration) error
}

// String keeps the key pair out of %v and %s.
func (c Client) String() string {
	return "langfuse.Client{Host: " + hostString(c.host) + ", keys: " + redacted + "}"
}

// GoString keeps the key pair out of %#v.
func (c Client) GoString() string { return c.String() }

// LogValue keeps the key pair out of slog.
func (c Client) LogValue() slog.Value { return slog.StringValue(c.String()) }

func hostString(u *url.URL) string {
	if u == nil {
		return "<nil>"
	}
	return u.Redacted()
}

// New returns a client for the host in opts, with one shared, tuned transport.
func New(opts Options) *Client {
	transport := &http.Transport{
		// No proxy until proxy settings exist (M2): nothing is sent through a
		// proxy the operator did not configure for this server.
		Proxy:                 nil,
		TLSClientConfig:       opts.TLS,
		ForceAttemptHTTP2:     true,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	c := &Client{
		host:      opts.Host,
		publicKey: opts.PublicKey,
		secretKey: opts.SecretKey,
		// The per-call deadline is a context deadline set in Do, so that it
		// also bounds the retries and their waits.
		httpClient: &http.Client{Transport: transport},
		timeout:    opts.RequestTimeout,
		wait:       opts.Wait,
	}
	if c.timeout <= 0 {
		c.timeout = DefaultRequestTimeout
	}
	if c.wait == nil {
		c.wait = sleep
	}
	return c
}

// maxResponseBytes caps how much of a response body the client reads: Langfuse
// Cloud caps responses at 5 MB.
const maxResponseBytes = 5 << 20

// Response is a successful Langfuse answer.
type Response struct {
	Status int
	// Body is the response JSON, forwarded as is.
	Body json.RawMessage
}

// Do sends a request: method, the escaped path below the host (e.g.
// /api/public/traces/abc) and the query, with Basic auth, under the client's
// per-call deadline. It returns the response JSON for a 2xx answer and an
// *APIError otherwise. A GET is retried within the deadline: twice with
// exponential backoff and jitter on a 5xx, once after Retry-After on a 429.
// Other methods are never retried: they are not idempotent.
func (c *Client) Do(ctx context.Context, method, escapedPath string, query url.Values) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var r retries
	for {
		resp, err := c.send(ctx, method, escapedPath, query)
		wait, retry := r.next(method, err)
		if !retry || !fits(ctx, wait) || c.wait(ctx, wait) != nil {
			return resp, err
		}
	}
}

// send makes one attempt of Do.
func (c *Client) send(ctx context.Context, method, escapedPath string, query url.Values) (Response, error) {
	u := *c.host
	u.RawPath = strings.TrimSuffix(c.host.EscapedPath(), "/") + escapedPath
	path, err := url.PathUnescape(u.RawPath)
	if err != nil {
		return Response{}, fmt.Errorf("request path: %w", err)
	}
	u.Path = path
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return Response{}, fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(c.publicKey, c.secretKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("send request: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes)) // drain so the connection is reused
		_ = resp.Body.Close()                                                   // nothing useful to do on a close error
	}()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		if err != nil {
			return Response{}, fmt.Errorf("read error response: %w", err)
		}
		return Response{}, newAPIError(resp.StatusCode, resp.Header, body)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return Response{}, fmt.Errorf("response exceeds %d bytes", maxResponseBytes)
	}
	if !json.Valid(body) {
		return Response{}, errors.New("response is not JSON")
	}
	return Response{Status: resp.StatusCode, Body: body}, nil
}
