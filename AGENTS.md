# qoder-terminal-data — Agent Guide

Go data service. **This repo owns `api/openapi.yaml`**; downstream `qoder-terminal-analyst` and `qoder-terminal-web` depend on it.

## Architecture
- `cmd/server` — entry point: reads config, selects the provider, shuts down gracefully
- `internal/provider` — the `Provider` interface; `mock.go` (offline) and `longbridge.go` (live data)
- `internal/indicators` — technical indicators as pure functions
- `internal/money` — fixed-point decimals; **all price math must go through here, never float64**
- `internal/api` — HTTP handlers that depend only on the `Provider` interface

## Rules
- **Contract first**: change `api/openapi.yaml` before changing the API and make sure `make lint-api` passes; breaking changes require a new `/v2` path.
- Symbol format is `700.HK` (`^[0-9A-Z]{1,6}\.(HK|US|SH|SZ)$`); Hong Kong symbols are priced in HKD.
- External SDKs may only appear in `internal/provider/<name>.go`, injected through small interfaces (such as `quoteAPI`); unit tests use fakes and **must never hit the real network**.
- Error responses are always `{"code","message"}` and never leak raw upstream errors to clients.
- Credentials are read from environment variables only; never commit `.env`.
- New providers or indicators: write table-driven tests first, then implement.

## Commands
- `make test` / `make lint` / `make dev` / `make dev-live`
