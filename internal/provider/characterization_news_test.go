package provider

// Characterization tests locking the PRE-CHANGE behaviour of Longbridge.News.
//
// Purpose: BUG-01 makes a Longbridge rate-limit answer (HTTP 429 / code 429003, "minimum 0.02 s
// between calls") retry with backoff instead of surfacing immediately as 502 provider_error, and
// gives news the short-TTL cache history/intraday already have. These tests pin every other
// observable behaviour of the news path so the fix cannot silently change it.
// Group selection (Go has no tag mechanism in this repo, so the group is name-based):
//
//	go test ./... -run TestCharacterization
//
// Baseline: actually run on the unchanged tree at commit f34a290 before any modification
// (evidence/baseline-characterization-data.log).
//
// Changed on purpose, at the requester's written instruction, and pinned elsewhere:
//   - the number of upstream News calls made for a rate-limited symbol. Pre-change it was exactly
//     1 and the caller got the wrapped 429 (evidence/baseline-news-ratelimit-probe.log); round 1
//     made it 2 (one 100 ms retry); round 2 makes it 4 (100/300/900 ms ladder), because human
//     acceptance rejected round 1 — a saturated window outlasted a single retry (work item 10009
//     comment 10074, qoder-terminal-web backlog commit 50e0cec). news_retry_test.go pins the count.
//   - whether Cached reuses a News result. Pre-change every Cached.News call reached the underlying
//     provider; round 1 added the short-TTL news cache with in-flight coalescing that acceptance
//     criterion 2 requires, pinned in cache_test.go. Round 2 keeps it unchanged.
//
// Still locked: a non-rate-limit upstream failure is called exactly once and surfaced wrapped with
// "longbridge news <symbol>: " context, an exhausted rate-limit ladder keeps the same wrapped 429
// detail, an empty symbol never reaches the upstream, and the item mapping (source, symbols, UTC
// timestamp, limit, skipped items) is unchanged.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/longbridge/openapi-go/content"
	lbhttp "github.com/longbridge/openapi-go/http"
)

// charNewsAPI is a newsAPI fake that returns errs[i] on the i-th call and succeeds afterwards, so a
// test can count exactly how many upstream calls one News request made.
type charNewsAPI struct {
	items []*content.NewsItem
	errs  []error
	calls int
	asked []string
}

func (f *charNewsAPI) News(_ context.Context, symbol string) ([]*content.NewsItem, error) {
	n := f.calls
	f.calls++
	f.asked = append(f.asked, symbol)
	if n < len(f.errs) {
		return nil, f.errs[n]
	}
	return f.items, nil
}

// charRateLimitError is the answer Longbridge gives when a burst of news calls arrives too fast.
func charRateLimitError() error {
	return &lbhttp.ApiError{
		HttpStatus: 429,
		Code:       429003,
		Message:    "request rate limit: minimum 0.02s between calls",
	}
}

func TestCharacterizationLongbridgeNewsMappingIsUnchanged(t *testing.T) {
	published := time.Date(2026, 9, 25, 6, 30, 0, 0, time.FixedZone("HKT", 8*3600))
	up := &charNewsAPI{items: []*content.NewsItem{
		nil,
		{Id: "no-url", Title: "dropped", PublishedAt: published},
		{Id: "a", Title: "First", Description: "Summary A", Url: "https://example.com/a", PublishedAt: published},
		{Id: "b", Title: "Second", Description: "Summary B", Url: "https://example.com/b", PublishedAt: published},
	}}
	lb, _ := newPacedLongbridge(up)

	items, err := lb.News(context.Background(), "700.HK", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2 (nil and url-less items are skipped)", len(items))
	}
	if items[0].ID != "a" || items[1].ID != "b" {
		t.Errorf("order changed: %s, %s", items[0].ID, items[1].ID)
	}
	first := items[0]
	if first.Headline != "First" || first.Summary != "Summary A" || first.URL != "https://example.com/a" {
		t.Errorf("unexpected mapping: %+v", first)
	}
	if first.Source != "Longbridge" {
		t.Errorf("source = %q, want %q", first.Source, "Longbridge")
	}
	if len(first.Symbols) != 1 || first.Symbols[0] != "700.HK" {
		t.Errorf("symbols = %v, want [700.HK]", first.Symbols)
	}
	if want := time.Date(2026, 9, 24, 22, 30, 0, 0, time.UTC); !first.PublishedAt.Equal(want) {
		t.Errorf("publishedAt = %s, want %s (UTC)", first.PublishedAt, want)
	}
	if up.calls != 1 {
		t.Errorf("upstream calls = %d, want 1", up.calls)
	}
}

func TestCharacterizationLongbridgeNewsLimitIsAppliedAfterSkipping(t *testing.T) {
	up := &charNewsAPI{items: []*content.NewsItem{
		{Id: "no-url", Title: "dropped", Url: ""},
		{Id: "a", Url: "https://example.com/a"},
		{Id: "b", Url: "https://example.com/b"},
	}}
	lb, _ := newPacedLongbridge(up)

	items, err := lb.News(context.Background(), "9988.HK", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "a" {
		t.Errorf("got %+v, want exactly the first item with a url", items)
	}
}

func TestCharacterizationLongbridgeNewsEmptySymbolNeverCallsUpstream(t *testing.T) {
	up := &charNewsAPI{}
	lb, _ := newPacedLongbridge(up)

	_, err := lb.News(context.Background(), "", 5)
	if !errors.Is(err, ErrSymbolRequired) {
		t.Errorf("want ErrSymbolRequired, got %v", err)
	}
	if up.calls != 0 {
		t.Errorf("upstream calls = %d, want 0", up.calls)
	}
}

// A failure that is not a rate limit must stay a single upstream call: the retry BUG-01 adds is
// scoped to the rate-limit answer, so an ordinary upstream error must not become two.
func TestCharacterizationLongbridgeNewsNonRateLimitFailureIsNotRetried(t *testing.T) {
	upstream := errors.New("connection reset by peer")
	up := &charNewsAPI{errs: []error{upstream, upstream}}
	lb, _ := newPacedLongbridge(up)

	_, err := lb.News(context.Background(), "700.HK", 5)
	if err == nil {
		t.Fatal("want the upstream error")
	}
	if !errors.Is(err, upstream) {
		t.Errorf("error %v does not wrap the upstream failure", err)
	}
	if want := "longbridge news 700.HK: connection reset by peer"; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
	if up.calls != 1 {
		t.Errorf("upstream calls = %d, want 1 (a non-rate-limit failure is not retried)", up.calls)
	}
}

// The rate-limit answer must keep its wrapping and stay identifiable to the caller; only the number
// of attempts changes with BUG-01 (4 since round 2, pinned in news_retry_test.go), not the error the
// caller eventually sees. The fake therefore refuses every attempt of the ladder.
func TestCharacterizationLongbridgeNewsRateLimitStaysWrapped(t *testing.T) {
	up := &charNewsAPI{errs: []error{
		charRateLimitError(), charRateLimitError(), charRateLimitError(), charRateLimitError(),
	}}
	lb, _ := newPacedLongbridge(up)

	_, err := lb.News(context.Background(), "700.HK", 5)
	if err == nil {
		t.Fatal("want the rate-limit error once the attempts are exhausted")
	}
	if !errors.As(err, new(*lbhttp.ApiError)) {
		t.Errorf("error %v no longer carries the Longbridge API error", err)
	}
	var apiErr *lbhttp.ApiError
	if errors.As(err, &apiErr) && (apiErr.HttpStatus != 429 || apiErr.Code != 429003) {
		t.Errorf("api error = %+v, want httpStatus 429 code 429003", apiErr)
	}
	if want := "longbridge news 700.HK: "; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("error %q is not wrapped with %q", err, want)
	}
}
