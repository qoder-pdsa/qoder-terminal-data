# qoder-terminal-data — Agent 指南

Go 数据服务。**本 repo 维护 `api/openapi.yaml`**，下游 `qoder-terminal-analyst` 与 `qoder-terminal-web` 依赖它。

## 架构
- `cmd/server` — 入口：读配置、选择 provider、优雅退出
- `internal/provider` — `Provider` 接口；`mock.go`（离线）、`longbridge.go`（真实行情）
- `internal/indicators` — 技术指标纯函数
- `internal/money` — 定点十进制；**所有价格运算必须走这里，禁止 float64**
- `internal/api` — HTTP handler，只依赖 `Provider` 接口

## 规则
- **契约先行**：改接口先改 `api/openapi.yaml` 并通过 `make lint-api`；破坏性变更需新增 `/v2` 路径。
- 标的格式 `700.HK`（`^[0-9A-Z]{1,6}\.(HK|US|SH|SZ)$`），港股币种 HKD。
- 外部 SDK 只允许出现在 `internal/provider/<name>.go`；通过小接口（如 `quoteAPI`）注入，单测用 fake，**测试禁止访问真实网络**。
- 错误响应统一 `{"code","message"}`，不向客户端泄露上游原始错误。
- 凭证只从环境变量读取，禁止提交 `.env`。
- 新 provider 或指标：先写 table-driven 测试，再实现。

## 命令
- `make test` / `make lint` / `make dev` / `make dev-live`
