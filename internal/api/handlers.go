// Package api implements the HTTP layer of the data.v1 contract.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/qoder-pdsa/qoder-terminal-data/internal/indicators"
	"github.com/qoder-pdsa/qoder-terminal-data/internal/money"
	"github.com/qoder-pdsa/qoder-terminal-data/internal/provider"
)

// symbolPattern matches the Symbol parameter in api/openapi.yaml, e.g. 700.HK or AAPL.US.
var symbolPattern = regexp.MustCompile(`^[0-9A-Z]{1,6}\.(HK|US|SH|SZ)$`)

const symbolHint = "symbol must look like 700.HK"

const maxIndicatorWindow = 250

var rangeDays = map[string]int{"1M": 21, "3M": 63, "6M": 126, "1Y": 252}

// indicatorFuncs maps the kind query parameter, whose enum lives in api/openapi.yaml, to its indicator.
var indicatorFuncs = map[string]func([]money.Decimal, int) []*money.Decimal{
	"sma": indicators.SMA,
	"ema": indicators.EMA,
	"rsi": indicators.RSI,
}

// Server holds the handler dependencies.
type Server struct {
	Provider provider.Provider
	Log      *slog.Logger
}

// Routes registers all routes.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /v1/quotes/{symbol}", s.quote)
	mux.HandleFunc("GET /v1/history/{symbol}", s.history)
	mux.HandleFunc("GET /v1/news", s.news)
	mux.HandleFunc("GET /v1/indicators/{symbol}", s.indicator)
	return withCORS(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) quote(w http.ResponseWriter, r *http.Request) {
	symbol, ok := s.symbol(w, r)
	if !ok {
		return
	}
	q, err := s.Provider.Quote(r.Context(), symbol)
	if err != nil {
		s.providerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"symbol":        q.Symbol,
		"price":         q.Price.String(),
		"change":        q.Change.String(),
		"changePercent": q.ChangePercent.String(),
		"currency":      q.Currency,
		"asOf":          q.AsOf.Format(time.RFC3339),
	})
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	symbol, ok := s.symbol(w, r)
	if !ok {
		return
	}
	days, ok := s.days(w, r.URL.Query().Get("range"))
	if !ok {
		return
	}
	candles, err := s.Provider.History(r.Context(), symbol, days)
	if err != nil {
		s.providerError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(candles))
	for _, c := range candles {
		out = append(out, map[string]any{
			"time": c.Time.Format(time.RFC3339), "open": c.Open.String(), "high": c.High.String(),
			"low": c.Low.String(), "close": c.Close.String(), "volume": c.Volume,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) news(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol != "" && !symbolPattern.MatchString(symbol) {
		writeError(w, http.StatusBadRequest, "invalid_symbol", symbolHint)
		return
	}
	limit := 10
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 50 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be 1..50")
			return
		}
		limit = n
	}
	items, err := s.Provider.News(r.Context(), symbol, limit)
	if err != nil {
		s.providerError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, n := range items {
		out = append(out, map[string]any{
			"id": n.ID, "headline": n.Headline, "summary": n.Summary, "source": n.Source,
			"url": n.URL, "symbols": n.Symbols, "publishedAt": n.PublishedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) indicator(w http.ResponseWriter, r *http.Request) {
	symbol, ok := s.symbol(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	kind := q.Get("kind")
	compute, ok := indicatorFuncs[kind]
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_kind", "kind must be one of sma, ema, rsi")
		return
	}
	window, err := strconv.Atoi(q.Get("window"))
	if err != nil || window < 1 || window > maxIndicatorWindow {
		writeError(w, http.StatusBadRequest, "invalid_window", "window must be 1..250")
		return
	}
	days, ok := s.days(w, q.Get("range"))
	if !ok {
		return
	}
	candles, err := s.Provider.History(r.Context(), symbol, days)
	if err != nil {
		s.providerError(w, err)
		return
	}
	closes := make([]money.Decimal, len(candles))
	for i, c := range candles {
		closes[i] = c.Close
	}
	values := compute(closes, window)
	points := make([]map[string]any, len(candles))
	for i, c := range candles {
		var value any
		if values[i] != nil {
			value = values[i].String()
		}
		points[i] = map[string]any{"time": c.Time.Format(time.RFC3339), "value": value}
	}
	writeJSON(w, http.StatusOK, map[string]any{"symbol": symbol, "kind": kind, "window": window, "points": points})
}

// days converts the range parameter to a number of trading days, defaulting to 3M.
func (s *Server) days(w http.ResponseWriter, rng string) (int, bool) {
	if rng == "" {
		rng = "3M"
	}
	days, ok := rangeDays[rng]
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_range", "range must be one of 1M, 3M, 6M, 1Y")
	}
	return days, ok
}

func (s *Server) symbol(w http.ResponseWriter, r *http.Request) (string, bool) {
	symbol := r.PathValue("symbol")
	if !symbolPattern.MatchString(symbol) {
		writeError(w, http.StatusBadRequest, "invalid_symbol", symbolHint)
		return "", false
	}
	return symbol, true
}

func (s *Server) providerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, provider.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "symbol not found")
		return
	case errors.Is(err, provider.ErrSymbolRequired):
		writeError(w, http.StatusBadRequest, "symbol_required", "this data provider requires a symbol")
		return
	}
	s.Log.Error("provider failure", "err", err)
	writeError(w, http.StatusBadGateway, "provider_error", "upstream data provider failed")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}

// withCORS allows cross-origin access from the local terminal frontend (demo only).
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		next.ServeHTTP(w, r)
	})
}
