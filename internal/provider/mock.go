package provider

import (
	"context"
	"errors"
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
	openUnits := closeUnits(symbol, 1)
	priceUnits := closeUnits(symbol, 0)
	price := money.FromUnits(priceUnits)
	prev := money.FromUnits(openUnits)
	change := price.Sub(prev)
	volume := 1_000_000 + seed(symbol)%5_000_000
	return Quote{
		Symbol:        symbol,
		Price:         price,
		Change:        change,
		ChangePercent: change.PercentOf(prev),
		// The session range mirrors the offset-0 daily candle in History: the day opens at the
		// previous close and trades 0.1500 beyond the open/close extremes, so the invariant
		// low <= open, close <= high holds for every symbol by construction. Turnover is the
		// last price times the volume; the mock has no intraday trade tape to integrate.
		Open:     prev,
		High:     money.FromUnits(max(openUnits, priceUnits) + 1500),
		Low:      money.FromUnits(min(openUnits, priceUnits) - 1500),
		Volume:   volume,
		Turnover: price.MulInt(volume),
		Currency: CurrencyOf(symbol),
		AsOf:     m.Now().UTC(),
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

// demoWatchlist is the mock's single watchlist group, in a stable display order.
var demoWatchlist = []string{"700.HK", "9988.HK", "3690.HK", "1810.HK", "1211.HK", "2800.HK"}

// capitalFlowMinutes is how many one-minute points the mock capital flow covers.
const capitalFlowMinutes = 60

// Quotes implements Provider; symbols the mock does not know are omitted, like a real provider does for options.
func (m *Mock) Quotes(ctx context.Context, symbols []string) ([]Quote, error) {
	quotes := make([]Quote, 0, len(symbols))
	for _, symbol := range symbols {
		q, err := m.Quote(ctx, symbol)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		quotes = append(quotes, q)
	}
	return quotes, nil
}

// Watchlists implements Provider with one demo group of the known symbols.
func (m *Mock) Watchlists(context.Context) ([]Watchlist, error) {
	symbols := make([]WatchedSymbol, 0, len(demoWatchlist))
	for _, s := range demoWatchlist {
		symbols = append(symbols, WatchedSymbol{Symbol: s, Name: knownSymbols[s]})
	}
	return []Watchlist{{ID: "demo", Name: "Demo", Symbols: symbols}}, nil
}

// CapitalFlow implements Provider: deterministic per-minute net inflow ending now, with both signs.
func (m *Mock) CapitalFlow(_ context.Context, symbol string) (CapitalFlow, error) {
	if err := m.ensure(symbol); err != nil {
		return CapitalFlow{}, err
	}
	s := seed(symbol)
	end := m.Now().UTC().Truncate(time.Minute)
	flow := make([]CapitalFlowPoint, 0, capitalFlowMinutes)
	for i := capitalFlowMinutes - 1; i >= 0; i-- {
		wave := (s + int64(i)*104_729) % 2_000_000 // 0 ~ 1,999,999
		amount := (wave - 1_000_000) * 10_000      // ±1,000.0000 × 10,000 → ±10,000,000.0000
		flow = append(flow, CapitalFlowPoint{Time: end.Add(-time.Duration(i) * time.Minute), Inflow: money.FromUnits(amount)})
	}
	bucket := func(k int64) money.Decimal { return money.FromUnits((s*k%5_000_000 + 1_000_000) * 10_000) }
	return CapitalFlow{
		Symbol:   symbol,
		Currency: CurrencyOf(symbol),
		AsOf:     end,
		Flow:     flow,
		In:       CapitalBuckets{Large: bucket(3), Medium: bucket(5), Small: bucket(7)},
		Out:      CapitalBuckets{Large: bucket(11), Medium: bucket(13), Small: bucket(17)},
	}, nil
}

// Hong Kong session in UTC: 09:30-12:00 and 13:00-16:00 HKT.
var hkSessions = [][2]int{{1*60 + 30, 4 * 60}, {5 * 60, 8 * 60}}

// Intraday implements Provider: a deterministic minute line from the open up to now, skipping lunch,
// oscillating around the previous close so the chart shows both sides of the baseline.
func (m *Mock) Intraday(_ context.Context, symbol string) (Intraday, error) {
	if err := m.ensure(symbol); err != nil {
		return Intraday{}, err
	}
	s := seed(symbol)
	prev := money.FromUnits(closeUnits(symbol, 1))
	now := m.Now().UTC()
	day := now.Truncate(24 * time.Hour)
	nowMinute := int(now.Sub(day) / time.Minute)
	points := make([]IntradayPoint, 0, 332)
	var sumUnits, sumVolume int64
	for _, session := range hkSessions {
		for minute := session[0]; minute <= session[1] && minute <= nowMinute; minute++ {
			wave := (s + int64(minute)*7919) % 20_000 // ±1.0000 around the previous close
			price := money.FromUnits(prev.Units() + wave - 10_000)
			volume := 10_000 + (s+int64(minute)*104_729)%90_000
			sumUnits += price.Units() * volume
			sumVolume += volume
			points = append(points, IntradayPoint{
				Time:     day.Add(time.Duration(minute) * time.Minute),
				Price:    price,
				AvgPrice: money.FromUnits(sumUnits / sumVolume),
				Volume:   volume,
			})
		}
	}
	return Intraday{Symbol: symbol, Currency: CurrencyOf(symbol), PrevClose: prev, Points: points}, nil
}
