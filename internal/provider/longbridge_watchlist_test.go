package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/longbridge/openapi-go/quote"
)

func TestLongbridgeQuotesUsesOneUpstreamCallInRequestedOrder(t *testing.T) {
	fake := &fakeQuotes{quotes: []*quote.SecurityQuote{
		{Symbol: "9988.HK", LastDone: dec("109.4"), PrevClose: dec("109.8"), Timestamp: 1_758_000_000},
		{Symbol: "700.HK", LastDone: dec("438.4"), PrevClose: dec("441.0"), Timestamp: 1_758_000_000},
	}}
	lb := &Longbridge{quotes: fake}
	quotes, err := lb.Quotes(context.Background(), []string{"700.HK", "9988.HK"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.asked) != 2 {
		t.Errorf("upstream asked for %v, want both symbols in one call", fake.asked)
	}
	if quotes[0].Symbol != "700.HK" || quotes[1].Symbol != "9988.HK" {
		t.Errorf("order not preserved: %s, %s", quotes[0].Symbol, quotes[1].Symbol)
	}
	if quotes[0].Price.String() != "438.4000" || quotes[0].Change.String() != "-2.6000" {
		t.Errorf("700.HK mapped wrong: %+v", quotes[0])
	}
}

func TestLongbridgeQuotesOmitsSymbolsWithoutAQuote(t *testing.T) {
	fake := &fakeQuotes{quotes: []*quote.SecurityQuote{{Symbol: "700.HK", LastDone: dec("1"), PrevClose: dec("1")}}}
	lb := &Longbridge{quotes: fake}
	quotes, err := lb.Quotes(context.Background(), []string{"MSFT261016P420000.US", "700.HK"})
	if err != nil {
		t.Fatal(err)
	}
	if len(quotes) != 1 || quotes[0].Symbol != "700.HK" {
		t.Errorf("want only 700.HK, got %+v", quotes)
	}
	if _, err := lb.Quote(context.Background(), "MSFT261016P420000.US"); !errors.Is(err, ErrNotFound) {
		t.Errorf("single quote of a missing symbol: want ErrNotFound, got %v", err)
	}
}

func TestLongbridgeWatchlistsMapsGroups(t *testing.T) {
	lb := &Longbridge{quotes: &fakeQuotes{groups: []*quote.WatchedGroup{
		{Id: 42, Name: "港股", Securites: []*quote.WatchedSecurity{{Symbol: "700.HK", Name: "腾讯控股"}, {Symbol: "9988.HK", Name: "阿里巴巴-W"}}},
		{Id: 7, Name: "Empty"},
	}}}
	lists, err := lb.Watchlists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(lists) != 2 || lists[0].ID != "42" || lists[0].Name != "港股" || lists[1].ID != "7" {
		t.Fatalf("unexpected groups: %+v", lists)
	}
	if len(lists[0].Symbols) != 2 || lists[0].Symbols[1] != (WatchedSymbol{Symbol: "9988.HK", Name: "阿里巴巴-W"}) {
		t.Errorf("symbols: %+v", lists[0].Symbols)
	}
	if lists[1].Symbols == nil {
		t.Error("an empty group must serialize as [] not null")
	}
}

func TestLongbridgeCapitalFlowMapsLinesAndDistribution(t *testing.T) {
	lb := &Longbridge{quotes: &fakeQuotes{
		flow: []quote.CapitalFlowLine{{Inflow: dec("-1200.5"), Timestamp: 1_758_000_000}, {Inflow: dec("300"), Timestamp: 1_758_000_060}},
		dist: quote.CapitalDistribution{
			Symbol: "700.HK", Timestamp: 1_758_000_120,
			CapitalIn:  quote.Capital{Large: dec("10"), Medium: dec("5"), Small: dec("2")},
			CapitalOut: quote.Capital{Large: dec("4"), Medium: dec("6"), Small: dec("2")},
		},
	}}
	cf, err := lb.CapitalFlow(context.Background(), "700.HK")
	if err != nil {
		t.Fatal(err)
	}
	if cf.Symbol != "700.HK" || cf.Currency != "HKD" || cf.AsOf.Unix() != 1_758_000_120 {
		t.Errorf("header: %+v", cf)
	}
	if len(cf.Flow) != 2 || cf.Flow[0].Inflow.String() != "-1200.5000" || cf.Flow[1].Time.Unix() != 1_758_000_060 {
		t.Errorf("flow: %+v", cf.Flow)
	}
	net := cf.In.Sub(cf.Out)
	if net.Large.String() != "6.0000" || net.Medium.String() != "-1.0000" || net.Small.String() != "0.0000" {
		t.Errorf("net: %+v", net)
	}
}

func TestLongbridgeCapitalFlowBeforeOpenIsEmptyNotMissing(t *testing.T) {
	lb := &Longbridge{quotes: &fakeQuotes{dist: quote.CapitalDistribution{
		CapitalIn: quote.Capital{Large: dec("0"), Medium: dec("0"), Small: dec("0")}, CapitalOut: quote.Capital{Large: dec("0"), Medium: dec("0"), Small: dec("0")},
	}}}
	cf, err := lb.CapitalFlow(context.Background(), "700.HK")
	if err != nil {
		t.Fatalf("pre-open flow must not error: %v", err)
	}
	if cf.Flow == nil || len(cf.Flow) != 0 {
		t.Errorf("want an empty (non-nil) flow, got %+v", cf.Flow)
	}
}
