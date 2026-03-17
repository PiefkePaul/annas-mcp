FROM golang:1.23.4-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/annas-mcp ./cmd/annas-mcp

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata wget

WORKDIR /app
COPY --from=builder /out/annas-mcp /usr/local/bin/annas-mcp
RUN mkdir -p /data/downloads

ENV ANNAS_DOWNLOAD_PATH=/data/downloads
ENV ANNAS_HTTP_ADDR=:8080
ENV ANNAS_HTTP_PATH=/mcp

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD wget -qO- http://127.0.0.1:8080/healthz > /dev/null || exit 1

ENTRYPOINT ["annas-mcp"]
CMD ["http"]
