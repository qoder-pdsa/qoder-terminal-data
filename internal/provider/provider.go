// Package provider 定义行情数据源接口。
package provider

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/qoder-pdsa/qoder-terminal-data/internal/money"
)

// ErrNotFound 表示标的不存在。
var ErrNotFound = errors.New("symbol not found")

// CurrencyOf 根据标的市场后缀推断计价货币，如 700.HK → HKD。
func CurrencyOf(symbol string) string {
	switch {
	case strings.HasSuffix(symbol, ".HK"):
		return "HKD"
	case strings.HasSuffix(symbol, ".SH"), strings.HasSuffix(symbol, ".SZ"):
		return "CNY"
	default:
		return "USD"
	}
}

// Quote 最新报价。
type Quote struct {
	Symbol        string
	Price         money.Decimal
	Change        money.Decimal
	ChangePercent money.Decimal
	Currency      string
	AsOf          time.Time
}

// Candle 日线。
type Candle struct {
	Time                   time.Time
	Open, High, Low, Close money.Decimal
	Volume                 int64
}

// NewsItem 新闻条目。
type NewsItem struct {
	ID          string
	Headline    string
	Summary     string
	Source      string
	URL         string
	Symbols     []string
	PublishedAt time.Time
}

// Provider 是所有数据源必须实现的接口。
type Provider interface {
	Quote(ctx context.Context, symbol string) (Quote, error)
	History(ctx context.Context, symbol string, days int) ([]Candle, error)
	News(ctx context.Context, symbol string, limit int) ([]NewsItem, error)
}
