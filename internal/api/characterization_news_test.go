package api

// Characterization tests locking the PRE-CHANGE behaviour of GET /v1/news.
//
// Purpose: BUG-01 gives news the same short-TTL cache history/intraday already have and retries a
// Longbridge rate-limit answer once with backoff before it becomes a 502. These tests pin every
// other observable behaviour of the handler so the fix cannot silently change it.
// Group selection (Go has no tag mechanism in this repo, so the group is name-based):
//
//	go test ./... -run TestCharacterization
//
// Baseline: actually run on the unchanged tree at commit f34a290 before any modification
// (evidence/baseline-characterization-data.log).
//
// Deliberately NOT asserted here, because the work item explicitly changes it:
//   - a rate-limited upstream news fetch (Longbridge HTTP 429 / code 429003) reaches the client as
//     502 provider_error after exactly ONE upstream call today; acceptance criterion 2 requires one
//     backoff retry first. Its real pre-change value is recorded in
//     evidence/baseline-news-ratelimit-probe.log.
//
// The 502 provider_error mapping itself is still locked: once the retries are exhausted the client
// must see exactly the same {"code","message"} body as before, and the raw upstream error must not
// leak into it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qoder-pdsa/qoder-terminal-data/internal/provider"
)

// charNewsProvider answers News with a fixed result and counts the calls, so the handler's error
// mapping can be characterized without an upstream.
type charNewsProvider struct {
	provider.Provider
	items []provider.NewsItem
	err   error
	calls int
}

func (p *charNewsProvider) News(context.Context, string, int) ([]provider.NewsItem, error) {
	p.calls++
	return p.items, p.err
}

func newCharNewsServer(p provider.Provider) *httptest.Server {
	s := &Server{Provider: p, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return httptest.NewServer(s.Routes())
}

// charBody decodes an error response into the contract's {"code","message"} shape.
func charBody(t *testing.T, resp *http.Response) map[string]string {
	t.Helper()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]string
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("body is not a JSON object: %s (%v)", raw, err)
	}
	return body
}

func TestCharacterizationNewsUpstreamFailureIs502ProviderError(t *testing.T) {
	up := &charNewsProvider{err: errors.New("longbridge news 700.HK: upstream exploded")}
	ts := newCharNewsServer(up)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/news?symbol=700.HK")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
	body := charBody(t, resp)
	if body["code"] != "provider_error" {
		t.Errorf("code = %q, want %q", body["code"], "provider_error")
	}
	// The raw upstream error must never reach the client.
	if body["message"] != "upstream data provider failed" {
		t.Errorf("message = %q, want %q", body["message"], "upstream data provider failed")
	}
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("content-type = %q, want application/json", resp.Header.Get("Content-Type"))
	}
}

func TestCharacterizationNewsErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantMsg    string
	}{
		{
			name: "not found is 404", err: provider.ErrNotFound,
			wantStatus: http.StatusNotFound, wantCode: "not_found", wantMsg: "symbol not found",
		},
		{
			name:       "wrapped not found is still 404",
			err:        fmt.Errorf("longbridge news 0000.HK: %w", provider.ErrNotFound),
			wantStatus: http.StatusNotFound, wantCode: "not_found", wantMsg: "symbol not found",
		},
		{
			name: "symbol required is 400", err: provider.ErrSymbolRequired,
			wantStatus: http.StatusBadRequest, wantCode: "symbol_required",
			wantMsg: "this data provider requires a symbol",
		},
		{
			name: "context deadline is 502", err: context.DeadlineExceeded,
			wantStatus: http.StatusBadGateway, wantCode: "provider_error",
			wantMsg: "upstream data provider failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newCharNewsServer(&charNewsProvider{err: tt.err})
			defer ts.Close()

			resp, err := http.Get(ts.URL + "/v1/news?symbol=700.HK")
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			body := charBody(t, resp)
			if body["code"] != tt.wantCode || body["message"] != tt.wantMsg {
				t.Errorf("body = %v, want code=%q message=%q", body, tt.wantCode, tt.wantMsg)
			}
		})
	}
}

func TestCharacterizationNewsRequestValidation(t *testing.T) {
	tests := []struct {
		name, query string
		wantStatus  int
		wantCode    string
	}{
		{"bad symbol", "?symbol=tencent", 400, "invalid_symbol"},
		{"symbol without market", "?symbol=700", 400, "invalid_symbol"},
		{"symbol too long", "?symbol=AAAAAAAAAAAAAAAAAAAAA.US", 400, "invalid_symbol"},
		{"option symbol is valid", "?symbol=MSFT261016P420000.US", 200, ""},
		{"limit zero", "?symbol=700.HK&limit=0", 400, "invalid_limit"},
		{"limit above max", "?symbol=700.HK&limit=51", 400, "invalid_limit"},
		{"limit not a number", "?symbol=700.HK&limit=abc", 400, "invalid_limit"},
		{"limit negative", "?symbol=700.HK&limit=-1", 400, "invalid_limit"},
		{"limit lower bound", "?symbol=700.HK&limit=1", 200, ""},
		{"limit upper bound", "?symbol=700.HK&limit=50", 200, ""},
		{"no symbol reaches the provider", "?limit=3", 200, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			up := &charNewsProvider{items: []provider.NewsItem{charNewsItem("700.HK")}}
			ts := newCharNewsServer(up)
			defer ts.Close()

			resp, err := http.Get(ts.URL + "/v1/news" + tt.query)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.wantCode == "" {
				return
			}
			if body := charBody(t, resp); body["code"] != tt.wantCode {
				t.Errorf("code = %q, want %q", body["code"], tt.wantCode)
			}
			if up.calls != 0 {
				t.Errorf("provider was called %d times for a rejected request, want 0", up.calls)
			}
		})
	}
}

func TestCharacterizationNewsSuccessShape(t *testing.T) {
	published := time.Date(2026, 9, 25, 1, 2, 3, 0, time.UTC)
	up := &charNewsProvider{items: []provider.NewsItem{{
		ID: "n-1", Headline: "Headline", Summary: "Summary", Source: "Longbridge",
		URL: "https://example.com/a", Symbols: []string{"700.HK"}, PublishedAt: published,
	}}}
	ts := newCharNewsServer(up)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/news?symbol=700.HK&limit=5")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("body is not a JSON array: %s (%v)", raw, err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	want := map[string]any{
		"id": "n-1", "headline": "Headline", "summary": "Summary", "source": "Longbridge",
		"url": "https://example.com/a", "publishedAt": "2026-09-25T01:02:03Z",
	}
	for key, value := range want {
		if items[0][key] != value {
			t.Errorf("%s = %v, want %v", key, items[0][key], value)
		}
	}
	symbols, ok := items[0]["symbols"].([]any)
	if !ok || len(symbols) != 1 || symbols[0] != "700.HK" {
		t.Errorf("symbols = %v, want [700.HK]", items[0]["symbols"])
	}
	if len(items[0]) != 7 {
		t.Errorf("item has %d keys, want exactly 7: %v", len(items[0]), items[0])
	}
}

func TestCharacterizationNewsEmptyIsJSONArrayNotNull(t *testing.T) {
	ts := newCharNewsServer(&charNewsProvider{items: []provider.NewsItem{}})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/news?symbol=700.HK")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := string(raw); got != "[]\n" {
		t.Errorf("body = %q, want %q", got, "[]\n")
	}
}

func charNewsItem(symbol string) provider.NewsItem {
	return provider.NewsItem{
		ID: "n-1", Headline: "Headline", Source: "Longbridge", URL: "https://example.com/a",
		Symbols: []string{symbol}, PublishedAt: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC),
	}
}
