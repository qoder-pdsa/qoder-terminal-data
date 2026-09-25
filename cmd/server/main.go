package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/qoder-pdsa/qoder-terminal-data/internal/api"
	"github.com/qoder-pdsa/qoder-terminal-data/internal/provider"
)

const shutdownTimeout = 5 * time.Second

// historyCacheTTL is how long daily candles are reused; it collapses the burst of history + indicator
// requests one graph panel (or an ASK comparison opening several) makes into one upstream call per symbol.
const historyCacheTTL = time.Minute

// intradayCacheTTL is how long the minute line is reused; every open Q panel re-polls it every 10 s.
const intradayCacheTTL = 10 * time.Second

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("qoder-terminal-data stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	addr := envOr("DATA_ADDR", ":8081")
	name := envOr("DATA_PROVIDER", "mock")

	p, closeProvider, err := buildProvider(name)
	if err != nil {
		return err
	}
	defer func() { _ = closeProvider() }()

	srv := &http.Server{
		Addr:              addr,
		Handler:           (&api.Server{Provider: provider.NewCached(p, historyCacheTTL, intradayCacheTTL), Log: log}).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Info("qoder-terminal-data listening", "addr", addr, "provider", name)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func buildProvider(name string) (provider.Provider, func() error, error) {
	switch name {
	case "mock":
		return provider.NewMock(), func() error { return nil }, nil
	case "longbridge":
		lb, err := provider.NewLongbridge()
		if err != nil {
			return nil, nil, fmt.Errorf("init longbridge provider (check LONGBRIDGE_* env and HTTPS_PROXY): %w", err)
		}
		return lb, lb.Close, nil
	default:
		return nil, nil, fmt.Errorf("unsupported DATA_PROVIDER %q (mock | longbridge)", name)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
