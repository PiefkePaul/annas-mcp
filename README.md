# Anna's Archive MCP Server

This fork of [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp) is structured as a normal hostable MCP server first, with Docker images published as distribution artifacts on top.

It keeps the original CLI and stdio MCP mode, adds a native Streamable HTTP MCP endpoint, and exposes ChatGPT-friendly MCP metadata such as tool titles, read-only hints, and server instructions.

> [!NOTE]
> This project is a technical wrapper around Anna's Archive search and download flows. Make sure your use complies with applicable law, licensing terms, and the rights of authors and publishers.

## Distribution Model

This repository now cleanly separates source distribution from container distribution:

- GitHub is the source of truth for the MCP server code.
- GitHub Releases publish hostable binaries for supported platforms.
- Docker Hub publishes ready-to-run container images derived from the same source.
- Pushes to `main` automatically rebuild and publish the Docker images after verification.

The Docker output is intentionally split into two runtime variants:

- `latest`: hosted HTTP MCP server for VPS, Docker Compose, reverse proxies, and remote clients.
- `mcp`: local stdio-oriented MCP image for Docker-based MCP clients and Docker MCP Toolkit style usage.

## Supported Modes

| Mode | Command | Use case |
| --- | --- | --- |
| CLI | `annas-mcp book-search ...` | Manual search and download from the terminal |
| MCP over stdio | `annas-mcp mcp` | Local desktop MCP clients |
| MCP over Streamable HTTP | `annas-mcp http` | Docker, servers, reverse proxies, ChatGPT remote MCP |

## Install From GitHub

### Option 1: Install with Go

```bash
go install github.com/PiefkePaul/annas-mcp/cmd/annas-mcp@latest
```

### Option 2: Download a release binary

Create a version tag like `v0.1.0` and GitHub Actions will publish platform binaries through GoReleaser.

## Run Without Docker

### Local stdio MCP server

```bash
annas-mcp mcp
```

### Hosted HTTP MCP server

```bash
ANNAS_HTTP_AUTH_MODE=none \
annas-mcp http
```

Optional additions:

- `ANNAS_SECRET_KEY=your-key` if you want a server-default fast-download key
- `ANNAS_BASE_URLS=annas-archive.gl,annas-archive.pk,annas-archive.gd` to override the mirror list explicitly
- `ANNAS_MAX_INLINE_DOWNLOAD_MB=20` to tune the maximum embedded file size
- `ANNAS_HTTP_AUTH_MODE=oauth` plus `ANNAS_AUTH_MASTER_KEY=...` if you want per-user OAuth sign-in

## Run From Docker Hub

Replace `<your-namespace>` with the Docker Hub namespace you configured in GitHub Actions. If you keep the defaults, it becomes your Docker Hub username.

### Hosted HTTP image

```bash
docker run --rm -p 8080:8080 \
  -e ANNAS_HTTP_AUTH_MODE=none \
  docker.io/<your-namespace>/annas-mcp:latest
```

The HTTP image is the production-style runtime. It exposes `/mcp`, `/healthz`, and, when enabled, the OAuth/account routes.

### Local MCP image

```bash
docker run --rm -i \
  -e ANNAS_SECRET_KEY=your-secret \
  docker.io/<your-namespace>/annas-mcp:mcp
```

The `:mcp` image starts `annas-mcp mcp` directly:

- no HTTP listener
- no account portal
- no login web UI
- secret handling through `ANNAS_SECRET_KEY` or the `secret_key` tool argument

That makes it the better Docker artifact for local MCP clients that launch servers over `stdio`.

## Docker Compose Quick Start

1. Create a local `.env` file from `.env.example`.
2. Set `ANNAS_HTTP_AUTH_MODE=oauth` if you want per-user sign-in and stored secrets, or leave it at `none` for public unauthenticated use.
3. If you use OAuth mode, set `ANNAS_AUTH_MASTER_KEY` and keep the `/data` volume persistent.
4. Optionally set `ANNAS_SECRET_KEY` only if you want a server-side default fast-download key. Remote clients can also pass `secret_key` per download call.
5. Start the hosted HTTP container from source:

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
| `ANNAS_PUBLIC_BASE_URL` | Recommended for public deployments, effectively required for stable OAuth behind reverse proxies | Public canonical base URL used for connector metadata, OAuth discovery, redirects, and temporary download links. Use the origin only, without a path suffix. |
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

Important details:

- `ANNAS_SECRET_KEY` is not used as ChatGPT connector auth. It stays an Anna's Archive backend secret for fast downloads.
- `oauth` mode exposes a built-in account portal at `/account`, an OAuth authorization server, and encrypted storage for per-user Anna's Archive secrets.
- when `ANNAS_HTTP_AUTH_MODE=oauth`, set `ANNAS_PUBLIC_BASE_URL` to the exact public origin you will use in ChatGPT or Claude
- `book_download` accepts `secret_key` per tool call, but with OAuth users usually do not need to pass it manually because the server resolves it from the signed-in account
- remote HTTP clients receive a text result with a temporary direct download URL for maximum ChatGPT and Claude compatibility
- local stdio clients can additionally receive embedded file attachments when supported

## CI, Releases, and Docker Automation

The repository now has three distinct automation paths:

- `.github/workflows/ci.yml`: pull request validation for Go tests, binary build, and both Docker runtime variants
- `.github/workflows/release.yml`: GitHub release artifacts on version tags matching `v*`
- `.github/workflows/docker-publish.yml`: verified Docker Hub publishing on `main`, on version tags, and on manual runs

The Docker workflow publishes:

- hosted HTTP tags: `latest`, `sha-<commit>`, and semantic version tags such as `1.2.3`, `1.2`, and `1`
- local MCP tags: `mcp`, `mcp-sha-<commit>`, and semantic version tags such as `mcp-1.2.3`, `mcp-1.2`, and `mcp-1`

To enable Docker publishing in GitHub, add these repository settings under `Settings -> Secrets and variables -> Actions`:

- Repository variable `DOCKER_USERNAME`: your Docker Hub login name
- Repository secret `DOCKER_PASSWORD`: a Docker Hub personal access token with read/write access

Optional repository variables:

- `DOCKERHUB_NAMESPACE`: target namespace if you want to push somewhere other than your login namespace
- `DOCKERHUB_REPOSITORY`: target repository name if it should differ from the GitHub repository name

If the optional variables are unset, the workflow pushes to `<DOCKER_USERNAME>/<github-repo-name>`.
If `DOCKER_PASSWORD` is not set yet, the workflow skips the publish steps cleanly instead of failing the entire workflow.

## Docker MCP Toolkit Notes

Docker documents that MCP profiles can use both remote servers and containerized servers, and custom catalogs can include private images through `docker://...` references. That means the `:mcp` image in this repository is suitable for Docker-based local MCP setups and custom Docker MCP catalogs.

The official Docker MCP Catalog is a separate review and publishing channel. Pushing this image to Docker Hub does not by itself publish it into Docker's official catalog.

## Making The HTTP Server Reachable From The Internet

Docker alone only makes the service reachable on the Docker host. To make it available from the public internet, you still need an ingress layer such as:

- a reverse proxy like Caddy, Nginx, or Traefik
- a cloud load balancer or tunnel
- TLS or HTTPS on the public edge
- optionally an OAuth-capable front door if you want authenticated ChatGPT access later

A minimal public deployment pattern is:

1. Run the `latest` image on a VPS or home server.
2. Bind it only to an internal port.
3. Put HTTPS in front of it with a reverse proxy.
4. Forward the proxy to the app's MCP endpoint, usually `/mcp`.
5. For direct ChatGPT use, keep the upstream app in `ANNAS_HTTP_AUTH_MODE=none`.
6. For other MCP clients, you may enable `ANNAS_HTTP_AUTH_MODE=bearer` instead.

## Development Notes

- The upstream project uses Go and the official [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk).
- This fork keeps the original search and download behavior, while extending transport, deployment, and remote MCP metadata.
- The default source and container runtime for hosted deployments is `annas-mcp http`.
- The dedicated Docker MCP image starts `annas-mcp mcp`.

## Upstream

- Upstream repository: [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp)
- Fork repository: [PiefkePaul/annas-mcp](https://github.com/PiefkePaul/annas-mcp)
