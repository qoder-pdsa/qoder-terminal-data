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
