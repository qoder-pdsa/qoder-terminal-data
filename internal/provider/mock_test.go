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

// TestMockQuoteOHLCVIsDeterministic pins the session open/high/low/volume/turnover the mock
// derives for every known symbol. The expected values follow the documented mock formula rather
// than being captured from a run: with s = seed(symbol) = fnv64a(symbol) % 1e6,
//
//	close(dayOffset) = 500_000 + s*5 + (s + dayOffset*7919) % 40_000 - 20_000   (units of 1e-4)
//	open  = close(1)          the previous day's close is today's open, as in History
//	price = close(0)
//	high  = max(open, price) + 1500
//	low   = min(open, price) - 1500
//	volume   = 1_000_000 + s % 5_000_000
//	turnover = price * volume
//
// Worked example, 700.HK: s = 601441, so the base is 3_507_205; wave(0) = 601441 % 40_000 = 1441
// gives close 3_488_646 = 348.8646, and wave(1) = 609360 % 40_000 = 9360 gives open 3_496_565 =
// 349.6565. high = 3_496_565 + 1500 = 349.8065, low = 3_488_646 - 1500 = 348.7146,
// volume = 1_601_441 and turnover = 348.8646 * 1_601_441 = 558686073.8886.
func TestMockQuoteOHLCVIsDeterministic(t *testing.T) {
	tests := []struct {
		symbol                                   string
		open, high, low, price, change, turnover string
		volume                                   int64
	}{
		{"700.HK", "349.6565", "349.8065", "348.7146", "348.8646", "-0.7919", "558686073.8886", 1_601_441},
		{"9988.HK", "385.0947", "388.4528", "384.9447", "388.3028", "3.2081", "649955982.1464", 1_673_838},
		{"3690.HK", "341.0655", "341.2155", "340.1236", "340.2736", "-0.7919", "537787452.7616", 1_580_456},
		{"1810.HK", "138.5071", "138.6571", "137.5652", "137.7152", "-0.7919", "161979516.5184", 1_176_192},
		{"1211.HK", "438.5113", "438.6613", "437.5694", "437.7194", "-0.7919", "777476760.5606", 1_776_199},
		{"2800.HK", "135.9715", "136.1215", "135.0296", "135.1796", "-0.7919", "158425895.0936", 1_171_966},
	}
	m := fixedMock()
	for _, tt := range tests {
		got, err := m.Quote(context.Background(), tt.symbol)
		if err != nil {
			t.Fatalf("%s: %v", tt.symbol, err)
		}
		fields := []struct{ name, want, actual string }{
			{"open", tt.open, got.Open.String()},
			{"high", tt.high, got.High.String()},
			{"low", tt.low, got.Low.String()},
			{"price", tt.price, got.Price.String()},
			{"change", tt.change, got.Change.String()},
			{"turnover", tt.turnover, got.Turnover.String()},
		}
		for _, f := range fields {
			if f.actual != f.want {
				t.Errorf("%s: %s = %s, want %s", tt.symbol, f.name, f.actual, f.want)
			}
		}
		if got.Volume != tt.volume {
			t.Errorf("%s: volume = %d, want %d", tt.symbol, got.Volume, tt.volume)
		}
	}
}

// TestMockQuoteOHLCVInvariants is the acceptance invariant: the session range must contain both
// the open and the close for every known symbol, and the session must not be degenerate.
func TestMockQuoteOHLCVInvariants(t *testing.T) {
	m := fixedMock()
	for symbol := range knownSymbols {
		q, err := m.Quote(context.Background(), symbol)
		if err != nil {
			t.Fatal(err)
		}
		if q.Low.Units() > q.Open.Units() {
			t.Errorf("%s: low %s > open %s", symbol, q.Low, q.Open)
		}
		if q.Low.Units() > q.Price.Units() {
			t.Errorf("%s: low %s > close %s", symbol, q.Low, q.Price)
		}
		if q.High.Units() < q.Open.Units() {
			t.Errorf("%s: high %s < open %s", symbol, q.High, q.Open)
		}
		if q.High.Units() < q.Price.Units() {
			t.Errorf("%s: high %s < close %s", symbol, q.High, q.Price)
		}
		if q.High.Units() <= q.Low.Units() {
			t.Errorf("%s: high %s <= low %s, the session range is degenerate", symbol, q.High, q.Low)
		}
		if q.Volume <= 0 {
			t.Errorf("%s: volume = %d, want a positive session volume", symbol, q.Volume)
		}
		if q.Turnover.Units() <= 0 {
			t.Errorf("%s: turnover = %s, want a positive session turnover", symbol, q.Turnover)
		}
	}
}

// TestMockQuotesCarryOHLCV locks that the batch path returns the same session stats as the single
// path, because the web watchlist and the Q panel must not disagree.
func TestMockQuotesCarryOHLCV(t *testing.T) {
	m := fixedMock()
	single, err := m.Quote(context.Background(), "700.HK")
	if err != nil {
		t.Fatal(err)
	}
	batch, err := m.Quotes(context.Background(), []string{"9988.HK", "700.HK"})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != 2 {
		t.Fatalf("batch length = %d, want 2", len(batch))
	}
	if batch[1] != single {
		t.Errorf("batch quote differs from single quote:\n batch=%+v\nsingle=%+v", batch[1], single)
	}
	if batch[0].Open.Units() == 0 || batch[0].Volume == 0 {
		t.Errorf("9988.HK: batch quote lost its session stats: %+v", batch[0])
	}
}
