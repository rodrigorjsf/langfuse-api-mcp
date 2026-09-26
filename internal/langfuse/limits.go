package langfuse

import (
	"context"
	"errors"
	"fmt"
)

// ErrThrottled marks a request that waited on the client's own rate limit or
// concurrency cap until its deadline passed: it was never sent. The error
// wraps ErrTimeout too.
var ErrThrottled = errors.New("the request waited on the client's rate limit or concurrency cap past its deadline")

// acquire waits for the rate limit to let one more request leave, then for a
// free slot below the concurrency cap. It returns the function that frees the
// slot. A wait that cannot end before ctx's deadline is not started.
func (c *Client) acquire(ctx context.Context) (release func(), err error) {
	now := c.now()
	r := c.limiter.ReserveN(now, 1) // always OK: the burst is at least 1
	if d := r.DelayFrom(now); d > 0 {
		if !fits(ctx, d) {
			r.CancelAt(now)
			return nil, throttled(context.DeadlineExceeded)
		}
		if err := c.wait(ctx, d); err != nil {
			r.CancelAt(c.now())
			return nil, throttled(err)
		}
	}
	select {
	case c.slots <- struct{}{}:
		return func() { <-c.slots }, nil
	case <-ctx.Done():
		return nil, throttled(ctx.Err())
	}
}

// throttled is the error of a wait on the limits that ended with err: a
// passed deadline is ErrThrottled and ErrTimeout, a cancellation ErrCanceled.
func throttled(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", ErrThrottled, ErrTimeout)
	}
	return fmt.Errorf("wait on the request limits: %w", classify(err))
}
