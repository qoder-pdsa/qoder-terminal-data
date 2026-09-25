package provider

// BUG-01: a bare `N` fans out one news request per symbol of the first watchlist group, and
// Longbridge answers such a burst with HTTP 429 / code 429003 ("minimum 0.02 s between calls"),
// which the API layer maps to 502 provider_error. These tests express the target behaviour: one
// backoff retry before the rate-limit answer is allowed to surface.
//
// The news fake (charNewsAPI) and the rate-limit error are shared with
// characterization_news_test.go, which pins the behaviour this fix must not change.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/longbridge/openapi-go/content"
	lbhttp "github.com/longbridge/openapi-go/http"
)

func TestLongbridgeNewsRetriesRateLimitOnceWithBackoff(t *testing.T) {
	up := &charNewsAPI{
		items: newsItems("a", "b"),
		errs:  []error{charRateLimitError()},
	}
	lb := &Longbridge{news: up}

	start := time.Now()
	items, err := lb.News(context.Background(), "700.HK", 5)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("the retry should have succeeded, got %v", err)
	}
	if up.calls != 2 {
		t.Errorf("upstream calls = %d, want 2 (the rate-limit answer is retried once)", up.calls)
	}
	if len(items) != 2 || items[0].ID != "a" || items[1].ID != "b" {
		t.Errorf("items = %+v, want the two articles of the retried call", items)
	}
	if elapsed < newsRetryBackoff {
		t.Errorf("retry waited %s, want at least the %s backoff", elapsed.Round(time.Millisecond), newsRetryBackoff)
	}
}

func TestLongbridgeNewsGivesUpAfterOneRetry(t *testing.T) {
	up := &charNewsAPI{errs: []error{charRateLimitError(), charRateLimitError(), charRateLimitError()}}
	lb := &Longbridge{news: up}

	_, err := lb.News(context.Background(), "700.HK", 5)
	if err == nil {
		t.Fatal("want the rate-limit error once the single retry is used up")
	}
	if up.calls != 2 {
		t.Errorf("upstream calls = %d, want exactly 2 (one retry, no loop)", up.calls)
	}
	var apiErr *lbhttp.ApiError
	if !errors.As(err, &apiErr) || apiErr.HttpStatus != 429 || apiErr.Code != 429003 {
		t.Errorf("error %v lost the upstream rate-limit detail the API layer maps to 502", err)
	}
	if want := "longbridge news 700.HK: "; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("error %q is not wrapped with %q", err, want)
	}
}

// A caller that goes away must not be kept waiting by the backoff.
func TestLongbridgeNewsBackoffStopsOnCancelledContext(t *testing.T) {
	up := &charNewsAPI{errs: []error{charRateLimitError(), charRateLimitError()}}
	lb := &Longbridge{news: up}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := lb.News(ctx, "700.HK", 5)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > newsRetryBackoff {
		t.Errorf("cancelled caller waited %s, want to return before the %s backoff elapses", elapsed.Round(time.Millisecond), newsRetryBackoff)
	}
	if up.calls != 1 {
		t.Errorf("upstream calls = %d, want 1 (no retry after the caller left)", up.calls)
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
