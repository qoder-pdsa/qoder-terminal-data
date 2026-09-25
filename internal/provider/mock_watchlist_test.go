package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/qoder-pdsa/qoder-terminal-data/internal/money"
)

func watchMock() *Mock {
	return &Mock{Now: func() time.Time { return time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC) }}
}

func TestMockQuotesKeepRequestedOrder(t *testing.T) {
	m := watchMock()
	quotes, err := m.Quotes(context.Background(), []string{"9988.HK", "700.HK"})
	if err != nil {
		t.Fatal(err)
	}
	if len(quotes) != 2 || quotes[0].Symbol != "9988.HK" || quotes[1].Symbol != "700.HK" {
		t.Fatalf("unexpected order: %+v", quotes)
	}
	single, _ := m.Quote(context.Background(), "700.HK")
	if quotes[1] != single {
		t.Errorf("batch quote differs from single quote: %+v vs %+v", quotes[1], single)
	}
}

func TestMockQuotesUnknownSymbolIsNotFound(t *testing.T) {
	if _, err := watchMock().Quotes(context.Background(), []string{"700.HK", "0000.HK"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestMockWatchlistsHasOneDemoGroupOfKnownSymbols(t *testing.T) {
	lists, err := watchMock().Watchlists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(lists) != 1 || lists[0].ID == "" || lists[0].Name == "" {
		t.Fatalf("want one named group, got %+v", lists)
	}
	if len(lists[0].Symbols) != len(knownSymbols) {
		t.Fatalf("group has %d symbols, want %d", len(lists[0].Symbols), len(knownSymbols))
	}
	for _, s := range lists[0].Symbols {
		if knownSymbols[s.Symbol] != s.Name {
			t.Errorf("%s: name %q, want %q", s.Symbol, s.Name, knownSymbols[s.Symbol])
		}
	}
	if lists[0].Symbols[0].Symbol != "700.HK" {
		t.Errorf("first symbol = %s, want 700.HK (stable order)", lists[0].Symbols[0].Symbol)
	}
}

func TestMockCapitalFlowDeterministicAndAscending(t *testing.T) {
	m := watchMock()
	a, err := m.CapitalFlow(context.Background(), "700.HK")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := m.CapitalFlow(context.Background(), "700.HK")
	if len(a.Flow) < 30 {
		t.Fatalf("want at least 30 minutes of flow, got %d", len(a.Flow))
	}
	for i := 1; i < len(a.Flow); i++ {
		if !a.Flow[i].Time.After(a.Flow[i-1].Time) {
			t.Fatalf("flow not ascending at %d", i)
		}
		if a.Flow[i].Inflow != b.Flow[i].Inflow {
			t.Fatalf("flow not deterministic at %d", i)
		}
	}
	if a.Currency != "HKD" || a.Symbol != "700.HK" || a.AsOf.IsZero() {
		t.Errorf("header: %+v", a)
	}
	negative := false
	for _, p := range a.Flow {
		if p.Inflow.Units() < 0 {
			negative = true
		}
	}
	if !negative {
		t.Error("mock flow should include outflow minutes so the chart shows both colors")
	}
	if a.In.Large.Units() <= 0 || a.Out.Small.Units() <= 0 {
		t.Errorf("distribution buckets must be positive amounts: in=%+v out=%+v", a.In, a.Out)
	}
	if _, err := m.CapitalFlow(context.Background(), "0000.HK"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown symbol: want ErrNotFound, got %v", err)
	}
}

func TestCapitalBucketsSub(t *testing.T) {
	in := CapitalBuckets{Large: mustParse("10.5"), Medium: mustParse("3"), Small: mustParse("1")}
	out := CapitalBuckets{Large: mustParse("4"), Medium: mustParse("5"), Small: mustParse("1")}
	net := in.Sub(out)
	if net.Large.String() != "6.5000" || net.Medium.String() != "-2.0000" || net.Small.String() != "0.0000" {
		t.Errorf("net = %+v", net)
	}
}

func mustParse(s string) money.Decimal {
	d, err := money.Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}
