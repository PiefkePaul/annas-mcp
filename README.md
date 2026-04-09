# Anna's Archive MCP Docker Fork

This fork of [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp) adapts the project for containerized, internet-reachable deployment and makes the remote MCP endpoint easier to use from ChatGPT.

It keeps the original CLI and stdio MCP mode, adds a native Streamable HTTP MCP endpoint, and exposes ChatGPT-friendly MCP metadata such as tool titles, read-only hints, and server instructions.

> [!NOTE]
> This project is a technical wrapper around Anna's Archive search and download flows. Make sure your use complies with applicable law, licensing terms, and the rights of authors and publishers.

## What This Fork Adds

- Native MCP over Streamable HTTP via `annas-mcp http`
- ChatGPT-friendlier MCP metadata for the exposed tools
- Search-only operation without forcing `ANNAS_SECRET_KEY`
- Per-call download secrets for remote MCP clients instead of requiring a single server-global download key
- Built-in OAuth account portal for per-user secret registration and secure remote MCP sign-in
- Automatic fallback across the current official Anna's Archive mirrors
- Embedded MCP file responses for downloads that fit within the configured inline size limit
- Temporary direct download links for MCP clients that do not render embedded file attachments
- Article download fallback to Libgen PDF mirrors before falling back to SciDB HTML pages
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
| `ANNAS_SECRET_KEY` | No | Optional default Anna's Archive fast-download key used only when the client does not pass `secret_key` to the download tool. This is not ChatGPT authentication. |
| `ANNAS_DOWNLOAD_PATH` | Only for local CLI or optional server-side saves | Absolute path where files are written when you use the CLI download commands. Remote MCP downloads do not require it. |
| `ANNAS_BASE_URL` | No | Optional legacy single-mirror override. |
| `ANNAS_BASE_URLS` | No | Optional comma-separated mirror list. Defaults to the currently listed official mirrors: `annas-archive.gl`, `annas-archive.pk`, `annas-archive.gd`. |
| `ANNAS_MAX_INLINE_DOWNLOAD_MB` | No | Maximum file size returned inline through MCP as an embedded resource. Defaults to `20`. |

### HTTP Variables

| Variable | Required | Description |
| --- | --- | --- |
| `ANNAS_HTTP_ADDR` | No | Bind address for the HTTP server. Defaults to `:8080`. |
| `ANNAS_HTTP_PATH` | No | MCP endpoint path. Defaults to `/mcp`. |
| `ANNAS_HTTP_AUTH_MODE` | No | `none`, `oauth`, or `bearer`. Use `oauth` for per-user sign-in with stored secrets. Defaults to `none`. |
| `ANNAS_HTTP_BEARER_TOKEN` | Only if `ANNAS_HTTP_AUTH_MODE=bearer` | Bearer token for non-ChatGPT clients. |
| `ANNAS_PUBLIC_BASE_URL` | Recommended for public deployments | Public base URL used to advertise the final connector URL in the root endpoint. |
| `ANNAS_HTTP_PORT` | Compose only | Host port published by `compose.yaml`. Defaults to `8080`. |

### OAuth / Account Variables

| Variable | Required | Description |
| --- | --- | --- |
| `ANNAS_AUTH_MASTER_KEY` | Yes when `ANNAS_HTTP_AUTH_MODE=oauth` | Master encryption key for the on-disk auth database. Must decode to exactly 32 bytes using base64, base64url, or hex. |
| `ANNAS_AUTH_STORE_PATH` | No | Path to the encrypted auth database that stores users, registered OAuth clients, sessions, and tokens. Defaults to `/data/auth-store.enc` in `compose.yaml`. |
| `ANNAS_AUTH_ACCESS_TOKEN_TTL` | No | OAuth access token lifetime. Defaults to `1h`. |
| `ANNAS_AUTH_REFRESH_TOKEN_TTL` | No | OAuth refresh token lifetime. Defaults to `720h`. |
| `ANNAS_AUTH_CODE_TTL` | No | OAuth authorization code lifetime. Defaults to `10m`. |
| `ANNAS_AUTH_SESSION_TTL` | No | Account portal session lifetime. Defaults to `720h`. |

All stored OAuth data is encrypted at rest with the `ANNAS_AUTH_MASTER_KEY`, and account passwords are stored as bcrypt hashes instead of plaintext.

## ChatGPT / Apps SDK Notes

For direct ChatGPT MCP use, the current best-fit setup is:

1. Expose the server on a public HTTPS URL.
2. Set `ANNAS_HTTP_AUTH_MODE=none`.
3. Optionally set `ANNAS_PUBLIC_BASE_URL=https://your-domain.example` so `GET /` shows the final connector URL.
4. Add the remote MCP server in ChatGPT developer settings using the public MCP endpoint, usually `https://your-domain.example/mcp`.

A few important details:

- `ANNAS_SECRET_KEY` is **not** used as ChatGPT connector auth. It stays an Anna's Archive backend secret for fast downloads.
- `oauth` mode exposes a built-in account portal at `/account`, an OAuth authorization server, and encrypted storage for per-user Anna's Archive secrets.
- The built-in `bearer` mode is useful for simple shared-token clients, but not the right fit for per-user ChatGPT or Claude access.
- Search and download tools are always exposed.
- `book_download` accepts `secret_key` per tool call, but with OAuth users usually do not need to pass it manually because the server resolves it from the signed-in account.
- `article_download` uses Anna's fast-download path first when a secret is available, then tries Libgen PDF mirrors, and only falls back to SciDB when needed.
- Downloads are returned as embedded MCP resources when they fit within `ANNAS_MAX_INLINE_DOWNLOAD_MB`, and the server also includes a temporary direct download URL for clients that do not render the attachment visibly.

## Docker Quick Start

1. Create a local `.env` file from `.env.example`.
2. Set `ANNAS_HTTP_AUTH_MODE=oauth` if you want per-user sign-in and stored secrets, or leave it at `none` for public unauthenticated use.
3. If you use OAuth mode, set `ANNAS_AUTH_MASTER_KEY` and keep the `/data` volume persistent.
4. Optionally set `ANNAS_SECRET_KEY` only if you want a server-side default fast-download key. Remote clients can also pass `secret_key` per download call.
5. Start the container:

```bash
docker compose up -d --build
```

6. Check that the container is healthy:

```bash
curl http://localhost:8080/healthz
```

7. Inspect the advertised deployment metadata:

```bash
curl http://localhost:8080/
```

8. Use the MCP endpoint at:

```text
http://localhost:8080/mcp
```

Downloads are returned inline as embedded MCP resources when they fit within the configured size limit. The server also includes a temporary direct download URL as a fallback, so a host bind mount is no longer required for the normal remote MCP flow.

When `ANNAS_HTTP_AUTH_MODE=oauth`, users first create an account at `/account/register`, save their Anna's Archive secret there, and then complete OAuth from the MCP client.

## Automatic Docker Hub Publishing

This repository now includes [`.github/workflows/docker-publish.yml`](.github/workflows/docker-publish.yml).
It builds the container image from the existing `Dockerfile` and pushes it to Docker Hub:

- on every push to `main`
- on version tags matching `v*`
- on manual runs through `workflow_dispatch`

The workflow publishes:

- `latest` for the default branch
- `sha-<commit>` for each pushed commit
- semantic version tags such as `v1.2.3`, `1.2`, and `1` when you push a matching Git tag

To enable it in GitHub, add these repository settings under `Settings -> Secrets and variables -> Actions`:

- Repository variable `DOCKER_USERNAME`: your Docker Hub login name
- Repository secret `DOCKER_PASSWORD`: a Docker Hub personal access token with read/write access

Optional repository variables:

- `DOCKERHUB_NAMESPACE`: target namespace if you want to push somewhere other than your login namespace
- `DOCKERHUB_REPOSITORY`: target repository name if it should differ from the GitHub repository name

If the optional variables are unset, the workflow pushes to `<DOCKER_USERNAME>/<github-repo-name>`.
If `DOCKER_PASSWORD` is not set yet, the workflow exits cleanly with a short summary instead of failing the entire Actions run.

## Running Without Docker

### Local stdio MCP server

```bash
annas-mcp mcp
```

### Remote HTTP MCP server

```bash
ANNAS_HTTP_AUTH_MODE=none \
annas-mcp http
```

Optional additions:

- `ANNAS_SECRET_KEY=your-key` if you want a server-default fast-download key
- `ANNAS_BASE_URLS=annas-archive.gl,annas-archive.pk,annas-archive.gd` to override the mirror list explicitly
- `ANNAS_MAX_INLINE_DOWNLOAD_MB=20` to tune the maximum embedded file size
- `ANNAS_HTTP_AUTH_MODE=oauth` plus `ANNAS_AUTH_MASTER_KEY=...` if you want per-user OAuth sign-in

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
