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
	cached := NewCached(up, time.Minute, 10*time.Second, 30*time.Second)

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
	cached := NewCached(up, time.Minute, 10*time.Second, 30*time.Second)
	cached.history.now = func() time.Time { return now }
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
	cached := NewCached(up, time.Minute, 10*time.Second, 30*time.Second)
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
	cached := NewCached(up, time.Minute, 10*time.Second, 30*time.Second)
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
	cached := NewCached(newCounting(), time.Minute, 10*time.Second, 30*time.Second)
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
	cached := NewCached(up, time.Minute, 10*time.Second, 30*time.Second)
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
	cached := NewCached(newCounting(), time.Minute, 10*time.Second, 30*time.Second)
	q, err := cached.Quote(context.Background(), "700.HK")
	if err != nil {
		t.Fatal(err)
	}
	if q.Symbol != "700.HK" {
		t.Errorf("symbol = %q", q.Symbol)
	}
}

func (c *countingProvider) Intraday(ctx context.Context, symbol string) (Intraday, error) {
	c.calls.Add(1)
	return c.Provider.Intraday(ctx, symbol)
}

func TestCachedIntradayHasItsOwnShortTTL(t *testing.T) {
	up := newCounting()
	now := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)
	cached := NewCached(up, time.Minute, 10*time.Second, 30*time.Second)
	cached.intraday.now = func() time.Time { return now }
	ctx := context.Background()
	for range 3 {
		if _, err := cached.Intraday(ctx, "700.HK"); err != nil {
			t.Fatal(err)
		}
	}
	if got := up.calls.Load(); got != 1 {
		t.Fatalf("upstream calls within TTL = %d, want 1", got)
	}
	now = now.Add(11 * time.Second)
	if _, err := cached.Intraday(ctx, "700.HK"); err != nil {
		t.Fatal(err)
	}
	if got := up.calls.Load(); got != 2 {
		t.Errorf("upstream calls after 11s = %d, want 2", got)
	}
	if _, err := cached.History(ctx, "700.HK", 63); err != nil {
		t.Fatal(err)
	}
	if got := up.calls.Load(); got != 3 {
		t.Errorf("history and intraday must not share cache entries; calls = %d", got)
	}
}

// BUG-01: a bare `N` asks for one news feed per watchlist symbol, and nothing reused those results,
// so every keystroke re-issued the whole burst and Longbridge answered 429. These tests express the
// target behaviour: news is cached and coalesced like history and intraday.

func (c *countingProvider) News(ctx context.Context, symbol string, limit int) ([]NewsItem, error) {
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
	return c.Provider.News(ctx, symbol, limit)
}

func TestCachedNewsCoalescesConcurrentCalls(t *testing.T) {
	up := newCounting()
	up.started = make(chan struct{}, 10)
	up.release = make(chan struct{})
	cached := NewCached(up, time.Minute, 10*time.Second, 30*time.Second)

	const callers = 6
	results := make([][]NewsItem, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = cached.News(context.Background(), "700.HK", 10)
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
		if len(results[i]) != 10 {
			t.Errorf("caller %d: %d items, want 10", i, len(results[i]))
		}
	}
}

func TestCachedNewsServesRepeatedCallsFromCache(t *testing.T) {
	up := newCounting()
	cached := NewCached(up, time.Minute, 10*time.Second, 30*time.Second)
	ctx := context.Background()
	for range 3 {
		items, err := cached.News(ctx, "700.HK", 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 10 {
			t.Fatalf("%d items, want 10", len(items))
		}
	}
	if got := up.calls.Load(); got != 1 {
		t.Errorf("upstream calls = %d, want 1 (repeated feeds must be reused, not re-requested)", got)
	}
}

func TestCachedNewsKeysBySymbolAndLimit(t *testing.T) {
	up := newCounting()
	cached := NewCached(up, time.Minute, 10*time.Second, 30*time.Second)
	ctx := context.Background()
	for _, k := range []struct {
		symbol string
		limit  int
	}{{"700.HK", 10}, {"9988.HK", 10}, {"700.HK", 5}, {"700.HK", 10}} {
		if _, err := cached.News(ctx, k.symbol, k.limit); err != nil {
			t.Fatal(err)
		}
	}
	if got := up.calls.Load(); got != 3 {
		t.Errorf("upstream calls = %d, want 3 (one per distinct symbol/limit)", got)
	}
}

func TestCachedNewsDoesNotCacheErrors(t *testing.T) {
	boom := errors.New("request rate limit")
	up := newCounting()
	up.errs = []error{boom}
	cached := NewCached(up, time.Minute, 10*time.Second, 30*time.Second)
	ctx := context.Background()

	if _, err := cached.News(ctx, "700.HK", 10); !errors.Is(err, boom) {
		t.Fatalf("first call: want %v, got %v", boom, err)
	}
	items, err := cached.News(ctx, "700.HK", 10)
	if err != nil {
		t.Fatalf("second call should retry upstream: %v", err)
	}
	if len(items) != 10 || up.calls.Load() != 2 {
		t.Errorf("items=%d calls=%d, want 10 and 2", len(items), up.calls.Load())
	}
}

func TestCachedNewsReturnsCopies(t *testing.T) {
	cached := NewCached(newCounting(), time.Minute, 10*time.Second, 30*time.Second)
	ctx := context.Background()
	first, err := cached.News(ctx, "700.HK", 10)
	if err != nil {
		t.Fatal(err)
	}
	first[0].URL = "https://example.com/mutated"
	second, err := cached.News(ctx, "700.HK", 10)
	if err != nil {
		t.Fatal(err)
	}
	if second[0].URL == "https://example.com/mutated" {
		t.Error("mutating a returned slice leaked into the cache")
	}
}

func TestCachedNewsHasItsOwnTTL(t *testing.T) {
	up := newCounting()
	now := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)
	cached := NewCached(up, time.Minute, 10*time.Second, 30*time.Second)
	cached.news.now = func() time.Time { return now }
	ctx := context.Background()
	for range 3 {
		if _, err := cached.News(ctx, "700.HK", 10); err != nil {
			t.Fatal(err)
		}
	}
	if got := up.calls.Load(); got != 1 {
		t.Fatalf("upstream calls within TTL = %d, want 1", got)
	}
	now = now.Add(31 * time.Second)
	if _, err := cached.News(ctx, "700.HK", 10); err != nil {
		t.Fatal(err)
	}
	if got := up.calls.Load(); got != 2 {
		t.Errorf("upstream calls after 31s = %d, want 2", got)
	}
	if _, err := cached.Intraday(ctx, "700.HK"); err != nil {
		t.Fatal(err)
	}
	if got := up.calls.Load(); got != 3 {
		t.Errorf("news and intraday must not share cache entries; calls = %d", got)
	}
}
