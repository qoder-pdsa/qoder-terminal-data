.PHONY: dev dev-live test lint lint-api build
dev:
	DATA_PROVIDER=mock go run ./cmd/server
# Live Hong Kong market data: requires LONGBRIDGE_APP_KEY / APP_SECRET / ACCESS_TOKEN; set HTTPS_PROXY if this machine uses a proxy
dev-live:
	DATA_PROVIDER=longbridge go run ./cmd/server
test:
	go test ./...
lint: lint-api
	go vet ./...
	test -z "$$(gofmt -l .)"
lint-api:
	npx --yes @redocly/cli@1 lint --config api/redocly.yaml api/openapi.yaml
build:
	go build -o bin/qoder-terminal-data ./cmd/server
