package provider

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingProvider records History calls and can block them until released, so tests can force overlap.
type countingProvider struct {
	Provider
	calls   atomic.Int32
	started chan struct{} // receives one value per upstream call
	release chan struct{} // upstream calls block until this is closed (nil = never block)
	errs    []error       // errs[i] is returned by the i-th call; past the end, the call succeeds
}

func newCounting() *countingProvider {
	return &countingProvider{Provider: &Mock{Now: func() time.Time { return time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC) }}}
}

func (c *countingProvider) History(ctx context.Context, symbol string, days int) ([]Candle, error) {
	n := int(c.calls.Add(1)) - 1
	if c.started != nil {
		c.started <- struct{}{}
	}
	if c.release != nil {
		<-c.release
	}
	if n < len(c.errs) && c.errs[n] != nil {
		return nil, c.errs[n]
	}
	return c.Provider.History(ctx, symbol, days)
}

func TestCachedHistoryCoalescesConcurrentCalls(t *testing.T) {
	up := newCounting()
	up.started = make(chan struct{}, 10)
	up.release = make(chan struct{})
	cached := NewCached(up, time.Minute)

	const callers = 6
	results := make([][]Candle, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = cached.History(context.Background(), "700.HK", 63)
		}()
	}
	<-up.started // exactly one upstream call must be in flight
	select {
	case <-up.started:
		t.Fatal("a second upstream call started while the first was still in flight")
	case <-time.After(50 * time.Millisecond):
	}
	close(up.release)
	wg.Wait()

	if got := up.calls.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
	for i := range callers {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if len(results[i]) != 63 {
			t.Errorf("caller %d: %d candles, want 63", i, len(results[i]))
		}
	}
}

func TestCachedHistoryServesFromCacheUntilTTL(t *testing.T) {
	up := newCounting()
	now := time.Date(2026, 9, 24, 9, 30, 0, 0, time.UTC)
	cached := NewCached(up, time.Minute)
	cached.now = func() time.Time { return now }
	ctx := context.Background()

	for range 3 {
		if _, err := cached.History(ctx, "700.HK", 63); err != nil {
			t.Fatal(err)
		}
	}
	if got := up.calls.Load(); got != 1 {
		t.Fatalf("upstream calls within TTL = %d, want 1", got)
	}

	now = now.Add(time.Minute + time.Second)
	if _, err := cached.History(ctx, "700.HK", 63); err != nil {
		t.Fatal(err)
	}
	if got := up.calls.Load(); got != 2 {
		t.Errorf("upstream calls after TTL = %d, want 2", got)
	}
}

func TestCachedHistoryKeysBySymbolAndDays(t *testing.T) {
	up := newCounting()
	cached := NewCached(up, time.Minute)
	ctx := context.Background()
	for _, k := range []struct {
		symbol string
		days   int
	}{{"700.HK", 63}, {"9988.HK", 63}, {"700.HK", 21}, {"700.HK", 63}} {
		if _, err := cached.History(ctx, k.symbol, k.days); err != nil {
			t.Fatal(err)
		}
	}
	if got := up.calls.Load(); got != 3 {
		t.Errorf("upstream calls = %d, want 3 (one per distinct symbol/days)", got)
	}
}

func TestCachedHistoryDoesNotCacheErrors(t *testing.T) {
	boom := errors.New("request rate limit")
	up := newCounting()
	up.errs = []error{boom}
	cached := NewCached(up, time.Minute)
	ctx := context.Background()

	if _, err := cached.History(ctx, "700.HK", 63); !errors.Is(err, boom) {
		t.Fatalf("first call: want %v, got %v", boom, err)
	}
	candles, err := cached.History(ctx, "700.HK", 63)
	if err != nil {
		t.Fatalf("second call should retry upstream: %v", err)
	}
	if len(candles) != 63 || up.calls.Load() != 2 {
		t.Errorf("candles=%d calls=%d, want 63 and 2", len(candles), up.calls.Load())
	}
}

func TestCachedHistoryReturnsCopies(t *testing.T) {
	cached := NewCached(newCounting(), time.Minute)
	ctx := context.Background()
	first, err := cached.History(ctx, "700.HK", 63)
	if err != nil {
		t.Fatal(err)
	}
	first[0].Volume = -1
	second, err := cached.History(ctx, "700.HK", 63)
	if err != nil {
		t.Fatal(err)
	}
	if second[0].Volume == -1 {
		t.Error("mutating a returned slice leaked into the cache")
	}
}

func TestCachedHistoryCancelledCallerDoesNotPoisonCache(t *testing.T) {
	up := newCounting()
	cached := NewCached(up, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the shared upstream fetch must not be tied to one caller's context
	_, _ = cached.History(ctx, "700.HK", 63)

	candles, err := cached.History(context.Background(), "700.HK", 63)
	if err != nil {
		t.Fatalf("next caller after a cancelled one: %v", err)
	}
	if len(candles) != 63 || up.calls.Load() != 1 {
		t.Errorf("candles=%d calls=%d, want 63 candles served from the fetch the cancelled caller started", len(candles), up.calls.Load())
	}
}

func TestCachedPassesThroughQuote(t *testing.T) {
	cached := NewCached(newCounting(), time.Minute)
	q, err := cached.Quote(context.Background(), "700.HK")
	if err != nil {
		t.Fatal(err)
	}
	if q.Symbol != "700.HK" {
		t.Errorf("symbol = %q", q.Symbol)
	}
}
