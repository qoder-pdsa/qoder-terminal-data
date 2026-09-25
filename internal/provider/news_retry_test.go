package provider

// BUG-01: a bare `N` fans out one news request per symbol of the first watchlist group, and
// Longbridge answers such a burst with HTTP 429 / code 429003 ("minimum 0.02 s between calls"),
// which the API layer maps to 502 provider_error.
//
// Round 1 retried a rate-limited call once after 100 ms. Human acceptance rejected that
// (work item comment 10074): after ~40 news calls in a minute a cold 6-parallel burst still
// returned two 502s, because one retry cannot outlast a saturated window. Round 2 therefore waits
// 100 / 300 / 900 ms (+ jitter) and gives up only after four attempts; the pacing that stops the
// burst from being refused at all is covered in news_pacer_test.go. Timing is injected there, so
// none of these tests sleeps for real.
//
// The news fake (charNewsAPI) and the rate-limit error are shared with
// characterization_news_test.go, which pins the behaviour this rework must not change.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/longbridge/openapi-go/content"
	lbhttp "github.com/longbridge/openapi-go/http"
)

func TestLongbridgeNewsRetriesRateLimitWithBackoff(t *testing.T) {
	up := &charNewsAPI{
		items: newsItems("a", "b"),
		errs:  []error{charRateLimitError()},
	}
	lb, fc := newPacedLongbridge(up)

	items, err := lb.News(context.Background(), "700.HK", 5)

	if err != nil {
		t.Fatalf("the retry should have succeeded, got %v", err)
	}
	if up.calls != 2 {
		t.Errorf("upstream calls = %d, want 2 (the rate-limit answer is retried)", up.calls)
	}
	if len(items) != 2 || items[0].ID != "a" || items[1].ID != "b" {
		t.Errorf("items = %+v, want the two articles of the retried call", items)
	}
	if waits := fc.recorded(); !slices.Equal(waits, []time.Duration{100 * time.Millisecond}) {
		t.Errorf("waits = %v, want the ladder's first rung of 100ms", waits)
	}
}

func TestLongbridgeNewsGivesUpAfterThreeRetries(t *testing.T) {
	up := &charNewsAPI{errs: []error{
		charRateLimitError(), charRateLimitError(), charRateLimitError(), charRateLimitError(),
	}}
	lb, fc := newPacedLongbridge(up)

	_, err := lb.News(context.Background(), "700.HK", 5)
	if err == nil {
		t.Fatal("want the rate-limit error once the ladder is exhausted")
	}
	if up.calls != 4 {
		t.Errorf("upstream calls = %d, want exactly 4 (one attempt plus three retries, no loop)", up.calls)
	}
	want := []time.Duration{100 * time.Millisecond, 300 * time.Millisecond, 900 * time.Millisecond}
	if waits := fc.recorded(); !slices.Equal(waits, want) {
		t.Errorf("waits = %v, want the 100/300/900ms ladder %v", waits, want)
	}
	var apiErr *lbhttp.ApiError
	if !errors.As(err, &apiErr) || apiErr.HttpStatus != 429 || apiErr.Code != 429003 {
		t.Errorf("error %v lost the upstream rate-limit detail the API layer maps to 502", err)
	}
	if prefix := "longbridge news 700.HK: "; !strings.HasPrefix(err.Error(), prefix) {
		t.Errorf("error %q is not wrapped with %q", err, prefix)
	}
}

// A caller that goes away must not be kept waiting by a backoff.
func TestLongbridgeNewsBackoffStopsOnCancelledContext(t *testing.T) {
	up := &charNewsAPI{errs: []error{charRateLimitError(), charRateLimitError()}}
	lb, fc := newPacedLongbridge(up)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := lb.News(ctx, "700.HK", 5)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if up.calls != 1 {
		t.Errorf("upstream calls = %d, want 1 (no retry after the caller left)", up.calls)
	}
	if waits := fc.recorded(); len(waits) != 0 {
		t.Errorf("waits = %v, want none: a cancelled caller never waits out a backoff", waits)
	}
}

func newsItems(ids ...string) []*content.NewsItem {
	items := make([]*content.NewsItem, 0, len(ids))
	for _, id := range ids {
		items = append(items, &content.NewsItem{
			Id: id, Title: "headline " + id, Url: "https://example.com/" + id,
			PublishedAt: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC),
		})
	}
	return items
}
