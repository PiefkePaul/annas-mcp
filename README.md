# Anna's Archive MCP Docker Fork

This fork of [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp) adapts the project for containerized, internet-reachable deployment and makes the remote MCP endpoint easier to use from ChatGPT.

It keeps the original CLI and stdio MCP mode, adds a native Streamable HTTP MCP endpoint, and exposes ChatGPT-friendly MCP metadata such as tool titles, read-only hints, and server instructions.

> [!NOTE]
> This project is a technical wrapper around Anna's Archive search and download flows. Make sure your use complies with applicable law, licensing terms, and the rights of authors and publishers.

## What This Fork Adds

- Native MCP over Streamable HTTP via `annas-mcp http`
- ChatGPT-friendlier MCP metadata for the exposed tools
- Search-only operation without forcing `ANNAS_SECRET_KEY`
- Conditional download tools that only appear when the required download environment is configured
- `/healthz` endpoint for Docker and reverse-proxy health checks
- Multi-stage `Dockerfile` for container builds
- `compose.yaml` and `.env.example` for local or hosted deployment
- A fork-aligned Go module path (`github.com/PiefkePaul/annas-mcp`)

## Supported Modes

| Mode | Command | Use case |
| --- | --- | --- |
| CLI | `annas-mcp book-search ...` | Manual search and download from the terminal |
| MCP over stdio | `annas-mcp mcp` | Local desktop MCP clients |
| MCP over Streamable HTTP | `annas-mcp http` | Docker, servers, reverse proxies, ChatGPT remote MCP |

## Environment Variables

### Search and Download Variables

| Variable | Required | Description |
| --- | --- | --- |
| `ANNAS_SECRET_KEY` | Only for fast book downloads | Anna's Archive fast-download key. This is kept as a backend credential and is not used as ChatGPT authentication. |
| `ANNAS_DOWNLOAD_PATH` | Only for download tools | Absolute path where downloaded files are stored. |
| `ANNAS_BASE_URL` | No | Anna's Archive mirror hostname. Defaults to `annas-archive.li`. |

### HTTP Variables

| Variable | Required | Description |
| --- | --- | --- |
| `ANNAS_HTTP_ADDR` | No | Bind address for the HTTP server. Defaults to `:8080`. |
| `ANNAS_HTTP_PATH` | No | MCP endpoint path. Defaults to `/mcp`. |
| `ANNAS_HTTP_AUTH_MODE` | No | `none` or `bearer`. Use `none` for direct ChatGPT MCP connections. Defaults to `none`. |
| `ANNAS_HTTP_BEARER_TOKEN` | Only if `ANNAS_HTTP_AUTH_MODE=bearer` | Bearer token for non-ChatGPT clients. |
| `ANNAS_PUBLIC_BASE_URL` | Recommended for public deployments | Public base URL used to advertise the final connector URL in the root endpoint. |
| `ANNAS_HTTP_PORT` | Compose only | Host port published by `compose.yaml`. Defaults to `8080`. |

## ChatGPT / Apps SDK Notes

For direct ChatGPT MCP use, the current best-fit setup is:

1. Expose the server on a public HTTPS URL.
2. Set `ANNAS_HTTP_AUTH_MODE=none`.
3. Optionally set `ANNAS_PUBLIC_BASE_URL=https://your-domain.example` so `GET /` shows the final connector URL.
4. Add the remote MCP server in ChatGPT developer settings using the public MCP endpoint, usually `https://your-domain.example/mcp`.

A few important details:

- `ANNAS_SECRET_KEY` is **not** used as ChatGPT connector auth. It stays an Anna's Archive backend secret for fast downloads.
- The built-in `bearer` mode is useful for your own MCP clients, but not the right fit for direct ChatGPT MCP connectors.
- If you need authenticated ChatGPT access later, put an OAuth-capable gateway or proxy in front of this server instead of reusing `ANNAS_SECRET_KEY` as a client token.
- Search tools are always exposed. Download tools are only exposed when the required download environment is configured.

## Docker Quick Start

1. Create a local `.env` file from `.env.example`.
2. Set `ANNAS_HTTP_AUTH_MODE=none` if you want to connect the server directly from ChatGPT.
3. Set your real `ANNAS_SECRET_KEY` only if you want fast book downloads.
4. Start the container:

```bash
docker compose up -d --build
```

5. Check that the container is healthy:

```bash
curl http://localhost:8080/healthz
```

6. Inspect the advertised deployment metadata:

```bash
curl http://localhost:8080/
```

7. Use the MCP endpoint at:

```text
http://localhost:8080/mcp
```

Downloaded files are written to the local `downloads/` directory on the host through the bind mount defined in `compose.yaml`.

## Running Without Docker

### Local stdio MCP server

```bash
annas-mcp mcp
```

### Remote HTTP MCP server

```bash
ANNAS_DOWNLOAD_PATH=/absolute/path \
ANNAS_HTTP_AUTH_MODE=none \
annas-mcp http
```

Add `ANNAS_SECRET_KEY=your-key` if you also want fast book downloads.

## Making It Reachable From the Internet

Docker alone only makes the service reachable on the Docker host. To make it available from the public internet, you still need an ingress layer such as:

- a reverse proxy like Caddy, Nginx, or Traefik
- a cloud load balancer or tunnel
- TLS/HTTPS on the public edge
- optionally an OAuth-capable front door if you want authenticated ChatGPT access later

A minimal public deployment pattern is:

1. Run this container on a VPS or home server.
2. Bind it only to an internal port.
3. Put HTTPS in front of it with a reverse proxy.
4. Forward the proxy to the app's MCP endpoint, usually `/mcp`.
5. For direct ChatGPT use, keep the upstream app in `ANNAS_HTTP_AUTH_MODE=none`.
6. For other MCP clients, you may enable `ANNAS_HTTP_AUTH_MODE=bearer` instead.

## Development Notes

- The upstream project uses Go and the official [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk).
- This fork keeps the original search and download behavior, while extending transport, deployment, and remote MCP metadata.
- The default container command is `annas-mcp http`.

## Upstream

- Upstream repository: [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp)
- Fork repository: [PiefkePaul/annas-mcp](https://github.com/PiefkePaul/annas-mcp)
