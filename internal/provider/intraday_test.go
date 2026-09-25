package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/longbridge/openapi-go/quote"
)

func TestMockIntradayStartsAtOpenAndTracksPrevClose(t *testing.T) {
	m := &Mock{Now: func() time.Time { return time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC) }} // 11:00 HKT
	in, err := m.Intraday(context.Background(), "700.HK")
	if err != nil {
		t.Fatal(err)
	}
	if in.Symbol != "700.HK" || in.Currency != "HKD" {
		t.Errorf("header: %+v", in)
	}
	q, _ := m.Quote(context.Background(), "700.HK")
	if in.PrevClose != q.Price.Sub(q.Change) {
		t.Errorf("prevClose %s must equal quote price - change %s", in.PrevClose, q.Price.Sub(q.Change))
	}
	if len(in.Points) != 91 { // 09:30 .. 11:00 inclusive
		t.Fatalf("points = %d, want 91", len(in.Points))
	}
	if !in.Points[0].Time.Equal(time.Date(2026, 9, 25, 1, 30, 0, 0, time.UTC)) {
		t.Errorf("first point at %s, want 09:30 HKT", in.Points[0].Time)
	}
	for i := 1; i < len(in.Points); i++ {
		if !in.Points[i].Time.After(in.Points[i-1].Time) {
			t.Fatalf("not ascending at %d", i)
		}
		if in.Points[i].Volume <= 0 || in.Points[i].AvgPrice.Units() <= 0 {
			t.Fatalf("point %d has no volume/avg: %+v", i, in.Points[i])
		}
	}
	again, _ := m.Intraday(context.Background(), "700.HK")
	if again.Points[50] != in.Points[50] {
		t.Error("mock intraday must be deterministic")
	}
}

func TestMockIntradaySkipsLunchAndStopsAtClose(t *testing.T) {
	m := &Mock{Now: func() time.Time { return time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC) }} // after the close
	in, err := m.Intraday(context.Background(), "9988.HK")
	if err != nil {
		t.Fatal(err)
	}
	// 09:30-12:00 is 151 minutes, 13:00-16:00 is 181 minutes
	if len(in.Points) != 332 {
		t.Errorf("points = %d, want 332 for a full session", len(in.Points))
	}
	for _, p := range in.Points {
		h, mnt, _ := p.Time.UTC().Clock()
		if h == 4 && mnt > 0 { // 12:01-12:59 HKT
			t.Fatalf("point inside the lunch break: %s", p.Time)
		}
	}
}

func TestMockIntradayEmptyBeforeOpen(t *testing.T) {
	m := &Mock{Now: func() time.Time { return time.Date(2026, 9, 25, 0, 30, 0, 0, time.UTC) }} // 08:30 HKT
	in, err := m.Intraday(context.Background(), "700.HK")
	if err != nil {
		t.Fatal(err)
	}
	if in.Points == nil || len(in.Points) != 0 {
		t.Errorf("want empty non-nil points, got %+v", in.Points)
	}
	if _, err := m.Intraday(context.Background(), "0000.HK"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown symbol: want ErrNotFound, got %v", err)
	}
}

func TestLongbridgeIntradayMapsLinesAndPrevClose(t *testing.T) {
	lb := &Longbridge{quotes: &fakeQuotes{
		quotes: []*quote.SecurityQuote{{Symbol: "700.HK", LastDone: dec("438.4"), PrevClose: dec("441.0"), Timestamp: 1_758_000_000}},
		lines: []*quote.IntradayLine{
			{Price: dec("440.2"), AvgPrice: dec("440.2"), Volume: 1000, Timestamp: 1_758_000_000},
			nil,
			{Price: dec("438.4"), AvgPrice: dec("439.3"), Volume: 2000, Timestamp: 1_758_000_060},
		},
	}}
	in, err := lb.Intraday(context.Background(), "700.HK")
	if err != nil {
		t.Fatal(err)
	}
	if in.PrevClose.String() != "441.0000" || in.Currency != "HKD" {
		t.Errorf("header: %+v", in)
	}
	if len(in.Points) != 2 || in.Points[1].AvgPrice.String() != "439.3000" || in.Points[1].Volume != 2000 || in.Points[1].Time.Unix() != 1_758_000_060 {
		t.Errorf("points: %+v", in.Points)
	}
}

func TestLongbridgeIntradayBeforeOpenIsEmpty(t *testing.T) {
	lb := &Longbridge{quotes: &fakeQuotes{
		quotes: []*quote.SecurityQuote{{Symbol: "700.HK", LastDone: dec("441.0"), PrevClose: dec("441.0")}},
	}}
	in, err := lb.Intraday(context.Background(), "700.HK")
	if err != nil {
		t.Fatal(err)
	}
	if in.Points == nil || len(in.Points) != 0 {
		t.Errorf("want empty non-nil points, got %+v", in.Points)
	}
}

func TestLongbridgeIntradayUnknownSymbolIsNotFound(t *testing.T) {
	lb := &Longbridge{quotes: &fakeQuotes{}}
	if _, err := lb.Intraday(context.Background(), "0000.HK"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}
