package provider

import (
	"context"
	"sync"
	"time"
)

// fetchTimeout bounds the shared upstream fetch, which is detached from any single caller's context.
const fetchTimeout = 15 * time.Second

// Cached wraps a Provider so that concurrent and repeated History calls for the same (symbol, days)
// share one upstream fetch and reuse the result for a TTL. The graph panel asks for history plus
// one indicator per window, and `ASK compare` opens several panels at once, so without this the
// same daily candles were requested from Longbridge six times in a burst and hit its rate limit.
type Cached struct {
	Provider
	ttl     time.Duration
	now     func() time.Time
	mu      sync.Mutex
	entries map[historyKey]*historyEntry
}

type historyKey struct {
	symbol string
	days   int
}

// historyEntry is one in-flight or completed fetch; done is closed once candles/err are set.
type historyEntry struct {
	done    chan struct{}
	candles []Candle
	err     error
	expires time.Time
}

// NewCached wraps p; History results are reused for ttl.
func NewCached(p Provider, ttl time.Duration) *Cached {
	return &Cached{Provider: p, ttl: ttl, now: time.Now, entries: map[historyKey]*historyEntry{}}
}

// History implements Provider. Callers that arrive while a fetch is in flight wait for it instead of
// starting their own; errors are never cached, so the next caller retries upstream.
func (c *Cached) History(ctx context.Context, symbol string, days int) ([]Candle, error) {
	key := historyKey{symbol, days}
	entry, owner := c.acquire(key)
	if owner {
		c.fetch(key, entry)
	}
	select {
	case <-entry.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if entry.err != nil {
		return nil, entry.err
	}
	return append([]Candle(nil), entry.candles...), nil
}

// acquire returns the entry to wait on and whether this caller must perform the fetch.
func (c *Cached) acquire(key historyKey) (*historyEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[key]; ok && c.usable(entry) {
		return entry, false
	}
	entry := &historyEntry{done: make(chan struct{})}
	c.entries[key] = entry
	return entry, true
}

// usable reports whether an entry is still in flight or completed successfully within its TTL.
func (c *Cached) usable(entry *historyEntry) bool {
	select {
	case <-entry.done:
		return entry.err == nil && c.now().Before(entry.expires)
	default:
		return true
	}
}

// fetch performs the shared upstream call with its own timeout, detached from any caller's cancellation.
func (c *Cached) fetch(key historyKey, entry *historyEntry) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	entry.candles, entry.err = c.Provider.History(ctx, key.symbol, key.days)
	entry.expires = c.now().Add(c.ttl)
	close(entry.done)
	if entry.err != nil {
		c.mu.Lock()
		if c.entries[key] == entry {
			delete(c.entries, key)
		}
		c.mu.Unlock()
	}
}
