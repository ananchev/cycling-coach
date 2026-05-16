# MCP Server Smoke Test

A diagnostic binary that validates the full MCP server trust chain before going
live with a client (claude.ai, Claude Desktop, Claude Code). Run it after
deploying a new version of the cycling-coach app or after changing the MCP
server configuration.

## What it tests

The smoke test runs three sections in sequence. Each check prints `✓` or `✗`
with HTTP status and timing.

### Section 1 — App connectivity

Creates an `AppClient` (with mTLS if cert/key are configured) and calls every
`/api/mcp/v1/*` endpoint on the real cycling-coach app. For list endpoints it
automatically chains into a detail call using the first returned ID, so the
full read path is exercised end-to-end.

| Check | Notes |
|-------|-------|
| `GET /api/mcp/v1/profile` | Reads athlete profile markdown |
| `GET /api/mcp/v1/zone-config` | FTP / HR max / zone bounds |
| `GET /api/mcp/v1/block-context?block=current` | Composed coaching context |
| `GET /api/mcp/v1/progress` | KPI snapshot |
| `GET /api/mcp/v1/workouts?limit=5` | Workout list |
| `GET /api/mcp/v1/workouts/{id}` | First workout from list |
| `GET /api/mcp/v1/notes?limit=5` | Notes list |
| `GET /api/mcp/v1/body-metrics?limit=5` | Body metrics |
| `GET /api/mcp/v1/reports?limit=5` | Report list |
| `GET /api/mcp/v1/reports/{id}` | First report from list |

### Section 2 — OAuth in-process

Creates an `OAuthServer` in-process (same config as the running MCP server) and
validates the token lifecycle without any HTTP overhead.

| Check | Notes |
|-------|-------|
| ROPC token issuance | `issueJWT` with correct credentials |
| Valid token passes `RequireBearer` | Inner handler reached |
| Missing token rejected | `401` returned |
| Expired token rejected | `401` returned |

Skipped when `MCP_OAUTH_SIGNING_KEY` is not set (local dev mode).

### Section 3 — MCP server end-to-end (opt-in)

Calls the **running** MCP server process over HTTP. Enabled by setting
`MCP_SERVER_URL`. The ROPC token is issued in-process (same signing key) and
replayed against the live server, so this validates that auth middleware and
routing are wired correctly.

| Check | Notes |
|-------|-------|
| OAuth metadata endpoint | `/.well-known/oauth-authorization-server` returns JSON |
| ROPC token from running server | `POST /oauth/token` returns JWT |
| No token → 401 | `POST /mcp` without Bearer is rejected |
| Valid token → not 401 | `POST /mcp` with Bearer reaches the MCP handler |

---

## Configuration

All settings are read from environment variables — the same set used by the
MCP server itself.

### Required

| Variable | Example | Description |
|----------|---------|-------------|
| `MCP_APP_BASE_URL` | `https://cycling-coach.example.com` | Base URL of the cycling-coach app |

### mTLS (production; omit for local dev)

| Variable | Example | Description |
|----------|---------|-------------|
| `MCP_APP_CLIENT_CERT` | `testdata/client.crt` | Path to the mTLS client certificate PEM |
| `MCP_APP_CLIENT_KEY` | `testdata/client.key` | Path to the mTLS client private key PEM |

Place the certificate files in `mcp-server/testdata/` — that directory is
`.gitignore`d for `*.crt`, `*.key`, and `*.pem` so they will not be committed.

### OAuth

| Variable | Example | Description |
|----------|---------|-------------|
| `MCP_OAUTH_USER` | `athlete` | OAuth username (must match server config) |
| `MCP_OAUTH_PASSWORD` | `…` | OAuth password |
| `MCP_OAUTH_SIGNING_KEY` | `…` | JWT signing key — any random 32+ byte string |

### MCP server end-to-end (opt-in)

| Variable | Example | Description |
|----------|---------|-------------|
| `MCP_SERVER_URL` | `http://localhost:8091` | URL of a running MCP server process. When set, enables Section 3. |

---

## Usage

```sh
# ── Local dev (no mTLS, no OAuth) ────────────────────────────────────────────
MCP_APP_BASE_URL=http://localhost:8080 \
  go run ./cmd/smoketest/

# ── Against the real app with mTLS only ──────────────────────────────────────
MCP_APP_BASE_URL=https://cycling-coach.example.com \
MCP_APP_CLIENT_CERT=testdata/client.crt \
MCP_APP_CLIENT_KEY=testdata/client.key \
  go run ./cmd/smoketest/

# ── Full trust chain: mTLS + OAuth + running MCP server ──────────────────────
MCP_APP_BASE_URL=https://cycling-coach.example.com \
MCP_APP_CLIENT_CERT=testdata/client.crt \
MCP_APP_CLIENT_KEY=testdata/client.key \
MCP_OAUTH_USER=athlete \
MCP_OAUTH_PASSWORD=your-password \
MCP_OAUTH_SIGNING_KEY=your-signing-key \
MCP_SERVER_URL=http://localhost:8091 \
  go run ./cmd/smoketest/
```

Or build the binary first:

```sh
go build -o /tmp/smoketest ./cmd/smoketest/
MCP_APP_BASE_URL=… /tmp/smoketest
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | All executed checks passed |
| `1` | One or more checks failed, or a configuration error occurred |

## Example output

```
────────────────────────────────────────────────────────────
  cycling-coach MCP — smoke test
────────────────────────────────────────────────────────────

  [APP CONNECTIVITY]  https://cycling-coach.example.com

  ✓  GET /api/mcp/v1/profile                           200  142ms  {"markdown":"# Athlete Pr…
  ✓  GET /api/mcp/v1/zone-config                       200   88ms  {"ftp_w":270,"hr_max_bpm…
  ✓  GET /api/mcp/v1/block-context?block=current       200  310ms  {"markdown":"## Training …
  ✓  GET /api/mcp/v1/progress                          200  115ms  {"selected_range":{"from"…
  ✓  GET /api/mcp/v1/workouts?limit=5                  200   67ms  5 items
  ✓  GET /api/mcp/v1/workouts/42                       200   91ms  {"id":42,"wahoo_id":"…
  ✓  GET /api/mcp/v1/notes?limit=5                     200   44ms  5 items
  ✓  GET /api/mcp/v1/body-metrics?limit=5              200   38ms  5 items
  ✓  GET /api/mcp/v1/reports?limit=5                   200   29ms  5 items
  ✓  GET /api/mcp/v1/reports/11                        200   61ms  {"id":11,"type":"weekly_r…

  [OAUTH — in-process]

  ✓  POST /oauth/token (ROPC)                          200    0ms  JWT issued
  ✓  RequireBearer — valid token passes                200    0ms
  ✓  RequireBearer — missing token → 401               401    0ms
  ✓  RequireBearer — expired token → 401               401    0ms

  [MCP SERVER]  http://localhost:8091

  ✓  GET /.well-known/oauth-authorization-server       200    5ms
  ✓  POST /oauth/token (ROPC → running server)         200    3ms  JWT issued
  ✓  POST /mcp — no token → 401                        401    2ms
  ✓  POST /mcp — valid token → not 401                 200    8ms

────────────────────────────────────────────────────────────
  18/18 checks passed
────────────────────────────────────────────────────────────
```

## Placing the client certificate

The `testdata/` directory is the conventional location for integration test
fixtures in this module. Put certificate files there:

```
mcp-server/
  testdata/
    client.crt    ← mTLS client certificate  (git-ignored)
    client.key    ← mTLS client private key   (git-ignored)
    .gitignore
```

The `TestModuleBoundary_NoCyclingCoachImport` test in `internal/modulecheck_test.go`
skips the `testdata/` directory, so no files placed there will interfere with
module boundary checks.
