package provider

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"
)

// newsPaceGap is the interval Longbridge asks for between content calls: a burst that arrives
// faster is refused with HTTP 429 / code 429003, "minimum 0.02s between calls".
const newsPaceGap = 20 * time.Millisecond

// newsRetryBackoffs are the waits before the first, second and third retry of a rate-limited news
// call. A fourth rate-limit answer is surfaced to the caller, which maps it to 502 provider_error.
// One retry was not enough once the window was already saturated by unrelated traffic — an ASK
// news search while a bare `N` loads, or two terminals — so the waits form a ladder.
var newsRetryBackoffs = []time.Duration{
	100 * time.Millisecond,
	300 * time.Millisecond,
	900 * time.Millisecond,
}

// clock is the pacer's time source, so a test can drive the pace gap and the backoff ladder
// without ever sleeping for real.
type clock interface {
	Now() time.Time
	// Wait blocks until `until` and returns ctx's error if the caller goes away first. It returns
	// at once, without consulting ctx, when `until` has already passed: a caller that is ready to
	// go must not be turned away.
	Wait(ctx context.Context, until time.Time) error
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) Wait(ctx context.Context, until time.Time) error {
	d := time.Until(until)
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// newsPacer keeps upstream news calls at least newsPaceGap apart and retries a rate-limit answer
// on the backoff ladder. One pacer serves the whole process — main builds a single Longbridge
// provider, and the interval Longbridge enforces is per credential, not per request.
type newsPacer struct {
	gap      time.Duration
	backoffs []time.Duration
	clock    clock
	jitter   func(time.Duration) time.Duration

	mu   sync.Mutex
	next time.Time // earliest moment the next upstream call may start
}

func newNewsPacer() *newsPacer {
	return &newsPacer{gap: newsPaceGap, backoffs: newsRetryBackoffs, clock: systemClock{}, jitter: jitterUp}
}

// fetch runs call and repeats it after a backoff while Longbridge answers with a rate limit. The
// slot is held across the call itself, not only reserved before it: spacing just the starts would
// still let a six-symbol burst overlap upstream and arrive bunched. Any other error, and a rate
// limit that outlives the ladder, is returned unwrapped for the caller to give context.
func (p *newsPacer) fetch(ctx context.Context, call func(context.Context) error) error {
	for attempt := 0; ; attempt++ {
		err := p.paced(ctx, call)
		if !isRateLimited(err) {
			return err
		}
		if attempt >= len(p.backoffs) {
			return err
		}
		if err := p.clock.Wait(ctx, p.clock.Now().Add(p.jitter(p.backoffs[attempt]))); err != nil {
			return err
		}
	}
}

// paced waits for this caller's slot and runs call while holding it.
func (p *newsPacer) paced(ctx context.Context, call func(context.Context) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	slot := p.next
	if now := p.clock.Now(); slot.Before(now) {
		slot = now
	}
	p.next = slot.Add(p.gap)
	if err := p.clock.Wait(ctx, slot); err != nil {
		p.next = slot // this caller left, so the slot is free again
		return err
	}
	return call(ctx)
}

// jitterUp adds up to a quarter of d, so callers that hit the same rate limit together do not
// retry in lockstep.
func jitterUp(d time.Duration) time.Duration {
	return d + rand.N(d/4)
}
