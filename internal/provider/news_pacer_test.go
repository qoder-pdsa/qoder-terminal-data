package provider

// BUG-01 round 2: Longbridge refuses content calls that arrive less than 0.02 s apart with HTTP
// 429 / code 429003, and the single retry delivered in round 1 still surfaced 502 provider_error
// once the window was saturated. These tests drive the pacer and the retry ladder on an injected
// clock, so no test sleeps for real.
//
// The retry ladder's observable behaviour is pinned in news_retry_test.go; this file covers the
// pacing that keeps a burst from being refused in the first place.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/longbridge/openapi-go/content"
)

// fakeClock moves time forward only when the pacer asks it to wait, and records every wait.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Wait mirrors systemClock: with nothing to wait for it returns at once and never consults ctx.
func (c *fakeClock) Wait(ctx context.Context, until time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.now.Before(until) {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	c.waits = append(c.waits, until.Sub(c.now))
	c.now = until
	return nil
}

func (c *fakeClock) recorded() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.waits...)
}

// noJitter keeps the ladder's waits exact for assertions; TestNewsRetryLadderAppliesJitter covers
// the production jitter.
func noJitter(d time.Duration) time.Duration { return d }

// newPacedLongbridge wires up for news calls that are paced and retried on a virtual clock.
func newPacedLongbridge(up newsAPI) (*Longbridge, *fakeClock) {
	fc := newFakeClock()
	return &Longbridge{news: up, pacer: &newsPacer{
		gap: newsPaceGap, backoffs: newsRetryBackoffs, clock: fc, jitter: noJitter,
	}}, fc
}

// intervalNewsFake refuses a call that arrives less than minGap after the previous one, which is
// how Longbridge answers a burst.
type intervalNewsFake struct {
	clock  clock
	minGap time.Duration
	items  []*content.NewsItem

	mu       sync.Mutex
	times    []time.Time
	refusals int
}

func (f *intervalNewsFake) News(_ context.Context, _ string) ([]*content.NewsItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	at := f.clock.Now()
	tooSoon := len(f.times) > 0 && at.Sub(f.times[len(f.times)-1]) < f.minGap
	f.times = append(f.times, at)
	if tooSoon {
		f.refusals++
		return nil, charRateLimitError()
	}
	return f.items, nil
}

func (f *intervalNewsFake) snapshot() (times []time.Time, refusals int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Time(nil), f.times...), f.refusals
}

// A bare `N` fires one news call per symbol of the first watchlist group at once. Paced, the whole
// burst must get through without a single rate-limit refusal.
func TestNewsBurstIsPacedIntoSixSuccesses(t *testing.T) {
	fc := newFakeClock()
	up := &intervalNewsFake{clock: fc, minGap: newsPaceGap, items: newsItems("a", "b")}
	lb := &Longbridge{news: up, pacer: &newsPacer{
		gap: newsPaceGap, backoffs: newsRetryBackoffs, clock: fc, jitter: noJitter,
	}}

	symbols := []string{"ORCL.US", "ADBE.US", "ARM.US", "MSFT.US", "SNDK.US", "ACHR.US"}
	errs := make([]error, len(symbols))
	counts := make([]int, len(symbols))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, symbol := range symbols {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			items, err := lb.News(context.Background(), symbol, 10)
			errs[i], counts[i] = err, len(items)
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("news %s: %v, want a successful feed", symbols[i], err)
		}
		if counts[i] != 2 {
			t.Errorf("news %s returned %d items, want 2", symbols[i], counts[i])
		}
	}
	times, refusals := up.snapshot()
	if refusals != 0 {
		t.Errorf("upstream refused %d calls as arriving too soon, want 0", refusals)
	}
	if len(times) != len(symbols) {
		t.Fatalf("upstream calls = %d, want %d (a paced burst needs no retry)", len(times), len(symbols))
	}
	for i := 1; i < len(times); i++ {
		if gap := times[i].Sub(times[i-1]); gap < newsPaceGap {
			t.Errorf("calls %d and %d were %s apart, want at least %s", i-1, i, gap, newsPaceGap)
		}
	}
}

// The ladder's waits are spaced so that six terminals hitting the same limit do not retry in
// lockstep; the production pacer therefore jitters every rung.
func TestNewsRetryLadderAppliesJitter(t *testing.T) {
	up := &charNewsAPI{errs: []error{
		charRateLimitError(), charRateLimitError(), charRateLimitError(), charRateLimitError(),
	}}
	fc := newFakeClock()
	lb := &Longbridge{news: up, pacer: &newsPacer{
		gap: newsPaceGap, backoffs: newsRetryBackoffs, clock: fc, jitter: jitterUp,
	}}

	if _, err := lb.News(context.Background(), "700.HK", 5); err == nil {
		t.Fatal("want the rate-limit error once the ladder is exhausted")
	}
	if up.calls != 4 {
		t.Errorf("upstream calls = %d, want exactly 4 (one attempt plus three retries)", up.calls)
	}
	waits := fc.recorded()
	if len(waits) != len(newsRetryBackoffs) {
		t.Fatalf("waits = %v, want one per rung of the ladder", waits)
	}
	for i, base := range newsRetryBackoffs {
		if waits[i] < base || waits[i] > base+base/4 {
			t.Errorf("retry %d waited %s, want within [%s, %s]", i+1, waits[i], base, base+base/4)
		}
	}
}

func TestJitterUpStaysWithinAQuarterOfTheBackoff(t *testing.T) {
	for _, base := range newsRetryBackoffs {
		sawJitter := false
		for range 500 {
			got := jitterUp(base)
			if got < base || got > base+base/4 {
				t.Fatalf("jitterUp(%s) = %s, want within [%s, %s]", base, got, base, base+base/4)
			}
			sawJitter = sawJitter || got > base
		}
		if !sawJitter {
			t.Errorf("jitterUp(%s) never added jitter in 500 samples", base)
		}
	}
}
