package provider

import (
	"context"
	"sync"
	"time"
)

// fetchTimeout bounds the shared upstream fetch, which is detached from any single caller's context.
const fetchTimeout = 15 * time.Second

// Cached wraps a Provider so that concurrent and repeated calls for the same key share one upstream
// fetch and reuse the result for a TTL. The graph panel asks for history plus one indicator per window,
// `ASK compare` opens several panels at once, and every Q panel re-polls its intraday line, so
// without this the same data was requested from Longbridge in bursts and hit its rate limit.
type Cached struct {
	Provider
	history  *flightCache[historyKey, []Candle]
	intraday *flightCache[string, Intraday]
}

type historyKey struct {
	symbol string
	days   int
}

// NewCached wraps p; History results are reused for historyTTL, Intraday results for intradayTTL.
func NewCached(p Provider, historyTTL, intradayTTL time.Duration) *Cached {
	return &Cached{
		Provider: p,
		history:  newFlightCache[historyKey, []Candle](historyTTL),
		intraday: newFlightCache[string, Intraday](intradayTTL),
	}
}

// History implements Provider; callers get a copy so the cached slice is never mutated.
func (c *Cached) History(ctx context.Context, symbol string, days int) ([]Candle, error) {
	candles, err := c.history.get(ctx, historyKey{symbol, days}, func(ctx context.Context) ([]Candle, error) {
		return c.Provider.History(ctx, symbol, days)
	})
	if err != nil {
		return nil, err
	}
	return append([]Candle(nil), candles...), nil
}

// Intraday implements Provider with a short TTL, since the line gains a point every minute.
func (c *Cached) Intraday(ctx context.Context, symbol string) (Intraday, error) {
	in, err := c.intraday.get(ctx, symbol, func(ctx context.Context) (Intraday, error) {
		return c.Provider.Intraday(ctx, symbol)
	})
	if err != nil {
		return Intraday{}, err
	}
	return Intraday{Symbol: in.Symbol, Currency: in.Currency, PrevClose: in.PrevClose, Points: append([]IntradayPoint(nil), in.Points...)}, nil
}

// flightCache coalesces concurrent fetches per key and keeps successful results for ttl.
type flightCache[K comparable, V any] struct {
	ttl     time.Duration
	now     func() time.Time
	mu      sync.Mutex
	entries map[K]*flightEntry[V]
}

// flightEntry is one in-flight or completed fetch; done is closed once value/err are set.
type flightEntry[V any] struct {
	done    chan struct{}
	value   V
	err     error
	expires time.Time
}

func newFlightCache[K comparable, V any](ttl time.Duration) *flightCache[K, V] {
	return &flightCache[K, V]{ttl: ttl, now: time.Now, entries: map[K]*flightEntry[V]{}}
}

// get returns the cached value, waits for an in-flight fetch, or performs the fetch itself.
// Errors are never cached, so the next caller retries upstream.
func (f *flightCache[K, V]) get(ctx context.Context, key K, fetch func(context.Context) (V, error)) (V, error) {
	entry, owner := f.acquire(key)
	if owner {
		f.fetch(key, entry, fetch)
	}
	select {
	case <-entry.done:
	case <-ctx.Done():
		var zero V
		return zero, ctx.Err()
	}
	return entry.value, entry.err
}

// acquire returns the entry to wait on and whether this caller must perform the fetch.
func (f *flightCache[K, V]) acquire(key K) (*flightEntry[V], bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if entry, ok := f.entries[key]; ok && f.usable(entry) {
		return entry, false
	}
	entry := &flightEntry[V]{done: make(chan struct{})}
	f.entries[key] = entry
	return entry, true
}

// usable reports whether an entry is still in flight or completed successfully within its TTL.
func (f *flightCache[K, V]) usable(entry *flightEntry[V]) bool {
	select {
	case <-entry.done:
		return entry.err == nil && f.now().Before(entry.expires)
	default:
		return true
	}
}

// fetch performs the shared upstream call with its own timeout, detached from any caller's cancellation.
func (f *flightCache[K, V]) fetch(key K, entry *flightEntry[V], fetch func(context.Context) (V, error)) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	entry.value, entry.err = fetch(ctx)
	entry.expires = f.now().Add(f.ttl)
	close(entry.done)
	if entry.err != nil {
		f.mu.Lock()
		if f.entries[key] == entry {
			delete(f.entries, key)
		}
		f.mu.Unlock()
	}
}
