# CLAUDE.md — Project Instructions for Claude Code

## What this project is

A personal cycling training assistant that:
1. Automatically ingests ride data from a Wahoo KICKR trainer via the Wahoo Cloud API
2. Accepts subjective training notes (RPE, sleep, stress) via a Telegram bot
3. Analyzes every ride: HR zones, power zones, HR drift, NP, TSS, TRIMP
4. Generates weekly reports and training plans using Claude API
5. Delivers compact summaries via Telegram, full reports as web pages

## Two markdown files — different purposes

| File | Purpose | Lives where | Updated how |
|---|---|---|---|
| `CLAUDE.md` (this file) | Dev instructions for Claude Code | Repo root, checked into git | Edit in repo |
| Athlete profile | Training context: zones, FTP, coaching philosophy. Used as Claude API system prompt at runtime. | `/data/athlete-profile.md` (Docker volume) | Via Telegram `/profile` command, web upload, or direct file edit |

The athlete profile is **runtime configuration, not source code**. It is never baked into the Docker image. The app loads it from the data volume when calling the Claude API. A seed file ships at `config/athlete-profile.default.md` and is copied to the data volume on first startup if no profile exists yet.

## Architecture

Read `ARCHITECTURE.md` for the full system design, data flow, API details, and build phases. Follow the phased build order defined there.

## Stack

- **Language:** Go 1.22+, CGO enabled (required for SQLite)
- **HTTP:** `github.com/go-chi/chi/v5`
- **Database:** SQLite via `github.com/mattn/go-sqlite3`
- **Telegram:** `github.com/go-telegram-bot-api/telegram-bot-api/v5`
- **FIT parsing:** `github.com/muktihari/fit`
- **Scheduling:** `github.com/robfig/cron/v3`
- **OAuth2:** `golang.org/x/oauth2`
- **Logging:** `log/slog` (stdlib)
- **Claude API:** raw `net/http` (no SDK — simple JSON POST to `https://api.anthropic.com/v1/messages`)
- **Deployment:** Docker on homelab, behind Cloudflare + Nginx Proxy Manager

## Code conventions

- Standard Go formatting (`gofmt`)
- Errors: always wrap with context → `fmt.Errorf("wahoo.RefreshToken: %w", err)`
- Logging: use `log/slog` with structured fields → `slog.Info("workout processed", "wahoo_id", id, "drift_pct", drift)`
- No global state. Pass dependencies via structs (dependency injection through constructors)
- Use `context.Context` for cancellation and timeouts on all external calls (Wahoo API, Claude API, Telegram)
- Tests: table-driven, `_test.go` files alongside code
- Database: `database/sql` directly, no ORM. Migrations run on startup in `internal/storage/db.go`

## Project layout

```
cmd/server/          → main entry point
cmd/backfill/        → one-shot: fetch all historical workouts from Wahoo
cmd/import-csv/      → one-shot: import Wahoo CSV exports
cmd/reset/           → one-shot: wipe DB, reimport everything
internal/config/     → env var loading
internal/wahoo/      → OAuth2 + API client + webhook
internal/telegram/   → bot + command handlers
internal/fit/        → FIT file parsing
internal/analysis/   → metrics computation + trends
internal/reporting/  → Claude API + HTML rendering + delivery
internal/scheduler/  → cron jobs
internal/storage/    → SQLite (all DB access)
internal/web/        → HTTP router + handlers + middleware
config/              → default/seed config files (athlete-profile.default.md)
templates/           → HTML templates for report pages
static/              → CSS for report pages
```

## Athlete profile as runtime config

The athlete profile contains zones, FTP, HRmax, coaching philosophy, and training history. It is the system prompt sent to the Claude API for report/plan generation.

**Storage:** The canonical copy lives at `$ATHLETE_PROFILE_PATH` (default: `/data/athlete-profile.md`). On first startup, if this file doesn't exist, the app copies `config/athlete-profile.default.md` to the data volume.

**Structured config (DB):** Key numeric values (FTP, HRmax, weight, zone boundaries) are also stored in an `athlete_config` table in SQLite. The analysis engine reads zone boundaries and thresholds from this table — it never parses the markdown file for numbers. When the profile is updated, the app extracts structured values and syncs them to the DB.

**Update paths:**
- Telegram: `/profile` sends current profile as a document; `/profile set` accepts a new markdown file as attachment
- Web: `POST /api/profile` accepts markdown body (protected by Cloudflare Access)
- Direct file edit: edit `/data/athlete-profile.md` on the server, then call `/api/profile/reload`

**Schema:**

```sql
CREATE TABLE athlete_config (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
-- Keys: ftp_watts, hr_max, weight_kg,
--        hr_z1_max, hr_z2_max, hr_z3_max, hr_z4_max,
--        pwr_z1_max, pwr_z2_max, pwr_z3_max, pwr_z4_max
```

The analysis engine reads from `athlete_config`. **Never hardcode zones or FTP.**

Current defaults (for reference during development, not for hardcoding):

**FTP:** 251W · **HRmax:** 184 bpm · **Weight:** ~90-91 kg  
**HR Zones:** Z1 <110, Z2 110-127, Z3 128-145, Z4 146-164, Z5 ≥165  
**Power Zones:** Z1 <138W, Z2 139-188W, Z3 189-226W, Z4 227-263W, Z5 264-301W  
**Primary KPI:** HR drift (decoupling). <5% = excellent, 5-8% = acceptable, >8% = flag.

## External APIs

### Wahoo Cloud API
- Base URL: `https://api.wahooligan.com`
- Auth: OAuth2 (access token in `Authorization: Bearer` header)
- Docs: `https://cloud-api.wahooligan.com/`
- Access tokens expire every 2h; refresh before API calls
- FIT file downloads from CDN don't need auth headers

### Claude API
- Endpoint: `POST https://api.anthropic.com/v1/messages`
- Model: `claude-sonnet-4-20250514`
- System prompt: contents of the athlete profile (loaded from `$ATHLETE_PROFILE_PATH`)
- Used only for weekly report + plan generation

### Telegram Bot API
- Long-polling mode (not webhook)
- Only respond to messages from the configured `TELEGRAM_CHAT_ID`

## Build & run

```bash
make dev          # Run locally (go run)
make build        # Build Docker image
make run          # docker compose up -d
make test         # go test ./...
make lint         # golangci-lint run
```

## Important constraints

- Wahoo sandbox rate limit: 25 requests per 5 minutes. Add sleep/throttling in backfill.
- SQLite: single writer. No concurrent writes needed (single-user system).
- CGO: required for go-sqlite3. Dockerfile uses `gcc musl-dev` in build stage.
- Cloudflare Access protects `/reports/*` and `/plans/*`. Wahoo paths (`/wahoo/*`) must bypass Access.
- Telegram bot runs as a goroutine inside the main server process (not a separate binary).
- All times in CET/Europe/Amsterdam timezone.
- Athlete profile is runtime config, not build-time. Never hardcode zones or FTP.

## Testing approach

- Unit tests for analysis/metrics (known inputs → expected outputs)
- Unit tests for FIT parsing (use `testdata/sample.fit`)
- Integration tests for storage layer (in-memory SQLite)
- Manual testing for Wahoo OAuth flow and Telegram bot
- Test the full pipeline with historical CSV data before enabling live ingestion
