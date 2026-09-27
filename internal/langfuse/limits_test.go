package langfuse_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rodrigorjsf/langfuse-api-mcp/internal/langfuse"
)

// Issue #37: every request waits on one shared rate limiter and a cap on the
// requests in flight. Seam: the client against an httptest.Server, with an
// injected clock so no test sleeps.

// limitsOptions returns client options for the fake Langfuse at rawURL.
func limitsOptions(t *testing.T, rawURL string) langfuse.Options {
	t.Helper()
	host, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return langfuse.Options{Host: host, Keys: langfuse.NewKeyPair("pk-lf-test", "sk-lf-test")} //nolint:gosec // G101: fake keys for a fake host
}

// countingLangfuse answers 200 {} and counts the requests it received.
func countingLangfuse(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`) // a failed write shows up as a client error in the test
	}))
	t.Cleanup(fake.Close)
	return fake, &calls
}

func TestConcurrentRequestsNeverExceedTheConcurrencyCap(t *testing.T) {
	t.Parallel()
	const capacity, requests = 3, 12
	var inFlight, most atomic.Int32
	arrived := make(chan struct{}, requests)
	release := make(chan struct{})
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := inFlight.Add(1)
		for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
		}
		arrived <- struct{}{}
		<-release
		inFlight.Add(-1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`) // a failed write shows up as a client error in the test
	}))
	t.Cleanup(fake.Close)
	opts := limitsOptions(t, fake.URL)
	opts.MaxConcurrency = capacity
	opts.RateLimit = 60000 // the rate must not be what holds the requests back
	client := langfuse.New(opts)

	var wg sync.WaitGroup
	errs := make(chan error, requests)
	for range requests {
		wg.Go(func() {
			_, err := client.Do(context.Background(), http.MethodGet, "/api/public/traces", nil, nil)
			errs <- err
		})
	}
	// Let one request finish per arrival once the cap is full, so that every
	// request reaches Langfuse while the cap stays full.
	for i := range requests {
		<-arrived
		if i >= capacity-1 {
			release <- struct{}{}
		}
	}
	for range capacity - 1 {
		release <- struct{}{}
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
	}
	if got := most.Load(); got != capacity {
		t.Fatalf("at most %d requests were in flight, want exactly the cap %d", got, capacity)
	}
}

// fakeClock is the client's clock and wait: a wait records d and moves the
// clock forward by d at once, so paced requests never sleep.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Wait(_ context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waits = append(c.waits, d)
	c.now = c.now.Add(d)
	return nil
}

func (c *fakeClock) recorded() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.waits...)
}

// pacedOptions returns options for fake with the given limits on clock.
func pacedOptions(t *testing.T, fake *httptest.Server, clock *fakeClock, perMinute, concurrency int) langfuse.Options {
	t.Helper()
	opts := limitsOptions(t, fake.URL)
	opts.RateLimit, opts.MaxConcurrency = perMinute, concurrency
	opts.Now, opts.Wait = clock.Now, clock.Wait
	return opts
}

func TestRequestsArePacedToTheRateLimit(t *testing.T) {
	t.Parallel()
	fake, calls := countingLangfuse(t)
	clock := newFakeClock()
	client := langfuse.New(pacedOptions(t, fake, clock, 60, 1)) // one request per second

	for range 3 {
		if _, err := client.Do(context.Background(), http.MethodGet, "/api/public/traces", nil, nil); err != nil {
			t.Fatalf("Do: %v", err)
		}
	}

	waits := clock.recorded()
	if calls.Load() != 3 || len(waits) != 2 || waits[0] != time.Second || waits[1] != time.Second {
		t.Fatalf("%d requests with waits %v, want 3 requests, the 2nd and 3rd each after 1s", calls.Load(), waits)
	}
}

func TestTheBurstEqualsTheConcurrencyCap(t *testing.T) {
	t.Parallel()
	fake, calls := countingLangfuse(t)
	clock := newFakeClock()
	client := langfuse.New(pacedOptions(t, fake, clock, 60, 4))

	for range 5 {
		if _, err := client.Do(context.Background(), http.MethodGet, "/api/public/traces", nil, nil); err != nil {
			t.Fatalf("Do: %v", err)
		}
	}

	waits := clock.recorded()
	if calls.Load() != 5 || len(waits) != 1 || waits[0] != time.Second {
		t.Fatalf("%d requests with waits %v, want 5 requests, only the 5th after 1s", calls.Load(), waits)
	}
}

func TestARetryWaitsOnTheRateLimitToo(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_, _ = io.WriteString(w, `{}`) // a failed write shows up as a client error in the test
	}))
	t.Cleanup(fake.Close)
	clock := newFakeClock()
	client := langfuse.New(pacedOptions(t, fake, clock, 60, 1))

	if _, err := client.Do(context.Background(), http.MethodGet, "/api/public/traces", nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}

	// The 5xx backoff (125ms to 250ms) is followed by the rest of the second
	// the rate limit asks for, so the retry leaves 1s after the first attempt.
	var total time.Duration
	for _, d := range clock.recorded() {
		total += d
	}
	// The limiter computes in floating point: allow it a microsecond short.
	if calls.Load() != 2 || total < time.Second-time.Microsecond || total > time.Second {
		t.Fatalf("%d requests after waiting %v in all (%v), want 2 requests 1s apart", calls.Load(), total, clock.recorded())
	}
}

func TestARateLimitWaitBeyondTheDeadlineReturnsAThrottledTimeoutWithoutCallingLangfuse(t *testing.T) {
	t.Parallel()
	fake, calls := countingLangfuse(t)
	clock := newFakeClock()
	opts := pacedOptions(t, fake, clock, 1, 1) // the next request may leave in a minute
	opts.RequestTimeout = 5 * time.Second
	client := langfuse.New(opts)
	if _, err := client.Do(context.Background(), http.MethodGet, "/api/public/traces", nil, nil); err != nil {
		t.Fatalf("first Do: %v", err)
	}

	_, err := client.Do(context.Background(), http.MethodGet, "/api/public/traces", nil, nil)

	if !errors.Is(err, langfuse.ErrThrottled) || !errors.Is(err, langfuse.ErrTimeout) {
		t.Fatalf("second Do error = %v, want ErrThrottled and ErrTimeout", err)
	}
	if calls.Load() != 1 || len(clock.recorded()) != 0 {
		t.Fatalf("%d requests, waits %v; want Langfuse called once and no wait", calls.Load(), clock.recorded())
	}
}

func TestACanceledRequestWaitingOnTheRateLimitReturnsCanceledWithoutCallingLangfuse(t *testing.T) {
	t.Parallel()
	fake, calls := countingLangfuse(t)
	clock := newFakeClock()
	opts := pacedOptions(t, fake, clock, 60, 1)
	opts.Wait = func(ctx context.Context, _ time.Duration) error { return context.Canceled } // canceled mid-wait
	client := langfuse.New(opts)
	if _, err := client.Do(context.Background(), http.MethodGet, "/api/public/traces", nil, nil); err != nil {
		t.Fatalf("first Do: %v", err)
	}

	_, err := client.Do(context.Background(), http.MethodGet, "/api/public/traces", nil, nil)

	if !errors.Is(err, langfuse.ErrCanceled) || errors.Is(err, langfuse.ErrThrottled) {
		t.Fatalf("second Do error = %v, want ErrCanceled, not ErrThrottled", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("%d requests, want Langfuse called once", calls.Load())
	}
}

func TestARequestWaitingForASlotPastItsDeadlineReturnsAThrottledTimeoutWithoutCallingLangfuse(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	arrived, release := make(chan struct{}), make(chan struct{})
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		arrived <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`) // a failed write shows up as a client error in the test
	}))
	t.Cleanup(fake.Close)
	client := langfuse.New(pacedOptions(t, fake, newFakeClock(), 60000, 1))
	done := make(chan error, 1)
	go func() {
		_, err := client.Do(context.Background(), http.MethodGet, "/api/public/traces", nil, nil)
		done <- err
	}()
	<-arrived // the only slot is taken
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := client.Do(expired, http.MethodGet, "/api/public/traces", nil, nil)

	close(release)
	if firstErr := <-done; firstErr != nil {
		t.Fatalf("first Do: %v", firstErr)
	}
	if !errors.Is(err, langfuse.ErrThrottled) || !errors.Is(err, langfuse.ErrTimeout) {
		t.Fatalf("second Do error = %v, want ErrThrottled and ErrTimeout", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("%d requests, want Langfuse called once", calls.Load())
	}
}

// A request queued for a slot takes its rate-limit token only once it has the
// slot, so queued requests cannot all leave together when slots free up.
func TestARequestQueuedForASlotTakesNoRateLimitTokenUntilItHasTheSlot(t *testing.T) {
	t.Parallel()
	arrived, release := make(chan struct{}), make(chan struct{})
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		arrived <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`) // a failed write shows up as a client error in the test
	}))
	t.Cleanup(fake.Close)
	clock := newFakeClock()
	// The next token is due in 10ms, well within the queued request's deadline.
	client := langfuse.New(pacedOptions(t, fake, clock, 6000, 1))
	done := make(chan error, 1)
	go func() {
		_, err := client.Do(context.Background(), http.MethodGet, "/api/public/traces", nil, nil)
		done <- err
	}()
	<-arrived // the only slot is taken, and so is the only token
	short, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Do(short, http.MethodGet, "/api/public/traces", nil, nil)

	close(release)
	if firstErr := <-done; firstErr != nil {
		t.Fatalf("first Do: %v", firstErr)
	}
	if !errors.Is(err, langfuse.ErrThrottled) {
		t.Fatalf("queued Do error = %v, want ErrThrottled", err)
	}
	if waits := clock.recorded(); len(waits) != 0 {
		t.Fatalf("the queued request waited %v on the rate limit before it had a slot, want no wait", waits)
	}
}

// A retry the rate limit cannot let out before the deadline returns the
// Langfuse answer that caused the retry, not the throttling.
func TestARetryHeldByTheRateLimitReturnsTheLangfuseErrorThatCausedIt(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"message":"Service Unavailable"}`) // a failed write shows up as a client error in the test
	}))
	t.Cleanup(fake.Close)
	opts := pacedOptions(t, fake, newFakeClock(), 1, 1) // the retry may leave in a minute
	opts.RequestTimeout = 5 * time.Second
	client := langfuse.New(opts)

	_, err := client.Do(context.Background(), http.MethodGet, "/api/public/traces", nil, nil)

	var apiErr *langfuse.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusServiceUnavailable || errors.Is(err, langfuse.ErrThrottled) {
		t.Fatalf("Do error = %v, want the 503 APIError, not a throttling", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("%d requests, want 1", calls.Load())
	}
}
