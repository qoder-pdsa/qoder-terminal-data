package provider

import (
	"context"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/qoder-pdsa/qoder-terminal-data/internal/money"
)

// Mock generates deterministic fake data: the same symbol on the same day always yields the same result, keeping tests and offline demos stable.
type Mock struct {
	Now func() time.Time
}

// NewMock creates a mock provider that uses the real clock.
func NewMock() *Mock { return &Mock{Now: time.Now} }

// knownSymbols lists the Hong Kong symbols used in the demo.
var knownSymbols = map[string]string{
	"700.HK":  "Tencent",
	"9988.HK": "Alibaba",
	"3690.HK": "Meituan",
	"1810.HK": "Xiaomi",
	"1211.HK": "BYD",
	"2800.HK": "Tracker Fund",
}

var newsRotation = []string{"700.HK", "9988.HK", "3690.HK", "1810.HK"}

func seed(symbol string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(symbol))
	return int64(h.Sum64() % 1_000_000)
}

// closeUnits returns the close price in minimal units for dayOffset (0 = today), roughly 30~570.
func closeUnits(symbol string, dayOffset int) int64 {
	s := seed(symbol)
	base := 500_000 + s*5 // 50.0000 ~ 550.0000
	wave := (s + int64(dayOffset)*7919) % 40_000
	return base + wave - 20_000
}

func (m *Mock) ensure(symbol string) error {
	if _, ok := knownSymbols[symbol]; !ok {
		return fmt.Errorf("mock %s: %w", symbol, ErrNotFound)
	}
	return nil
}

// Quote implements Provider.
func (m *Mock) Quote(_ context.Context, symbol string) (Quote, error) {
	if err := m.ensure(symbol); err != nil {
		return Quote{}, err
	}
	price := money.FromUnits(closeUnits(symbol, 0))
	prev := money.FromUnits(closeUnits(symbol, 1))
	change := price.Sub(prev)
	return Quote{
		Symbol:        symbol,
		Price:         price,
		Change:        change,
		ChangePercent: change.PercentOf(prev),
		Currency:      CurrencyOf(symbol),
		AsOf:          m.Now().UTC(),
	}, nil
}

// History implements Provider, returning candles in ascending time order.
func (m *Mock) History(_ context.Context, symbol string, days int) ([]Candle, error) {
	if err := m.ensure(symbol); err != nil {
		return nil, err
	}
	today := m.Now().UTC().Truncate(24 * time.Hour)
	candles := make([]Candle, 0, days)
	for offset := days - 1; offset >= 0; offset-- {
		c := closeUnits(symbol, offset)
		o := closeUnits(symbol, offset+1)
		hi, lo := max(o, c)+1500, min(o, c)-1500
		candles = append(candles, Candle{
			Time:   today.AddDate(0, 0, -offset),
			Open:   money.FromUnits(o),
			High:   money.FromUnits(hi),
			Low:    money.FromUnits(lo),
			Close:  money.FromUnits(c),
			Volume: 1_000_000 + seed(symbol)*int64(offset+1)%5_000_000,
		})
	}
	return candles, nil
}

// News implements Provider. An empty symbol returns market-wide news.
func (m *Mock) News(_ context.Context, symbol string, limit int) ([]NewsItem, error) {
	if symbol != "" {
		if err := m.ensure(symbol); err != nil {
			return nil, err
		}
	}
	now := m.Now().UTC()
	items := make([]NewsItem, 0, limit)
	for i := 0; i < limit; i++ {
		sym := symbol
		if sym == "" {
			sym = newsRotation[i%len(newsRotation)]
		}
		items = append(items, NewsItem{
			ID:          fmt.Sprintf("mock-%s-%d", sym, i),
			Headline:    fmt.Sprintf("[MOCK] %s news #%d", knownSymbols[sym], i+1),
			Summary:     "Mock news item for offline demo.",
			Source:      "Qoder Mock Wire",
			URL:         fmt.Sprintf("https://example.com/news/%s/%d", sym, i),
			Symbols:     []string{sym},
			PublishedAt: now.Add(-time.Duration(i) * time.Hour),
		})
	}
	return items, nil
}
