# qoder-terminal-data

Qoder Terminal 的**数据服务**（Go）：行情、K 线、新闻、技术指标。
接口契约：[`api/openapi.yaml`](api/openapi.yaml)（本 repo 是该契约的唯一维护方）。

```bash
make dev        # mock 数据，:8081
make dev-live   # Longbridge 真实港股行情
make test
make lint       # go vet + gofmt + OpenAPI 校验

curl localhost:8081/v1/quotes/700.HK
curl "localhost:8081/v1/indicators/700.HK?kind=sma&window=20"
```

## Provider

| `DATA_PROVIDER` | 状态 | 说明 |
|---|---|---|
| `mock` | ✅ | 确定性假数据（腾讯、阿里、美团、小米、比亚迪、盈富基金），测试与离线兜底 |
| `longbridge` | ✅ 已接入（待真实账户验证） | 报价、日 K（前复权）、资讯 |
| `replay` | 🚧 backlog | 回放录制的盘中行情，保证演示可复现 |

### Longbridge 配置

复制 `.env.example` 为 `.env` 并填写，或直接导出环境变量：

```bash
export LONGBRIDGE_APP_KEY=... LONGBRIDGE_APP_SECRET=... LONGBRIDGE_ACCESS_TOKEN=...
export HTTPS_PROXY=http://127.0.0.1:7897   # 本机有 Clash 等代理时必须设置，否则连接超时
```

## 港股交易时段（北京时间）
09:30–12:00、13:00–16:00。午休期间价格不动，演示请避开。
