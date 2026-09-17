package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // distroless images ship no tz database; embed it to support America/New_York

	"github.com/longbridge/openapi-go/config"
	"github.com/longbridge/openapi-go/content"
	"github.com/longbridge/openapi-go/quote"
	"github.com/shopspring/decimal"

	"github.com/qoder-pdsa/qoder-terminal-data/internal/money"
)

// maxCandles is the Longbridge limit for a single candlestick request.
const maxCandles = 1000

// quoteAPI is the subset of Longbridge QuoteContext used by this service, so tests can substitute it.
type quoteAPI interface {
	Quote(ctx context.Context, symbols []string) ([]*quote.SecurityQuote, error)
	Candlesticks(ctx context.Context, symbol string, period quote.Period, count int32, adjust quote.AdjustType) ([]*quote.Candlestick, error)
}

// newsAPI is the subset of Longbridge ContentContext used by this service.
type newsAPI interface {
	News(ctx context.Context, symbol string) ([]*content.NewsItem, error)
}

// Longbridge fetches live market data from Longbridge OpenAPI.
type Longbridge struct {
	quotes quoteAPI
	news   newsAPI
	close  func() error
}

// NewLongbridge creates the provider from environment variables (LONGBRIDGE_APP_KEY / APP_SECRET / ACCESS_TOKEN).
// Callers must call Close on shutdown.
func NewLongbridge() (*Longbridge, error) {
	cfg, err := config.New()
	if err != nil {
		return nil, fmt.Errorf("longbridge config: %w", err)
	}
	qc, err := quote.NewFromCfg(cfg)
	if err != nil {
		return nil, fmt.Errorf("longbridge quote context: %w", err)
	}
	cc, err := content.NewFromCfg(cfg)
	if err != nil {
		_ = qc.Close()
		return nil, fmt.Errorf("longbridge content context: %w", err)
	}
	return &Longbridge{quotes: qc, news: cc, close: qc.Close}, nil
}

// Close releases the long-lived connection.
func (l *Longbridge) Close() error {
	if l.close == nil {
		return nil
	}
	return l.close()
}

// Quote implements Provider.
func (l *Longbridge) Quote(ctx context.Context, symbol string) (Quote, error) {
	quotes, err := l.quotes.Quote(ctx, []string{symbol})
	if err != nil {
		return Quote{}, fmt.Errorf("longbridge quote %s: %w", symbol, err)
	}
	if len(quotes) == 0 || quotes[0] == nil || quotes[0].LastDone == nil {
		return Quote{}, fmt.Errorf("longbridge quote %s: %w", symbol, ErrNotFound)
	}
	q := quotes[0]
	price, err := toMoney(q.LastDone)
	if err != nil {
		return Quote{}, err
	}
	prev, err := toMoney(q.PrevClose)
	if err != nil {
		return Quote{}, err
	}
	change := price.Sub(prev)
	return Quote{
		Symbol:        symbol,
		Price:         price,
		Change:        change,
		ChangePercent: change.PercentOf(prev),
		Currency:      CurrencyOf(symbol),
		AsOf:          time.Unix(q.Timestamp, 0).UTC(),
	}, nil
}

// History implements Provider, returning forward-adjusted daily candles in ascending time order.
func (l *Longbridge) History(ctx context.Context, symbol string, days int) ([]Candle, error) {
	count := min(days, maxCandles)
	sticks, err := l.quotes.Candlesticks(ctx, symbol, quote.PeriodDay, int32(count), quote.AdjustTypeForward)
	if err != nil {
		return nil, fmt.Errorf("longbridge candlesticks %s: %w", symbol, err)
	}
	if len(sticks) == 0 {
		return nil, fmt.Errorf("longbridge candlesticks %s: %w", symbol, ErrNotFound)
	}
	loc := exchangeLocation(symbol)
	candles := make([]Candle, 0, len(sticks))
	for _, s := range sticks {
		c, err := toDailyCandle(s, loc)
		if err != nil {
			return nil, fmt.Errorf("longbridge candlesticks %s: %w", symbol, err)
		}
		candles = append(candles, c)
	}
	return candles, nil
}

// News implements Provider. Longbridge news is queried per symbol, so an empty symbol returns ErrSymbolRequired.
func (l *Longbridge) News(ctx context.Context, symbol string, limit int) ([]NewsItem, error) {
	if symbol == "" {
		return nil, ErrSymbolRequired
	}
	raw, err := l.news.News(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("longbridge news %s: %w", symbol, err)
	}
	items := make([]NewsItem, 0, min(limit, len(raw)))
	for _, n := range raw {
		if len(items) == limit {
			break
		}
		if n == nil || n.Url == "" {
			continue
		}
		items = append(items, NewsItem{
			ID:          n.Id,
			Headline:    n.Title,
			Summary:     n.Description,
			Source:      "Longbridge",
			URL:         n.Url,
			Symbols:     []string{symbol},
			PublishedAt: n.PublishedAt.UTC(),
		})
	}
	return items, nil
}

// ErrSymbolRequired means the provider cannot query the whole market and needs a symbol.
var ErrSymbolRequired = errors.New("symbol is required by this provider")

func toMoney(d *decimal.Decimal) (money.Decimal, error) {
	if d == nil {
		return money.Decimal{}, errors.New("longbridge: missing decimal field")
	}
	return money.Parse(d.String())
}

// exchangeLocation returns the time zone of the symbol's exchange.
func exchangeLocation(symbol string) *time.Location {
	if strings.HasSuffix(symbol, ".US") {
		if loc, err := time.LoadLocation("America/New_York"); err == nil {
			return loc
		}
	}
	return time.FixedZone("UTC+8", 8*3600) // Hong Kong and mainland A-shares are UTC+8 with no DST
}

// tradingDate converts an exchange-local timestamp to "UTC midnight of the trading day", matching the mock provider.
// Longbridge daily candles are stamped at local midnight, so converting straight to UTC shifts the date back a day.
func tradingDate(unix int64, loc *time.Location) time.Time {
	y, m, d := time.Unix(unix, 0).In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func toDailyCandle(s *quote.Candlestick, loc *time.Location) (Candle, error) {
	if s == nil {
		return Candle{}, errors.New("longbridge: nil candlestick")
	}
	fields := []*decimal.Decimal{s.Open, s.High, s.Low, s.Close}
	parsed := make([]money.Decimal, len(fields))
	for i, f := range fields {
		v, err := toMoney(f)
		if err != nil {
			return Candle{}, err
		}
		parsed[i] = v
	}
	return Candle{
		Time:   tradingDate(s.Timestamp, loc),
		Open:   parsed[0],
		High:   parsed[1],
		Low:    parsed[2],
		Close:  parsed[3],
		Volume: s.Volume,
	}, nil
}
