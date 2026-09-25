package provider

// Characterization tests locking the PRE-CHANGE behaviour of the provider Quote path.
//
// Purpose: BL-11-1 adds Open, High, Low, Volume and Turnover to provider.Quote, which touches the
// struct definition itself, Mock.Quote and toQuote. Two legacy properties are easy to break by
// accident and are pinned here: the exact deterministic values the mock produces today, and the
// fact that Quote stays comparable with `==` (internal/provider/mock_watchlist_test.go and the
// web batch endpoint both rely on whole-struct equality, so a slice or map field would break it).
// Group selection (Go has no tag mechanism in this repo, so the group is name-based):
//
//	go test ./... -run TestCharacterization
//
// Baseline: actually run on the unchanged tree at commit 707c2fa before any modification
// (evidence/baseline-characterization.log, evidence/baseline-legacy-quote-values.log).
//
// Deliberately NOT asserted here, because the work item explicitly changes them:
//   - Open / High / Low / Volume / Turnover do not exist on Quote yet, so no value is pinned for
//     them; BL-11-1 adds them and its own tests pin them.

import (
	"context"
	"testing"
	"time"

	"github.com/longbridge/openapi-go/quote"
)

// charMockClock is the fixed clock used by these characterization tests.
func charMockClock() time.Time { return time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC) }

// charQuoteCases are the six mock symbols with the legacy quote values captured from the
// unchanged tree. price/change/changePercent come from closeUnits(symbol, 0) and
// closeUnits(symbol, 1); the values are recorded here as observed output, not recomputed.
var charQuoteCases = []struct {
	symbol, price, change, changePercent, currency string
}{
	{"700.HK", "348.8646", "-0.7919", "-0.2264", "HKD"},
	{"9988.HK", "388.3028", "3.2081", "0.8330", "HKD"},
	{"3690.HK", "340.2736", "-0.7919", "-0.2321", "HKD"},
	{"1810.HK", "137.7152", "-0.7919", "-0.5717", "HKD"},
	{"1211.HK", "437.7194", "-0.7919", "-0.1805", "HKD"},
	{"2800.HK", "135.1796", "-0.7919", "-0.5824", "HKD"},
}

// TestCharacterizationMockQuoteLegacyValues locks the exact deterministic quote the mock provider
// returns today for every known symbol, including the fixed clock.
func TestCharacterizationMockQuoteLegacyValues(t *testing.T) {
	m := &Mock{Now: charMockClock}
	for _, tt := range charQuoteCases {
		got, err := m.Quote(context.Background(), tt.symbol)
		if err != nil {
			t.Fatalf("%s: %v", tt.symbol, err)
		}
		if got.Symbol != tt.symbol {
			t.Errorf("%s: symbol = %q, want %q", tt.symbol, got.Symbol, tt.symbol)
		}
		if got.Price.String() != tt.price {
			t.Errorf("%s: price = %s, want %s", tt.symbol, got.Price, tt.price)
		}
		if got.Change.String() != tt.change {
			t.Errorf("%s: change = %s, want %s", tt.symbol, got.Change, tt.change)
		}
		if got.ChangePercent.String() != tt.changePercent {
			t.Errorf("%s: changePercent = %s, want %s", tt.symbol, got.ChangePercent, tt.changePercent)
		}
		if got.Currency != tt.currency {
			t.Errorf("%s: currency = %q, want %q", tt.symbol, got.Currency, tt.currency)
		}
		if !got.AsOf.Equal(charMockClock()) {
			t.Errorf("%s: asOf = %s, want %s", tt.symbol, got.AsOf, charMockClock())
		}
	}
}

// TestCharacterizationQuoteStaysComparable locks that Quote values can be compared with == and
// that the batch path returns exactly the same struct as the single path. mock_watchlist_test.go
// asserts `quotes[1] != single`, which stops compiling the moment Quote gains a slice or map field.
func TestCharacterizationQuoteStaysComparable(t *testing.T) {
	m := &Mock{Now: charMockClock}
	single, err := m.Quote(context.Background(), "9988.HK")
	if err != nil {
		t.Fatal(err)
	}
	batch, err := m.Quotes(context.Background(), []string{"700.HK", "9988.HK"})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != 2 {
		t.Fatalf("batch length = %d, want 2", len(batch))
	}
	if batch[1] != single { //nolint:staticcheck // whole-struct equality is the point of this test
		t.Errorf("batch quote differs from single quote:\n batch=%+v\nsingle=%+v", batch[1], single)
	}
}

// TestCharacterizationLongbridgeQuoteLegacyMapping locks the pre-change toQuote mapping for a
// SecurityQuote that carries only LastDone, PrevClose and Timestamp — the shape every existing
// fake in this package uses, and the shape Longbridge returns before the session opens.
func TestCharacterizationLongbridgeQuoteLegacyMapping(t *testing.T) {
	got, err := toQuote("700.HK", &quote.SecurityQuote{
		Symbol: "700.HK", LastDone: dec("388.200"), PrevClose: dec("380.000"), Timestamp: 1_758_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Symbol != "700.HK" {
		t.Errorf("symbol = %q, want 700.HK", got.Symbol)
	}
	if got.Price.String() != "388.2000" {
		t.Errorf("price = %s, want 388.2000", got.Price)
	}
	if got.Change.String() != "8.2000" {
		t.Errorf("change = %s, want 8.2000", got.Change)
	}
	if got.ChangePercent.String() != "2.1578" {
		t.Errorf("changePercent = %s, want 2.1578", got.ChangePercent)
	}
	if got.Currency != "HKD" {
		t.Errorf("currency = %q, want HKD", got.Currency)
	}
	if !got.AsOf.Equal(time.Date(2025, 9, 16, 5, 20, 0, 0, time.UTC)) {
		t.Errorf("asOf = %s, want 2025-09-16T05:20:00Z", got.AsOf)
	}
}

// TestCharacterizationLongbridgeIntradayPrevCloseUnaffected locks that Intraday keeps deriving its
// previous close from the quote as Price.Sub(Change). BL-11-1 changes toQuote, and this is the one
// existing consumer that recomputes a value from it.
func TestCharacterizationLongbridgeIntradayPrevCloseUnaffected(t *testing.T) {
	lb := &Longbridge{quotes: &fakeQuotes{
		quotes: []*quote.SecurityQuote{{Symbol: "700.HK", LastDone: dec("438.4"), PrevClose: dec("441.0"), Timestamp: 1_758_000_000}},
		lines:  []*quote.IntradayLine{{Price: dec("438.4"), AvgPrice: dec("439.3"), Volume: 2000, Timestamp: 1_758_000_060}},
	}}
	in, err := lb.Intraday(context.Background(), "700.HK")
	if err != nil {
		t.Fatal(err)
	}
	if in.PrevClose.String() != "441.0000" {
		t.Errorf("prevClose = %s, want 441.0000", in.PrevClose)
	}
	if in.Currency != "HKD" {
		t.Errorf("currency = %q, want HKD", in.Currency)
	}
	if len(in.Points) != 1 || in.Points[0].AvgPrice.String() != "439.3000" || in.Points[0].Volume != 2000 {
		t.Errorf("points = %+v, want one point with avgPrice 439.3000 and volume 2000", in.Points)
	}
}
