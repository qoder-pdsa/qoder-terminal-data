package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // distroless 镜像没有时区库，内嵌以支持 America/New_York

	"github.com/longbridge/openapi-go/config"
	"github.com/longbridge/openapi-go/content"
	"github.com/longbridge/openapi-go/quote"
	"github.com/shopspring/decimal"

	"github.com/qoder-pdsa/qoder-terminal-data/internal/money"
)

// maxCandles 是 Longbridge 单次 K 线请求上限。
const maxCandles = 1000

// quoteAPI 是 Longbridge QuoteContext 中本服务用到的子集，便于测试替换。
type quoteAPI interface {
	Quote(ctx context.Context, symbols []string) ([]*quote.SecurityQuote, error)
	Candlesticks(ctx context.Context, symbol string, period quote.Period, count int32, adjust quote.AdjustType) ([]*quote.Candlestick, error)
}

// newsAPI 是 Longbridge ContentContext 中本服务用到的子集。
type newsAPI interface {
	News(ctx context.Context, symbol string) ([]*content.NewsItem, error)
}

// Longbridge 通过 Longbridge OpenAPI 获取真实行情。
type Longbridge struct {
	quotes quoteAPI
	news   newsAPI
	close  func() error
}

// NewLongbridge 从环境变量（LONGBRIDGE_APP_KEY / APP_SECRET / ACCESS_TOKEN）创建 provider。
// 调用方负责在退出时调用 Close。
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

// Close 释放长连接。
func (l *Longbridge) Close() error {
	if l.close == nil {
		return nil
	}
	return l.close()
}

// Quote 实现 Provider。
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

// History 实现 Provider，返回前复权日线，按时间升序。
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

// News 实现 Provider。Longbridge 资讯按标的查询，symbol 为空时返回 ErrSymbolRequired。
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

// ErrSymbolRequired 表示数据源不支持全市场查询，必须指定标的。
var ErrSymbolRequired = errors.New("symbol is required by this provider")

func toMoney(d *decimal.Decimal) (money.Decimal, error) {
	if d == nil {
		return money.Decimal{}, errors.New("longbridge: missing decimal field")
	}
	return money.Parse(d.String())
}

// exchangeLocation 返回标的所属交易所时区。
func exchangeLocation(symbol string) *time.Location {
	if strings.HasSuffix(symbol, ".US") {
		if loc, err := time.LoadLocation("America/New_York"); err == nil {
			return loc
		}
	}
	return time.FixedZone("UTC+8", 8*3600) // 港股与 A 股均为 UTC+8，无夏令时
}

// tradingDate 把交易所当地时间戳转换为“交易日的 UTC 零点”，与 mock provider 语义一致。
// Longbridge 日 K 的时间戳是当地零点，直接转 UTC 会让日期提前一天。
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
