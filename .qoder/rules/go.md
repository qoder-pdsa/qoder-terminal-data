---
trigger: glob
paths:
  - "**/*.go"
---
- 错误用 `fmt.Errorf("...: %w", err)` 包装；handler 层统一转换为 HTTP 错误。
- `context.Context` 作为第一个参数传递到 provider。
- 测试与实现同目录，table-driven，HTTP 用 `httptest`。
