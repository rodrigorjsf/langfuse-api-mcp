package langfuse

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Retry policy (.claude/rules/errors.md): only GETs are retried, and only
// within the per-call deadline.
const (
	maxServerErrorRetries = 2
	maxRateLimitRetries   = 1
	// baseBackoff is the first 5xx backoff; each further retry doubles it.
	baseBackoff = 250 * time.Millisecond
	// maxRetryAfterSeconds bounds a Retry-After value: a day is longer than
	// any Langfuse rate-limit window.
	maxRetryAfterSeconds = 24 * 60 * 60
)

// retries counts the retries one Do call has made.
type retries struct {
	serverErrors, rateLimits int
}

// next reports whether the failed attempt err is retried, and after how long.
func (r *retries) next(method string, err error) (time.Duration, bool) {
	var apiErr *APIError
	if method != http.MethodGet || !errors.As(err, &apiErr) {
		return 0, false
	}
	switch {
	case apiErr.Status == http.StatusTooManyRequests:
		if r.rateLimits >= maxRateLimitRetries || apiErr.RetryAfter <= 0 {
			return 0, false
		}
		r.rateLimits++
		return apiErr.RetryAfter, true
	case apiErr.Status >= 500:
		if r.serverErrors >= maxServerErrorRetries {
			return 0, false
		}
		backoff := baseBackoff << r.serverErrors
		r.serverErrors++
		return jitter(backoff), true
	}
	return 0, false
}

// jitter returns a random duration in [d/2, d], so that clients failing
// together do not retry together.
func jitter(d time.Duration) time.Duration {
	half := d / 2
	return half + rand.N(half+1) //nolint:gosec // G404: backoff jitter, not a secret
}

// fits reports whether waiting d still leaves time before ctx's deadline.
func fits(ctx context.Context, d time.Duration) bool {
	deadline, ok := ctx.Deadline()
	return !ok || time.Until(deadline) > d
}

// sleep waits for d or until ctx is done.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retryAfter parses a Retry-After header: delay seconds or an HTTP date.
// It returns 0 when the header is absent, invalid or in the past.
func retryAfter(header http.Header, now time.Time) time.Duration {
	v := strings.TrimSpace(header.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		// Clamp before converting so a huge value cannot overflow.
		return time.Duration(min(max(secs, 0), maxRetryAfterSeconds)) * time.Second
	}
	if at, err := http.ParseTime(v); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}
