# qoder-terminal-data

The **data service** (Go) for Qoder Terminal: quotes, candlesticks, news, and technical indicators.
API contract: [`api/openapi.yaml`](api/openapi.yaml) (this repo is the single owner of that contract).

```bash
make dev        # mock data on :8081
make dev-live   # live Hong Kong market data from Longbridge
make test
make lint       # go vet + gofmt + OpenAPI lint

curl localhost:8081/v1/quotes/700.HK
curl "localhost:8081/v1/indicators/700.HK?kind=sma&window=20"
curl "localhost:8081/v1/indicators/700.HK?kind=ema&window=20"
curl "localhost:8081/v1/indicators/700.HK?kind=rsi&window=14"
```

## Providers

| `DATA_PROVIDER` | Status | Notes |
|---|---|---|
| `mock` | ✅ | Deterministic fake data (Tencent, Alibaba, Meituan, Xiaomi, BYD, Tracker Fund) for tests and offline fallback |
| `longbridge` | ✅ | Quotes, forward-adjusted daily candles, and news |
| `replay` | 🚧 backlog | Replays recorded intraday data so demos are reproducible |

### Longbridge configuration

Copy `.env.example` to `.env` and fill it in, or export the variables directly:

```bash
export LONGBRIDGE_APP_KEY=... LONGBRIDGE_APP_SECRET=... LONGBRIDGE_ACCESS_TOKEN=...
export HTTPS_PROXY=http://127.0.0.1:7897   # required when a local proxy such as Clash is running, otherwise connections time out
```

## Hong Kong trading hours (HKT)
09:30–12:00 and 13:00–16:00. Prices do not move during the lunch break, so avoid it when demoing.
