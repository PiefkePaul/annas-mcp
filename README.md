# Anna's Archive MCP Docker Fork

This fork of [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp) adapts the project for containerized and internet-reachable deployment.

It keeps the original CLI and stdio MCP mode, and adds a native Streamable HTTP MCP endpoint that can run behind Docker, a reverse proxy, or a public load balancer.

> [!NOTE]
> This project is a technical wrapper around Anna's Archive search and download flows. Make sure your use complies with applicable law, licensing terms, and the rights of authors and publishers.

## What This Fork Adds

- Native MCP over Streamable HTTP via `annas-mcp http`
- Optional Bearer token protection for the remote MCP endpoint
- `/healthz` endpoint for Docker and reverse-proxy health checks
- Multi-stage `Dockerfile` for container builds
- `compose.yaml` and `.env.example` for local or hosted deployment
- A fork-aligned Go module path (`github.com/PiefkePaul/annas-mcp`)

## Supported Modes

| Mode | Command | Use case |
| --- | --- | --- |
| CLI | `annas-mcp book-search ...` | Manual search and download from the terminal |
| MCP over stdio | `annas-mcp mcp` | Local desktop MCP clients |
| MCP over Streamable HTTP | `annas-mcp http` | Docker, servers, reverse proxies, internet-facing deployment |

## Environment Variables

### Core Variables

| Variable | Required | Description |
| --- | --- | --- |
| `ANNAS_SECRET_KEY` | Yes | Anna's Archive API key |
| `ANNAS_DOWNLOAD_PATH` | Yes | Absolute path where downloads are stored |
| `ANNAS_BASE_URL` | No | Anna's Archive mirror hostname. Defaults to `annas-archive.li` |

### HTTP Variables

| Variable | Required | Description |
| --- | --- | --- |
| `ANNAS_HTTP_ADDR` | No | Bind address for the HTTP server. Defaults to `:8080` |
| `ANNAS_HTTP_PATH` | No | MCP endpoint path. Defaults to `/mcp` |
| `ANNAS_HTTP_BEARER_TOKEN` | Strongly recommended for public exposure | Bearer token required on the MCP endpoint |
| `ANNAS_HTTP_PORT` | Compose only | Host port published by `compose.yaml`. Defaults to `8080` |

## Docker Quick Start

1. Create a local `.env` file from `.env.example` and set your real `ANNAS_SECRET_KEY`.
2. Start the container:

```bash
docker compose up -d --build
```

3. Check that the container is healthy:

```bash
curl http://localhost:8080/healthz
```

4. Use the MCP endpoint at:

```text
http://localhost:8080/mcp
```

If you set `ANNAS_HTTP_BEARER_TOKEN`, clients must send:

```text
Authorization: Bearer <your-token>
```

Downloaded files are written to the local `downloads/` directory on the host through the bind mount defined in `compose.yaml`.

## Running Without Docker

### Local stdio MCP server

```bash
annas-mcp mcp
```

### Remote HTTP MCP server

```bash
ANNAS_SECRET_KEY=your-key \
ANNAS_DOWNLOAD_PATH=/absolute/path \
ANNAS_HTTP_BEARER_TOKEN=change-me \
annas-mcp http
```

## Making It Reachable From the Internet

Docker alone only makes the service reachable on the Docker host. To make it available from the public internet, you still need an ingress layer such as:

- a reverse proxy like Caddy, Nginx, or Traefik
- a cloud load balancer or tunnel
- TLS/HTTPS on the public edge
- a non-empty `ANNAS_HTTP_BEARER_TOKEN`

A minimal public deployment pattern is:

1. Run this container on a VPS or home server.
2. Bind it only to an internal port.
3. Put HTTPS in front of it with a reverse proxy.
4. Forward the proxy to the app's MCP endpoint, usually `/mcp`.
5. Restrict access with a Bearer token before sharing the endpoint with any MCP client.

## Development Notes

- The upstream project uses Go and the official [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk).
- This fork keeps the original tool behavior and adds only transport and deployment changes.
- The default container command is `annas-mcp http`.

## Upstream

- Upstream repository: [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp)
- Fork repository: [PiefkePaul/annas-mcp](https://github.com/PiefkePaul/annas-mcp)


