package langfuse

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"time"
)

// maxNetworkRetries bounds how often a read that failed on the network is sent
// again (errors rule: DNS/connect/proxy failures are retried for GET only).
const maxNetworkRetries = 2

// networkRetryBase is the first backoff delay; it doubles on each retry.
const networkRetryBase = 50 * time.Millisecond

// send sends req and returns the response or the classified transport error.
// A GET that failed on the network is sent again, at most maxNetworkRetries
// times, with exponential backoff and jitter, and never past the context's
// deadline. Other methods are never retried: they may not be idempotent.
func (c *Client) send(ctx context.Context, req *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		resp, err := c.httpClient.Do(req)
		if err == nil {
			return resp, nil
		}
		err = classify(err)
		if req.Method != http.MethodGet || !errors.Is(err, ErrNetwork) || attempt == maxNetworkRetries {
			return nil, err
		}
		delay := backoff(attempt)
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < delay {
			return nil, err // the retry would not finish in time
		}
		select {
		case <-ctx.Done():
			return nil, classify(ctx.Err())
		case <-time.After(delay):
		}
	}
}

// backoff returns the delay before retry attempt+1: networkRetryBase doubled
// per attempt, plus up to as much again of random jitter.
func backoff(attempt int) time.Duration {
	d := networkRetryBase << attempt
	return d + rand.N(d) //nolint:gosec // G404: jitter spreads retries; it protects no secret
}
