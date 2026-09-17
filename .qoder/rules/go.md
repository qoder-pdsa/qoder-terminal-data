---
trigger: glob
paths:
  - "**/*.go"
---
- Wrap errors with `fmt.Errorf("...: %w", err)`; the handler layer converts them into HTTP errors.
- Pass `context.Context` as the first argument down to providers.
- Keep tests next to the code, table-driven, using `httptest` for HTTP.
