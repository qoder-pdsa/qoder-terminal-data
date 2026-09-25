package api

// Characterization tests locking the PRE-CHANGE behaviour of the quote endpoints
// GET /v1/quotes/{symbol} and GET /v1/quotes?symbols=.
//
// Purpose: BL-11-1 widens the contract `Quote` schema with open, high, low, turnover (Decimal)
// and volume (int64). That forces internal/api/handlers.go quoteBody to stop returning
// map[string]string, which is the single change most likely to silently break the legacy fields
// (a JSON number instead of a decimal string, a dropped key, a changed time format). These
// tests pin every other observable behaviour of both handlers so the widening cannot change it.
// Group selection (Go has no tag mechanism in this repo, so the group is name-based):
//
//	go test ./... -run TestCharacterization
//
// Baseline: actually run on the unchanged tree at commit 707c2fa before any modification
// (evidence/baseline-characterization.log, evidence/baseline-legacy-quote-values.log).
//
// Deliberately NOT asserted here, because the work item explicitly changes them:
//   - the exact key set of the quote object. BL-11-1 adds five required keys, so only the six
//     legacy keys and their exact values are locked.
//
// Their real pre-change key set is recorded in evidence/baseline-legacy-quote-values.log.

import (
	"encoding/json"
	"testing"
)

// legacyQuoteFields are the six fields the contract carried before BL-11-1.
var legacyQuoteFields = []string{"symbol", "price", "change", "changePercent", "currency", "asOf"}

// charQuoteSymbols are the six symbols the mock provider knows
// (internal/provider/mock.go knownSymbols), in the order the tests request them.
var charQuoteSymbols = []string{"700.HK", "9988.HK", "3690.HK", "1810.HK", "1211.HK", "2800.HK"}

// charQuoteLegacyValues is the pre-change response of both quote endpoints on the deterministic
// mock provider clocked at 2026-09-17T00:00:00Z by newTestServer. Captured from the unchanged
// tree, not derived from the implementation.
var charQuoteLegacyValues = map[string]map[string]string{
	"700.HK":  {"symbol": "700.HK", "price": "348.8646", "change": "-0.7919", "changePercent": "-0.2264", "currency": "HKD", "asOf": "2026-09-17T00:00:00Z"},
	"9988.HK": {"symbol": "9988.HK", "price": "388.3028", "change": "3.2081", "changePercent": "0.8330", "currency": "HKD", "asOf": "2026-09-17T00:00:00Z"},
	"3690.HK": {"symbol": "3690.HK", "price": "340.2736", "change": "-0.7919", "changePercent": "-0.2321", "currency": "HKD", "asOf": "2026-09-17T00:00:00Z"},
	"1810.HK": {"symbol": "1810.HK", "price": "137.7152", "change": "-0.7919", "changePercent": "-0.5717", "currency": "HKD", "asOf": "2026-09-17T00:00:00Z"},
	"1211.HK": {"symbol": "1211.HK", "price": "437.7194", "change": "-0.7919", "changePercent": "-0.1805", "currency": "HKD", "asOf": "2026-09-17T00:00:00Z"},
	"2800.HK": {"symbol": "2800.HK", "price": "135.1796", "change": "-0.7919", "changePercent": "-0.5824", "currency": "HKD", "asOf": "2026-09-17T00:00:00Z"},
}

// charLegacyQuote decodes one quote object into raw JSON values so the tests can tell a JSON
// string from a JSON number, which map[string]string decoding would hide.
func charLegacyQuote(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("quote body is not a JSON object: %v (%s)", err, raw)
	}
	return body
}

// TestCharacterizationQuoteSingleLegacyFields locks the exact pre-change response of
// GET /v1/quotes/{symbol} for every known symbol: status, the six legacy keys, their values and
// the fact that all of them are JSON strings (never numbers).
func TestCharacterizationQuoteSingleLegacyFields(t *testing.T) {
	for _, symbol := range charQuoteSymbols {
		status, raw := charGet(t, "/v1/quotes/"+symbol)
		if status != 200 {
			t.Errorf("%s: status = %d, want 200 (%s)", symbol, status, raw)
			continue
		}
		body := charLegacyQuote(t, raw)
		for _, field := range legacyQuoteFields {
			want := charQuoteLegacyValues[symbol][field]
			got, ok := body[field]
			if !ok {
				t.Errorf("%s: legacy field %q missing from %s", symbol, field, raw)
				continue
			}
			str, isString := got.(string)
			if !isString {
				t.Errorf("%s: legacy field %q = %#v (%T), want JSON string %q", symbol, field, got, got, want)
				continue
			}
			if str != want {
				t.Errorf("%s: legacy field %q = %q, want %q", symbol, field, str, want)
			}
		}
	}
}

// TestCharacterizationQuoteBatchMatchesLegacySingles locks that the batch endpoint answers the
// same legacy values in the requested order, which is what the web watchlist relies on.
func TestCharacterizationQuoteBatchMatchesLegacySingles(t *testing.T) {
	query := "/v1/quotes?symbols="
	for i, symbol := range charQuoteSymbols {
		if i > 0 {
			query += ","
		}
		query += symbol
	}
	status, raw := charGet(t, query)
	if status != 200 {
		t.Fatalf("status = %d, want 200 (%s)", status, raw)
	}
	var batch []map[string]any
	if err := json.Unmarshal(raw, &batch); err != nil {
		t.Fatalf("batch body is not a JSON array: %v (%s)", err, raw)
	}
	if len(batch) != len(charQuoteSymbols) {
		t.Fatalf("batch length = %d, want %d", len(batch), len(charQuoteSymbols))
	}
	for i, symbol := range charQuoteSymbols {
		row := batch[i]
		for _, field := range legacyQuoteFields {
			want := charQuoteLegacyValues[symbol][field]
			got, ok := row[field]
			if !ok {
				t.Errorf("batch[%d] (%s): legacy field %q missing", i, symbol, field)
				continue
			}
			str, isString := got.(string)
			if !isString {
				t.Errorf("batch[%d] (%s): legacy field %q = %#v (%T), want JSON string %q", i, symbol, field, got, got, want)
				continue
			}
			if str != want {
				t.Errorf("batch[%d] (%s): legacy field %q = %q, want %q", i, symbol, field, str, want)
			}
		}
	}
}

// TestCharacterizationQuoteErrorPathsUnaffected locks the error envelope and status codes of both
// quote endpoints, which share handlers.go with the code BL-11-1 touches.
func TestCharacterizationQuoteErrorPathsUnaffected(t *testing.T) {
	tests := []struct {
		path       string
		wantStatus int
		wantCode   string
	}{
		{"/v1/quotes/0000.HK", 404, "not_found"},
		{"/v1/quotes/tencent", 400, "invalid_symbol"},
		{"/v1/quotes?symbols=tencent", 400, "invalid_symbols"},
		{"/v1/quotes", 400, "invalid_symbols"},
	}
	for _, tt := range tests {
		status, raw := charGet(t, tt.path)
		if status != tt.wantStatus {
			t.Errorf("%s: status = %d, want %d (%s)", tt.path, status, tt.wantStatus, raw)
		}
		var body map[string]string
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("%s: error body is not {code,message}: %v (%s)", tt.path, err, raw)
			continue
		}
		if body["code"] != tt.wantCode {
			t.Errorf("%s: code = %q, want %q", tt.path, body["code"], tt.wantCode)
		}
		if body["message"] == "" {
			t.Errorf("%s: message is empty", tt.path)
		}
	}
}
