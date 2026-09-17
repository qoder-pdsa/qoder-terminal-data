---
trigger: always_on
---
# AutoWonder 交付约定

> 由 `qoder-terminal-web/scripts/sync-rules.sh` 下发，**不要在各 repo 内直接修改**。

本项目由三个 repo 组成，在 AutoWonder 中由数字人流水线交付：**全栈开发 → 代码评审 → 测试与部署 → 人工验收**。

| Repo | 语言 | 维护的契约 |
|---|---|---|
| `qoder-terminal-data` | Go | `api/openapi.yaml` |
| `qoder-terminal-analyst` | Python | `api/openapi.yaml`、`api/agent-event.schema.json` |
| `qoder-terminal-web` | TypeScript | 消费方；包含 `e2e/` |

## 统一入口
| 命令 | 用途 | 主要使用者 |
|---|---|---|
| `make test` | 单元测试 | DEV（改动前跑基线、改动后回归） |
| `make lint` | 格式、静态检查、类型检查、OpenAPI 校验 | DEV、CR 核对证据 |
| `GET /health` | 部署后健康检查（data :8081、analyst :8082） | QA |
| web `make e2e` | 部署后跨 repo 验证 | QA |

## 开发（DEV）
1. 开工前运行 `make test` 记录**旧行为基线**；基线失败先报告，不得先改代码再补基线。
2. TDD：先写失败测试，再实现，再重构。
3. **契约先行**：跨 repo 需求先合入提供方 repo 的契约，消费方不得自行发明字段。
4. 提交前 `make lint test` 全部通过；零用例、跳过、未执行不算通过。

## 评审（CR）
- 只读评审，不运行构建、不改代码；缺陷 REJECT 回 DEV，附文件与行号。
- 重点：契约一致性、价格不使用浮点运算、错误处理、凭证不入库、测试不访问真实网络。

## 证据
- 各步骤的真实命令输出与结论写入 `evidence/`（由 AutoWonder Runtime 封存上传）。
- 禁止编造证据；mock 数据的测试结果不能冒充部署后测试。

## 不适用的 SDLC 步骤
- **数据库变更预检 / 执行**：本系统无数据库，按“不适用”处理并在证据中说明依据。
