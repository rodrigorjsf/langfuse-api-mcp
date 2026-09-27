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

	"golang.org/x/time/rate"
)

// Options configures a Client.
type Options struct {
	// Host is the Langfuse base URL (LANGFUSE_BASE_URL); operation paths
	// such as /api/public/traces are appended to it.
	Host *url.URL
	// Keys is the key pair sent as HTTP Basic auth.
	Keys KeyPair
	// TLS is the client TLS configuration built from the trust pool; nil uses
	// Go's defaults.
	TLS *tls.Config
	// RequestTimeout is the deadline of one Do call, retries and their waits
	// included; zero means DefaultRequestTimeout.
	RequestTimeout time.Duration
	// Wait pauses between retries until d has passed or ctx is done; nil
	// uses a real timer. Tests replace it so backoff never sleeps.
	Wait func(ctx context.Context, d time.Duration) error
	// Now is the clock the rate limit reads; nil uses time.Now. Tests replace
	// it, with Wait, so paced requests never sleep.
	Now func() time.Time
	// RateLimit is the most requests the client sends per minute, retries
	// included (LANGFUSE_MCP_RATE_LIMIT); zero or less means DefaultRateLimit.
	RateLimit int
	// MaxConcurrency is the most requests in flight at once
	// (LANGFUSE_MCP_MAX_CONCURRENCY); zero or less means DefaultMaxConcurrency.
	MaxConcurrency int
	// Proxy picks the proxy for each request, with the http.Transport.Proxy
	// signature: the executable passes ProxyFromSettings, tests pass
	// http.ProxyURL of a fake proxy (ADR-0006). Credentials in the proxy URL
	// go out as Basic proxy authentication. Nil means no proxy.
	Proxy func(*http.Request) (*url.URL, error)
}

// DefaultRequestTimeout is the deadline of one Do call when Options leaves it zero.
const DefaultRequestTimeout = 60 * time.Second

// Default limits when Options leaves them zero. 30 requests per minute is the
// Langfuse Cloud Hobby General API limit, the lowest of the plans. The
// executable never leaves RateLimit zero: config picks its default by host (#47).
const (
	DefaultRateLimit      = 30
	DefaultMaxConcurrency = 4
)

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
	keys       KeyPair
	httpClient *http.Client
	timeout    time.Duration
	wait       func(ctx context.Context, d time.Duration) error
	now        func() time.Time
	// limiter paces every request of the client, retries included.
	limiter *rate.Limiter
	// slots holds one token per request in flight; its capacity is the cap.
	slots chan struct{}
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
		// Only the proxy the caller passes: nothing is sent through a proxy
		// the operator did not configure for this server.
		Proxy: opts.Proxy,
		// A CONNECT refused by the proxy becomes a typed network failure,
		// without the proxy's text.
		OnProxyConnectResponse: checkProxyConnect,
		TLSClientConfig:        opts.TLS,
		ForceAttemptHTTP2:      true,
		MaxIdleConnsPerHost:    4,
		IdleConnTimeout:        90 * time.Second,
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  30 * time.Second,
	}
	c := &Client{
		host: opts.Host,
		keys: opts.Keys,
		// The per-call deadline is a context deadline set in Do, so that it
		// also bounds the retries and their waits.
		httpClient: &http.Client{Transport: transport, CheckRedirect: sameOrigin},
		timeout:    opts.RequestTimeout,
		wait:       opts.Wait,
	}
	if c.timeout <= 0 {
		c.timeout = DefaultRequestTimeout
	}
	if c.wait == nil {
		c.wait = sleep
	}
	c.now = opts.Now
	if c.now == nil {
		c.now = time.Now
	}
	perMinute, concurrency := opts.RateLimit, opts.MaxConcurrency
	if perMinute <= 0 {
		perMinute = DefaultRateLimit
	}
	if concurrency <= 0 {
		concurrency = DefaultMaxConcurrency
	}
	// The burst equals the cap, so that a full set of parallel requests
	// leaves at once; the rate then paces the rest.
	c.limiter = rate.NewLimiter(rate.Limit(float64(perMinute)/60), concurrency)
	c.slots = make(chan struct{}, concurrency)
	return c
}

// CloseIdleConnections closes the connections the client keeps open between
// requests, and the transport goroutines that serve them; requests in flight
// are not interrupted, and a later request opens a new connection. The server
// calls it on shutdown; tests call it so that no goroutine outlives them.
func (c *Client) CloseIdleConnections() { c.httpClient.CloseIdleConnections() }

// ErrRedirectRefused marks a request Langfuse redirected to another scheme,
// host or port: the client never follows it, so the key pair is never sent
// anywhere but the configured host.
var ErrRedirectRefused = errors.New("redirect to another scheme, host or port refused")

// maxRedirects is how many same-origin redirects one request follows, as Go's
// default policy does.
const maxRedirects = 10

// sameOrigin is the client's redirect policy. Go forwards Authorization on a
// redirect to the same host name, even on another port or scheme, and to its
// subdomains, so every redirect that changes scheme, host or port is refused
// before it is sent.
func sameOrigin(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	from, to := via[0].URL, req.URL
	if !strings.EqualFold(from.Scheme, to.Scheme) || !strings.EqualFold(from.Hostname(), to.Hostname()) ||
		effectivePort(from) != effectivePort(to) {
		return ErrRedirectRefused
	}
	return nil
}

// effectivePort is the URL's port, or its scheme's default port.
func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if strings.EqualFold(u.Scheme, "http") {
		return "80"
	}
	return "443"
}

// MaxResponseBytes caps how much of a response body the client reads: Langfuse
// Cloud caps responses at 5 MB.
const MaxResponseBytes = 5 << 20

// ErrResponseTooLarge marks a 2xx answer whose body exceeds MaxResponseBytes;
// the body is not read further.
var ErrResponseTooLarge = fmt.Errorf("response exceeds %d bytes", MaxResponseBytes)

// Response is a successful Langfuse answer.
type Response struct {
	Status int
	// Body is the response JSON, forwarded as is.
	Body json.RawMessage
	// Attempts is the number of requests Do sent to Langfuse, retries
	// included; an attempt held by the client's limits was never sent and
	// does not count. Do sets it on every return, errors included.
	Attempts int
}

// Do sends a request: method, the escaped path below the host (e.g.
// /api/public/traces/abc) and the query, with Basic auth, under the client's
// per-call deadline. It returns the response JSON for a 2xx answer and an
// *APIError otherwise, or ErrResponseTooLarge, with the Response's Status
// set, for a 2xx body above MaxResponseBytes. A request that got no answer returns an error wrapping
// ErrUntrustedCertificate, ErrCertificateRejected, ErrNetwork, ErrTimeout or
// ErrCanceled. A request held by the client's rate limit or concurrency cap
// past the deadline is never sent and returns ErrThrottled, which wraps
// ErrTimeout; a held retry returns the failure that caused it instead. Every
// attempt, retries included, waits on both limits. A GET is retried within the deadline: twice with exponential
// backoff and jitter on a 5xx or a network failure, once after Retry-After on
// a 429 (see retries.next). Other methods are never retried: they are not
// idempotent.
func (c *Client) Do(ctx context.Context, method, escapedPath string, query url.Values) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var r retries
	var last error // the failure that caused the current retry
	attempts := 0
	for {
		resp, sent, err := c.attempt(ctx, method, escapedPath, query, true)
		if sent {
			attempts++
		}
		if last != nil && errors.Is(err, ErrThrottled) {
			// The retry never left: Langfuse's own answer is the useful error.
			return Response{Attempts: attempts}, last
		}
		wait, retry := r.next(method, err)
		if !retry || !fits(ctx, wait) || c.wait(ctx, wait) != nil {
			resp.Attempts = attempts
			return resp, err
		}
		last = err
	}
}

// attempt makes one attempt of Do, within the client's limits; the request
// carries the key pair only when authenticated is true. sent is false when
// the limits held the request, which then never left.
func (c *Client) attempt(ctx context.Context, method, escapedPath string, query url.Values, authenticated bool) (resp Response, sent bool, err error) {
	release, err := c.acquire(ctx)
	if err != nil {
		return Response{}, false, err
	}
	defer release()
	resp, err = c.send(ctx, method, escapedPath, query, authenticated)
	return resp, true, err
}

// errNotJSON marks a 2xx answer whose body is not JSON.
var errNotJSON = errors.New("response is not JSON")

// send sends one request and reads its answer.
func (c *Client) send(ctx context.Context, method, escapedPath string, query url.Values, authenticated bool) (Response, error) {
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
	if authenticated {
		req.SetBasicAuth(c.keys.reveal())
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("send request: %w", classifySend(err))
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, MaxResponseBytes)) // drain so the connection is reused
		_ = resp.Body.Close()                                                   // nothing useful to do on a close error
	}()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		if err != nil {
			return Response{}, fmt.Errorf("read error response: %w", err)
		}
		return Response{}, newAPIError(resp.StatusCode, resp.Header, body)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", classify(err))
	}
	if len(body) > MaxResponseBytes {
		return Response{Status: resp.StatusCode}, ErrResponseTooLarge // the status tells what Langfuse answered
	}
	if !json.Valid(body) {
		return Response{}, errNotJSON
	}
	return Response{Status: resp.StatusCode, Body: body}, nil
}
