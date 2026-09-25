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

// WatchedSymbol is one entry of a watchlist group.
type WatchedSymbol struct {
	Symbol string
	Name   string
}

// Watchlist is a named group of watched symbols.
type Watchlist struct {
	ID      string
	Name    string
	Symbols []WatchedSymbol
}

// CapitalFlowPoint is the net inflow of one minute; negative means outflow.
type CapitalFlowPoint struct {
	Time   time.Time
	Inflow money.Decimal
}

// CapitalBuckets splits an amount by order size.
type CapitalBuckets struct {
	Large, Medium, Small money.Decimal
}

// Sub returns b minus o per bucket.
func (b CapitalBuckets) Sub(o CapitalBuckets) CapitalBuckets {
	return CapitalBuckets{Large: b.Large.Sub(o.Large), Medium: b.Medium.Sub(o.Medium), Small: b.Small.Sub(o.Small)}
}

// CapitalFlow is the intraday capital flow of a symbol.
type CapitalFlow struct {
	Symbol   string
	Currency string
	AsOf     time.Time
	Flow     []CapitalFlowPoint
	In, Out  CapitalBuckets
}

// IntradayPoint is one minute of the current session.
type IntradayPoint struct {
	Time     time.Time
	Price    money.Decimal
	AvgPrice money.Decimal
	Volume   int64
}

// Intraday is the minute line of the current session; Points is empty before the first trade.
type Intraday struct {
	Symbol    string
	Currency  string
	PrevClose money.Decimal
	Points    []IntradayPoint
}

// Provider is the interface every data source must implement.
type Provider interface {
	Quote(ctx context.Context, symbol string) (Quote, error)
	// Quotes returns one quote per symbol in the requested order, using a single upstream call where possible.
	Quotes(ctx context.Context, symbols []string) ([]Quote, error)
	History(ctx context.Context, symbol string, days int) ([]Candle, error)
	News(ctx context.Context, symbol string, limit int) ([]NewsItem, error)
	Watchlists(ctx context.Context) ([]Watchlist, error)
	CapitalFlow(ctx context.Context, symbol string) (CapitalFlow, error)
	Intraday(ctx context.Context, symbol string) (Intraday, error)
}
