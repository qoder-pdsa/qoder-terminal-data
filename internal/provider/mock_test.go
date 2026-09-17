package provider

import (
	"context"
	"errors"
	"testing"
	"time"
)

func fixedMock() *Mock {
	return &Mock{Now: func() time.Time { return time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC) }}
}

func TestMockQuoteDeterministic(t *testing.T) {
	m := fixedMock()
	a, err := m.Quote(context.Background(), "700.HK")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := m.Quote(context.Background(), "700.HK")
	if a.Price != b.Price {
		t.Errorf("quote not deterministic: %s vs %s", a.Price, b.Price)
	}
}

func TestMockUnknownSymbol(t *testing.T) {
	_, err := fixedMock().Quote(context.Background(), "0000.HK")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestMockHistoryAscending(t *testing.T) {
	candles, err := fixedMock().History(context.Background(), "9988.HK", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(candles) != 30 {
		t.Fatalf("len = %d, want 30", len(candles))
	}
	for i := 1; i < len(candles); i++ {
		if !candles[i].Time.After(candles[i-1].Time) {
			t.Fatalf("candles not ascending at %d", i)
		}
		if candles[i].High.Units() < candles[i].Low.Units() {
			t.Fatalf("high < low at %d", i)
		}
	}
}

func TestMockPricesInDocumentedRange(t *testing.T) {
	m := fixedMock()
	for symbol := range knownSymbols {
		q, err := m.Quote(context.Background(), symbol)
		if err != nil {
			t.Fatal(err)
		}
		if u := q.Price.Units(); u < 300_000 || u > 5_700_000 {
			t.Errorf("%s price %s outside 30~570", symbol, q.Price)
		}
	}
}

func TestMockQuoteUsesHKD(t *testing.T) {
	q, err := fixedMock().Quote(context.Background(), "700.HK")
	if err != nil {
		t.Fatal(err)
	}
	if q.Currency != "HKD" {
		t.Errorf("currency = %s, want HKD", q.Currency)
	}
}

func TestCurrencyOf(t *testing.T) {
	tests := map[string]string{"700.HK": "HKD", "AAPL.US": "USD", "600519.SH": "CNY", "000001.SZ": "CNY"}
	for symbol, want := range tests {
		if got := CurrencyOf(symbol); got != want {
			t.Errorf("CurrencyOf(%s) = %s, want %s", symbol, got, want)
		}
	}
}
