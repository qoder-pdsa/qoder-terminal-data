// Package provider defines the market data source interface.
package provider

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/qoder-pdsa/qoder-terminal-data/internal/money"
)

// ErrNotFound means the symbol does not exist.
var ErrNotFound = errors.New("symbol not found")

// CurrencyOf infers the quote currency from the market suffix, e.g. 700.HK → HKD.
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

// Quote is the latest quote.
type Quote struct {
	Symbol        string
	Price         money.Decimal
	Change        money.Decimal
	ChangePercent money.Decimal
	Currency      string
	AsOf          time.Time
}

// Candle is a daily candlestick.
type Candle struct {
	Time                   time.Time
	Open, High, Low, Close money.Decimal
	Volume                 int64
}

// NewsItem is a news entry.
type NewsItem struct {
	ID          string
	Headline    string
	Summary     string
	Source      string
	URL         string
	Symbols     []string
	PublishedAt time.Time
}

// Provider is the interface every data source must implement.
type Provider interface {
	Quote(ctx context.Context, symbol string) (Quote, error)
	History(ctx context.Context, symbol string, days int) ([]Candle, error)
	News(ctx context.Context, symbol string, limit int) ([]NewsItem, error)
}
