package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/longbridge/openapi-go/content"
	"github.com/longbridge/openapi-go/quote"
	"github.com/shopspring/decimal"
)

type fakeQuotes struct {
	quotes []*quote.SecurityQuote
	sticks []*quote.Candlestick
	groups []*quote.WatchedGroup
	lines  []*quote.IntradayLine
	flow   []quote.CapitalFlowLine
	dist   quote.CapitalDistribution
	err    error
	asked  []string // symbols passed to Quote
}

func (f *fakeQuotes) Quote(_ context.Context, symbols []string) ([]*quote.SecurityQuote, error) {
	f.asked = append(f.asked, symbols...)
	return f.quotes, f.err
}

func (f *fakeQuotes) Intraday(context.Context, string) ([]*quote.IntradayLine, error) {
	return f.lines, f.err
}

func (f *fakeQuotes) WatchedGroups(context.Context) ([]*quote.WatchedGroup, error) {
	return f.groups, f.err
}

func (f *fakeQuotes) CapitalFlow(context.Context, string) ([]quote.CapitalFlowLine, error) {
	return f.flow, f.err
}

func (f *fakeQuotes) CapitalDistribution(context.Context, string) (quote.CapitalDistribution, error) {
	return f.dist, f.err
}

func (f *fakeQuotes) Candlesticks(context.Context, string, quote.Period, int32, quote.AdjustType) ([]*quote.Candlestick, error) {
	return f.sticks, f.err
}

type fakeNews struct{ items []*content.NewsItem }

func (f fakeNews) News(context.Context, string) ([]*content.NewsItem, error) { return f.items, nil }

func dec(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}

func TestLongbridgeQuoteMapsDecimals(t *testing.T) {
	lb := &Longbridge{quotes: &fakeQuotes{quotes: []*quote.SecurityQuote{{
		Symbol: "700.HK", LastDone: dec("388.200"), PrevClose: dec("380.000"), Timestamp: 1_758_000_000,
	}}}}
	q, err := lb.Quote(context.Background(), "700.HK")
	if err != nil {
		t.Fatal(err)
	}
	if q.Price.String() != "388.2000" || q.Change.String() != "8.2000" || q.Currency != "HKD" {
		t.Errorf("unexpected quote: price=%s change=%s currency=%s", q.Price, q.Change, q.Currency)
	}
	if q.ChangePercent.String() != "2.1578" {
		t.Errorf("changePercent = %s, want 2.1578", q.ChangePercent)
	}
}

// TestLongbridgeQuoteMapsSessionStats pins the BL-11 mapping of SecurityQuote.Open/High/Low/
// Volume/Turnover through toMoney, on a quote taken during the session when Longbridge fills
// every field.
func TestLongbridgeQuoteMapsSessionStats(t *testing.T) {
	lb := &Longbridge{quotes: &fakeQuotes{quotes: []*quote.SecurityQuote{{
		Symbol: "700.HK", LastDone: dec("388.200"), PrevClose: dec("380.000"),
		Open: dec("381.500"), High: dec("389.900"), Low: dec("379.100"),
		Volume: 12_345_678, Turnover: dec("4765432100.5"), Timestamp: 1_758_000_000,
	}}}}
	q, err := lb.Quote(context.Background(), "700.HK")
	if err != nil {
		t.Fatal(err)
	}
	fields := []struct{ name, want, actual string }{
		{"open", "381.5000", q.Open.String()},
		{"high", "389.9000", q.High.String()},
		{"low", "379.1000", q.Low.String()},
		{"turnover", "4765432100.5000", q.Turnover.String()},
		{"price", "388.2000", q.Price.String()},
		{"change", "8.2000", q.Change.String()},
	}
	for _, f := range fields {
		if f.actual != f.want {
			t.Errorf("%s = %s, want %s", f.name, f.actual, f.want)
		}
	}
	if q.Volume != 12_345_678 {
		t.Errorf("volume = %d, want 12345678", q.Volume)
	}
}

// TestLongbridgeQuoteBeforeOpenUsesPrevClose pins the pre-session fallback. Longbridge omits
// Open/High/Low/Turnover (nil pointers) and reports zero Volume until the first trade of the
// session, which on a HK symbol is 09:30 HKT. Mapping those nils through toMoney would either
// error or print a nonsense "0.0000" price on the terminal, so open/high/low fall back to the
// previous close — the only price that is actually known — while volume and turnover stay zero
// because nothing has traded yet.
func TestLongbridgeQuoteBeforeOpenUsesPrevClose(t *testing.T) {
	lb := &Longbridge{quotes: &fakeQuotes{quotes: []*quote.SecurityQuote{{
		Symbol: "700.HK", LastDone: dec("380.000"), PrevClose: dec("380.000"),
		Open: nil, High: nil, Low: nil, Turnover: nil, Volume: 0, Timestamp: 1_758_000_000,
	}}}}
	q, err := lb.Quote(context.Background(), "700.HK")
	if err != nil {
		t.Fatalf("pre-open quote must not error: %v", err)
	}
	fields := []struct{ name, want, actual string }{
		{"open", "380.0000", q.Open.String()},
		{"high", "380.0000", q.High.String()},
		{"low", "380.0000", q.Low.String()},
		{"turnover", "0.0000", q.Turnover.String()},
	}
	for _, f := range fields {
		if f.actual != f.want {
			t.Errorf("%s = %s, want %s", f.name, f.actual, f.want)
		}
	}
	if q.Volume != 0 {
		t.Errorf("volume = %d, want 0 before the first trade", q.Volume)
	}
	if q.Open.Units() == 0 {
		t.Error("open is 0.0000; the previous close fallback did not run")
	}
}

// TestLongbridgeQuotesCarrySessionStats pins that the batch path — one upstream call for the whole
// watchlist — maps the new fields too, so the W panel and the Q panel cannot disagree.
func TestLongbridgeQuotesCarrySessionStats(t *testing.T) {
	lb := &Longbridge{quotes: &fakeQuotes{quotes: []*quote.SecurityQuote{
		{Symbol: "700.HK", LastDone: dec("388.200"), PrevClose: dec("380.000"), Open: dec("381.500"), High: dec("389.900"), Low: dec("379.100"), Volume: 12_345_678, Turnover: dec("4765432100.5"), Timestamp: 1_758_000_000},
		{Symbol: "9988.HK", LastDone: dec("120.000"), PrevClose: dec("118.000"), Open: nil, High: nil, Low: nil, Volume: 0, Turnover: nil, Timestamp: 1_758_000_000},
	}}}
	quotes, err := lb.Quotes(context.Background(), []string{"9988.HK", "700.HK"})
	if err != nil {
		t.Fatal(err)
	}
	if len(quotes) != 2 {
		t.Fatalf("len = %d, want 2", len(quotes))
	}
	if quotes[0].Symbol != "9988.HK" || quotes[1].Symbol != "700.HK" {
		t.Fatalf("order not preserved: %s, %s", quotes[0].Symbol, quotes[1].Symbol)
	}
	if quotes[0].Open.String() != "118.0000" || quotes[0].High.String() != "118.0000" || quotes[0].Low.String() != "118.0000" {
		t.Errorf("9988.HK pre-open fallback: open=%s high=%s low=%s, want all 118.0000", quotes[0].Open, quotes[0].High, quotes[0].Low)
	}
	if quotes[1].Open.String() != "381.5000" || quotes[1].Volume != 12_345_678 {
		t.Errorf("700.HK: open=%s volume=%d, want 381.5000 and 12345678", quotes[1].Open, quotes[1].Volume)
	}
}

func TestLongbridgeQuoteEmptyIsNotFound(t *testing.T) {
	lb := &Longbridge{quotes: &fakeQuotes{}}
	if _, err := lb.Quote(context.Background(), "0000.HK"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestLongbridgeQuoteUpstreamError(t *testing.T) {
	upstream := errors.New("token expired")
	lb := &Longbridge{quotes: &fakeQuotes{err: upstream}}
	if _, err := lb.Quote(context.Background(), "700.HK"); !errors.Is(err, upstream) || errors.Is(err, ErrNotFound) {
		t.Errorf("want wrapped upstream error, got %v", err)
	}
}

func TestLongbridgeHistory(t *testing.T) {
	lb := &Longbridge{quotes: &fakeQuotes{sticks: []*quote.Candlestick{
		{Open: dec("1"), High: dec("2"), Low: dec("0.5"), Close: dec("1.5"), Volume: 10, Timestamp: 1_758_000_000},
		{Open: dec("1.5"), High: nil, Low: dec("1"), Close: dec("1.2"), Volume: 10, Timestamp: 1_758_086_400},
	}}}
	if _, err := lb.History(context.Background(), "700.HK", 2); err == nil {
		t.Error("want error for candlestick with missing high")
	}
}

func TestLongbridgeNewsSkipsItemsWithoutURLAndRespectsLimit(t *testing.T) {
	now := time.Now()
	lb, _ := newPacedLongbridge(fakeNews{items: []*content.NewsItem{
		{Id: "1", Title: "no url", PublishedAt: now},
		{Id: "2", Title: "a", Url: "https://example.com/a", PublishedAt: now},
		{Id: "3", Title: "b", Url: "https://example.com/b", PublishedAt: now},
	}})
	items, err := lb.News(context.Background(), "700.HK", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "2" {
		t.Errorf("unexpected items: %+v", items)
	}
	if _, err := lb.News(context.Background(), "", 5); !errors.Is(err, ErrSymbolRequired) {
		t.Errorf("want ErrSymbolRequired, got %v", err)
	}
}

func TestLongbridgeDailyCandleUsesExchangeTradingDate(t *testing.T) {
	// Longbridge daily candles are stamped at exchange-local midnight: 2026-09-17 00:00 HKT = 2026-09-16T16:00:00Z
	hkMidnight := time.Date(2026, 9, 17, 0, 0, 0, 0, time.FixedZone("HKT", 8*3600))
	lb := &Longbridge{quotes: &fakeQuotes{sticks: []*quote.Candlestick{
		{Open: dec("426.2"), High: dec("431"), Low: dec("425"), Close: dec("426"), Volume: 1, Timestamp: hkMidnight.Unix()},
	}}}
	candles, err := lb.History(context.Background(), "700.HK", 1)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	if !candles[0].Time.Equal(want) {
		t.Errorf("candle time = %s, want %s (trading date as UTC midnight)", candles[0].Time, want)
	}
}
