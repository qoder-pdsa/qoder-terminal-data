package api

// Characterization tests locking the PRE-CHANGE behaviour of GET /v1/indicators/{symbol}.
//
// Purpose: BL-01 adds kind=ema and kind=rsi to this endpoint. These tests pin every other
// observable behaviour of the handler so the addition cannot silently change it.
// Group selection (Go has no tag mechanism in this repo, so the group is name-based):
//
//	go test ./... -run TestCharacterization
//
// Baseline: actually run on the unchanged tree at commit 35f33b3 before any modification
// (evidence/baseline-characterization.log, evidence/baseline-legacy-http-behaviour.log).
//
// Deliberately NOT asserted here, because the work item explicitly changes them:
//   - kind=ema and kind=rsi answer 400 invalid_kind today; acceptance criterion 6 requires 200.
//   - the invalid_kind message text "kind must be sma (ema/rsi on backlog)".
//
// Their real pre-change values are recorded in evidence/baseline-legacy-http-behaviour.log.
// The invalid_kind *code* is still locked: unknown kinds must keep returning 400 invalid_kind.

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"
)

type charSeries struct {
	Symbol string `json:"symbol"`
	Kind   string `json:"kind"`
	Window int    `json:"window"`
	Points []struct {
		Time  string  `json:"time"`
		Value *string `json:"value"`
	} `json:"points"`
}

// charGet performs one request against a fresh httptest server running the real routes on the
// deterministic mock provider. No real network, no real service, no shared state between cases.
func charGet(t *testing.T, path string) (int, []byte) {
	t.Helper()
	ts := newTestServer()
	defer ts.Close()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, body
}

// TestCharacterizationIndicatorSMASeries locks the exact legacy SMA response: warm-up nils,
// decimal string formatting, time formatting, series/history alignment and the echo fields.
func TestCharacterizationIndicatorSMASeries(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantPoints int
		wantFirst  int // index of the first non-nil value
		want       map[int]string
		wantTimes  map[int]string
	}{
		{
			name:       "window 5 over 1M",
			path:       "/v1/indicators/700.HK?kind=sma&window=5&range=1M",
			wantPoints: 21,
			wantFirst:  4,
			want:       map[int]string{4: "351.1188", 5: "350.3269", 20: "350.4484"},
			wantTimes:  map[int]string{0: "2026-08-28T00:00:00Z", 20: "2026-09-17T00:00:00Z"},
		},
		{
			name:       "window 20 over default range 3M",
			path:       "/v1/indicators/700.HK?kind=sma&window=20",
			wantPoints: 63,
			wantFirst:  19,
			want:       map[int]string{19: "350.8394", 20: "350.8475", 62: "350.3877"},
			wantTimes:  map[int]string{0: "2026-07-17T00:00:00Z", 19: "2026-08-05T00:00:00Z", 62: "2026-09-17T00:00:00Z"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, raw := charGet(t, tt.path)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", status, raw)
			}
			var body charSeries
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("decode: %v (%s)", err, raw)
			}
			if body.Symbol != "700.HK" {
				t.Errorf("symbol = %q, want 700.HK", body.Symbol)
			}
			if body.Kind != "sma" {
				t.Errorf("kind = %q, want sma", body.Kind)
			}
			if len(body.Points) != tt.wantPoints {
				t.Fatalf("points = %d, want %d (series must stay aligned with history)", len(body.Points), tt.wantPoints)
			}
			for i := 0; i < tt.wantFirst; i++ {
				if body.Points[i].Value != nil {
					t.Errorf("points[%d].value = %s, want null (warm-up)", i, *body.Points[i].Value)
				}
			}
			for i, want := range tt.want {
				got := body.Points[i].Value
				if got == nil {
					t.Errorf("points[%d].value = null, want %s", i, want)
					continue
				}
				if *got != want {
					t.Errorf("points[%d].value = %s, want %s", i, *got, want)
				}
			}
			for i, want := range tt.wantTimes {
				if body.Points[i].Time != want {
					t.Errorf("points[%d].time = %s, want %s", i, body.Points[i].Time, want)
				}
			}
		})
	}
}

// TestCharacterizationIndicatorWindowEcho locks that window is echoed back as an integer.
func TestCharacterizationIndicatorWindowEcho(t *testing.T) {
	for _, window := range []int{1, 20, 250} {
		status, raw := charGet(t, "/v1/indicators/700.HK?kind=sma&window="+strconv.Itoa(window)+"&range=1M")
		if status != http.StatusOK {
			t.Fatalf("window=%d status = %d, want 200 (%s)", window, status, raw)
		}
		var body charSeries
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("window=%d decode: %v", window, err)
		}
		if body.Window != window {
			t.Errorf("window = %d, want %d", body.Window, window)
		}
	}
}

// TestCharacterizationIndicatorValidation locks the request-validation behaviour that must
// survive the change: status code plus error code for every rejected request.
func TestCharacterizationIndicatorValidation(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantCode   string
		// wantMessage is only pinned for errors BL-01 does not touch. The invalid_kind
		// message text is intentionally left unpinned because the requirement changes it.
		wantMessage string
	}{
		{"unknown kind macd", "/v1/indicators/700.HK?kind=macd&window=20", 400, "invalid_kind", ""},
		{"kind missing", "/v1/indicators/700.HK?window=20", 400, "invalid_kind", ""},
		{"kind wrong case", "/v1/indicators/700.HK?kind=SMA&window=20", 400, "invalid_kind", ""},
		{"kind empty", "/v1/indicators/700.HK?kind=&window=20", 400, "invalid_kind", ""},
		{"window zero", "/v1/indicators/700.HK?kind=sma&window=0", 400, "invalid_window", "window must be 1..250"},
		{"window negative", "/v1/indicators/700.HK?kind=sma&window=-1", 400, "invalid_window", "window must be 1..250"},
		{"window above max", "/v1/indicators/700.HK?kind=sma&window=251", 400, "invalid_window", "window must be 1..250"},
		{"window missing", "/v1/indicators/700.HK?kind=sma", 400, "invalid_window", "window must be 1..250"},
		{"window not a number", "/v1/indicators/700.HK?kind=sma&window=abc", 400, "invalid_window", "window must be 1..250"},
		{"range unknown", "/v1/indicators/700.HK?kind=sma&window=20&range=5Y", 400, "invalid_range", "range must be one of 1M, 3M, 6M, 1Y"},
		{"symbol without market", "/v1/indicators/700?kind=sma&window=20", 400, "invalid_symbol", "symbol must look like 700.HK"},
		{"symbol not matching pattern", "/v1/indicators/tencent?kind=sma&window=20", 400, "invalid_symbol", "symbol must look like 700.HK"},
		{"symbol unknown to provider", "/v1/indicators/0000.HK?kind=sma&window=20", 404, "not_found", "symbol not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, raw := charGet(t, tt.path)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", status, tt.wantStatus, raw)
			}
			var body map[string]string
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("decode: %v (%s)", err, raw)
			}
			if body["code"] != tt.wantCode {
				t.Errorf("code = %q, want %q", body["code"], tt.wantCode)
			}
			if tt.wantMessage != "" && body["message"] != tt.wantMessage {
				t.Errorf("message = %q, want %q", body["message"], tt.wantMessage)
			}
		})
	}
}

// TestCharacterizationErrorEnvelopeShape locks the AGENTS.md rule that every error response is
// exactly {"code","message"} and never leaks raw upstream errors.
func TestCharacterizationErrorEnvelopeShape(t *testing.T) {
	paths := []string{
		"/v1/indicators/700.HK?kind=macd&window=20",
		"/v1/indicators/700.HK?kind=sma&window=0",
		"/v1/indicators/700.HK?kind=sma&window=20&range=5Y",
		"/v1/indicators/tencent?kind=sma&window=20",
		"/v1/indicators/0000.HK?kind=sma&window=20",
	}
	for _, path := range paths {
		_, raw := charGet(t, path)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("%s decode: %v (%s)", path, err, raw)
		}
		if len(body) != 2 {
			t.Errorf("%s: envelope has %d keys %v, want exactly code+message", path, len(body), body)
		}
		if _, ok := body["code"].(string); !ok {
			t.Errorf("%s: code missing or not a string: %v", path, body["code"])
		}
		if _, ok := body["message"].(string); !ok {
			t.Errorf("%s: message missing or not a string: %v", path, body["message"])
		}
	}
}

// TestCharacterizationSiblingRoutesUnaffected locks that the routes sharing handlers.go with the
// indicator handler keep their behaviour while the indicator handler changes.
func TestCharacterizationSiblingRoutesUnaffected(t *testing.T) {
	tests := []struct {
		path       string
		wantStatus int
	}{
		{"/health", 200},
		{"/v1/quotes/700.HK", 200},
		{"/v1/quotes/0000.HK", 404},
		{"/v1/history/9988.HK", 200},
		{"/v1/history/9988.HK?range=5Y", 400},
		{"/v1/news?limit=3", 200},
		{"/v1/news?limit=0", 400},
	}
	for _, tt := range tests {
		status, raw := charGet(t, tt.path)
		if status != tt.wantStatus {
			t.Errorf("%s: status = %d, want %d (%s)", tt.path, status, tt.wantStatus, raw)
		}
	}
}

// TestCharacterizationIndicatorValuesAreDecimalStrings locks that every non-null indicator value
// is a JSON string with exactly money.Scale fractional digits (never a JSON number / float64).
func TestCharacterizationIndicatorValuesAreDecimalStrings(t *testing.T) {
	status, raw := charGet(t, "/v1/indicators/700.HK?kind=sma&window=20")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	var probe struct {
		Points []struct {
			Value json.RawMessage `json:"value"`
		} `json:"points"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for i, p := range probe.Points {
		if string(p.Value) == "null" {
			continue
		}
		seen++
		var s string
		if err := json.Unmarshal(p.Value, &s); err != nil {
			t.Fatalf("points[%d].value is not a JSON string: %s", i, p.Value)
		}
		if len(s) < 6 || s[len(s)-5] != '.' {
			t.Errorf("points[%d].value = %q, want 4 fractional digits", i, s)
		}
	}
	if seen == 0 {
		t.Fatal("no non-null values found; characterization would be vacuous")
	}
}
