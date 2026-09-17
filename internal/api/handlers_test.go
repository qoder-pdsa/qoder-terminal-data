package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qoder-pdsa/qoder-terminal-data/internal/provider"
)

func newTestServer() *httptest.Server {
	mock := &provider.Mock{Now: func() time.Time { return time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC) }}
	s := &Server{Provider: mock, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return httptest.NewServer(s.Routes())
}

func TestRoutes(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	tests := []struct {
		name, path string
		wantStatus int
	}{
		{"health", "/health", 200},
		{"quote ok", "/v1/quotes/700.HK", 200},
		{"quote unknown", "/v1/quotes/0000.HK", 404},
		{"quote bad symbol", "/v1/quotes/tencent", 400},
		{"quote missing market", "/v1/quotes/700", 400},
		{"history default", "/v1/history/9988.HK", 200},
		{"history bad range", "/v1/history/9988.HK?range=5Y", 400},
		{"news all", "/v1/news?limit=3", 200},
		{"news by symbol", "/v1/news?symbol=3690.HK", 200},
		{"news bad limit", "/v1/news?limit=0", 400},
		{"sma ok", "/v1/indicators/700.HK?kind=sma&window=20", 200},
		{"sma bad kind", "/v1/indicators/700.HK?kind=macd&window=20", 400},
		{"sma bad window", "/v1/indicators/700.HK?kind=sma&window=0", 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + tt.path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestQuotePricesAreDecimalStrings(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/quotes/700.HK")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"price", "change", "changePercent"} {
		if _, ok := body[field].(string); !ok {
			t.Errorf("%s should be a decimal string, got %T", field, body[field])
		}
	}
}

func TestIndicatorPointsAlignWithHistory(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/indicators/700.HK?kind=sma&window=5&range=1M")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Points []struct {
			Value *string `json:"value"`
		} `json:"points"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Points) != rangeDays["1M"] {
		t.Fatalf("points = %d, want %d", len(body.Points), rangeDays["1M"])
	}
	if body.Points[3].Value != nil || body.Points[4].Value == nil {
		t.Errorf("warm-up boundary wrong: [3]=%v [4]=%v", body.Points[3].Value, body.Points[4].Value)
	}
}
