# Basis-Images: bei Updates hier anheben. Go-Toolchain neuer als go.mod ist gewollt
# (Sicherheitsfixes der Standardbibliothek), go.mod bleibt kompatibel.
FROM golang:1.27-alpine3.24 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/annas-mcp ./cmd/annas-mcp

FROM alpine:3.24 AS runtime-base
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /out/annas-mcp /usr/local/bin/annas-mcp

ENTRYPOINT ["annas-mcp"]

FROM runtime-base AS mcp-stdio-runtime
CMD ["mcp"]

FROM runtime-base AS http-runtime
RUN apk add --no-cache wget

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD wget -qO- http://127.0.0.1:8080/healthz > /dev/null || exit 1

CMD ["http"]
