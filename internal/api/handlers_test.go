package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qoder-pdsa/qoder-terminal-data/internal/money"
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
		{"quotes batch", "/v1/quotes?symbols=700.HK,9988.HK", 200},
		{"quotes batch spaces", "/v1/quotes?symbols=700.HK,%209988.HK", 200},
		{"quotes batch missing", "/v1/quotes", 400},
		{"quotes batch bad symbol", "/v1/quotes?symbols=700.HK,tencent", 400},
		{"quotes batch unknown omitted", "/v1/quotes?symbols=700.HK,0000.HK", 200},
		{"quotes batch option symbol", "/v1/quotes?symbols=700.HK,MSFT261016P420000.US", 200},
		{"quote option symbol unknown to mock", "/v1/quotes/MSFT261016P420000.US", 404},
		{"quote symbol too long", "/v1/quotes/AAAAAAAAAAAAAAAAAAAAA.US", 400},
		{"quotes batch too many", "/v1/quotes?symbols=" + manySymbols(21), 400},
		{"watchlists", "/v1/watchlists", 200},
		{"intraday ok", "/v1/intraday/700.HK", 200},
		{"intraday unknown", "/v1/intraday/0000.HK", 404},
		{"intraday bad symbol", "/v1/intraday/tencent", 400},
		{"capital flow ok", "/v1/capital-flow/700.HK", 200},
		{"capital flow unknown", "/v1/capital-flow/0000.HK", 404},
		{"capital flow bad symbol", "/v1/capital-flow/tencent", 400},
		{"sma ok", "/v1/indicators/700.HK?kind=sma&window=20", 200},
		{"ema ok", "/v1/indicators/700.HK?kind=ema&window=20", 200},
		{"rsi ok", "/v1/indicators/700.HK?kind=rsi&window=14", 200},
		{"sma bad kind", "/v1/indicators/700.HK?kind=macd&window=20", 400},
		{"ema bad kind", "/v1/indicators/700.HK?kind=bollinger&window=20", 400},
		{"rsi bad window", "/v1/indicators/700.HK?kind=rsi&window=251", 400},
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
	for _, field := range []string{"price", "change", "changePercent", "open", "high", "low", "turnover"} {
		if _, ok := body[field].(string); !ok {
			t.Errorf("%s should be a decimal string, got %T", field, body[field])
		}
	}
}

// TestQuoteVolumeIsAJSONInteger pins the one new field that is not a decimal string: the contract
// declares volume as integer/int64, so it must serialise as a JSON number, and it must survive the
// move of quoteBody from map[string]string to map[string]any.
func TestQuoteVolumeIsAJSONInteger(t *testing.T) {
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
	volume, ok := body["volume"]
	if !ok {
		t.Fatalf("volume missing from the quote body: %v", body)
	}
	number, isNumber := volume.(float64)
	if !isNumber {
		t.Fatalf("volume = %#v (%T), want a JSON number", volume, volume)
	}
	if number != float64(int64(number)) {
		t.Errorf("volume = %v, want a whole int64 value", volume)
	}
	if int64(number) <= 0 {
		t.Errorf("volume = %v, want a positive session volume", volume)
	}
}

// TestQuoteSessionStatsReachTheClient pins the five fields BL-11-1 adds to the contract on both
// quote endpoints, with the exact values the mock provider derives (see
// internal/provider/mock_test.go for the formula).
func TestQuoteSessionStatsReachTheClient(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	wantSingle := map[string]any{
		"open": "349.6565", "high": "349.8065", "low": "348.7146",
		"turnover": "558686073.8886", "volume": float64(1_601_441),
	}
	var single map[string]any
	resp, err := http.Get(ts.URL + "/v1/quotes/700.HK")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&single); err != nil {
		t.Fatal(err)
	}
	for field, want := range wantSingle {
		if single[field] != want {
			t.Errorf("single %s = %#v, want %#v", field, single[field], want)
		}
	}

	var batch []map[string]any
	resp2, err := http.Get(ts.URL + "/v1/quotes?symbols=700.HK")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if err := json.NewDecoder(resp2.Body).Decode(&batch); err != nil {
		t.Fatal(err)
	}
	if len(batch) != 1 {
		t.Fatalf("batch length = %d, want 1", len(batch))
	}
	for field, want := range wantSingle {
		if batch[0][field] != want {
			t.Errorf("batch %s = %#v, want %#v", field, batch[0][field], want)
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

// TestIndicatorKinds covers every kind the contract allows: the requested kind is echoed back, the
// series stays aligned with the requested range, the warm-up nulls match each indicator's own rule
// (sma/ema start at window-1, rsi starts at window) and every value is a canonical decimal string.
func TestIndicatorKinds(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	tests := []struct {
		name      string
		path      string
		kind      string
		window    int
		rng       string
		wantFirst int
	}{
		{"sma over 1M", "/v1/indicators/700.HK?kind=sma&window=5&range=1M", "sma", 5, "1M", 4},
		{"ema over 1M", "/v1/indicators/700.HK?kind=ema&window=5&range=1M", "ema", 5, "1M", 4},
		{"rsi over 1M", "/v1/indicators/700.HK?kind=rsi&window=5&range=1M", "rsi", 5, "1M", 5},
		{"ema over default range", "/v1/indicators/700.HK?kind=ema&window=20", "ema", 20, "3M", 19},
		{"rsi over default range", "/v1/indicators/700.HK?kind=rsi&window=14", "rsi", 14, "3M", 14},
		{"rsi window one", "/v1/indicators/700.HK?kind=rsi&window=1&range=1M", "rsi", 1, "1M", 1},
		{"ema window one", "/v1/indicators/700.HK?kind=ema&window=1&range=1M", "ema", 1, "1M", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + tt.path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			var body struct {
				Symbol string `json:"symbol"`
				Kind   string `json:"kind"`
				Window int    `json:"window"`
				Points []struct {
					Time  string          `json:"time"`
					Value json.RawMessage `json:"value"`
				} `json:"points"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Symbol != "700.HK" {
				t.Errorf("symbol = %q, want 700.HK", body.Symbol)
			}
			if body.Kind != tt.kind {
				t.Errorf("kind = %q, want %q echoed back", body.Kind, tt.kind)
			}
			if body.Window != tt.window {
				t.Errorf("window = %d, want %d", body.Window, tt.window)
			}
			if len(body.Points) != rangeDays[tt.rng] {
				t.Fatalf("points = %d, want %d", len(body.Points), rangeDays[tt.rng])
			}
			for i, p := range body.Points {
				if i < tt.wantFirst {
					if string(p.Value) != "null" {
						t.Errorf("points[%d].value = %s, want null during warm-up", i, p.Value)
					}
					continue
				}
				var s string
				if err := json.Unmarshal(p.Value, &s); err != nil {
					t.Fatalf("points[%d].value = %s, want a decimal string not a JSON number", i, p.Value)
				}
				d, err := money.Parse(s)
				if err != nil {
					t.Fatalf("points[%d].value = %q: %v", i, s, err)
				}
				if d.String() != s {
					t.Errorf("points[%d].value = %q, want the canonical %q", i, s, d.String())
				}
				if tt.kind == "rsi" && (d.Units() < 0 || d.Units() > 1_000_000) {
					t.Errorf("points[%d].value = %s, want within 0-100", i, s)
				}
			}
		})
	}
}

// TestIndicatorTimesMatchHistory locks the deployment acceptance criterion that the indicator series
// is aligned with history, for every kind.
func TestIndicatorTimesMatchHistory(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	histResp, err := http.Get(ts.URL + "/v1/history/700.HK?range=1M")
	if err != nil {
		t.Fatal(err)
	}
	defer histResp.Body.Close()
	var candles []struct {
		Time string `json:"time"`
	}
	if err := json.NewDecoder(histResp.Body).Decode(&candles); err != nil {
		t.Fatal(err)
	}
	if len(candles) == 0 {
		t.Fatal("history returned no candles, so this test would be vacuous")
	}

	for _, kind := range []string{"sma", "ema", "rsi"} {
		t.Run(kind, func(t *testing.T) {
			resp, err := http.Get(ts.URL + "/v1/indicators/700.HK?kind=" + kind + "&window=5&range=1M")
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var body struct {
				Points []struct {
					Time string `json:"time"`
				} `json:"points"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Points) != len(candles) {
				t.Fatalf("points = %d, want %d", len(body.Points), len(candles))
			}
			for i := range candles {
				if body.Points[i].Time != candles[i].Time {
					t.Errorf("points[%d].time = %s, want %s", i, body.Points[i].Time, candles[i].Time)
				}
			}
		})
	}
}
