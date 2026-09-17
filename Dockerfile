FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /qoder-terminal-data ./cmd/server

FROM gcr.io/distroless/static
COPY --from=build /qoder-terminal-data /qoder-terminal-data
EXPOSE 8081
ENTRYPOINT ["/qoder-terminal-data"]
