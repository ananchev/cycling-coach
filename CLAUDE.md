# CLAUDE.md — Project Instructions for Claude Code

## Ground Rule

The current Go implementation is authoritative. If this file, `README.md`, or `ARCHITECTURE.md` disagrees with the code, follow the code.

## What This Project Is

A personal cycling training assistant that currently:

1. Connects to Wahoo through OAuth2
2. Ingests workouts through webhook and manual/scheduled sync
3. Downloads FIT files when available
4. Computes ride metrics from FIT files
5. Accepts subjective notes and body metrics through Telegram
6. Generates weekly reports and weekly plans with Claude
7. Stores rendered HTML in SQLite and optionally delivers summaries to Telegram

## Current Stack

- Language: Go 1.24 in `go.mod`
- HTTP: `github.com/go-chi/chi/v5`
- Database: SQLite via `github.com/mattn/go-sqlite3`
- OAuth2: `golang.org/x/oauth2`
- FIT parsing: `github.com/muktihari/fit`
- Scheduling: `github.com/robfig/cron/v3`
- Telegram: `github.com/go-telegram-bot-api/telegram-bot-api/v5`
- Logging: `log/slog`
- Claude API: raw `net/http`

## Current Layout

```text
cmd/server/          main entrypoint
internal/config/     environment loading
internal/wahoo/      OAuth, API client, sync, webhook models/handler
internal/telegram/   bot and outbound sender
internal/fit/        FIT parsing
internal/analysis/   metric computation and FIT processing pipeline
internal/reporting/  prompt assembly, Claude calls, rendering, delivery, profile evolution
internal/scheduler/  cron wiring
internal/storage/    SQLite migrations and CRUD helpers
internal/web/        router, handlers, middleware, admin UI, SSE log stream
config/              seed athlete profile
templates/           packaged but not the active report rendering path
static/              packaged but not the active report rendering path
testdata/            sample FIT file
```

## Current Runtime Wiring

`cmd/server/main.go` wires:

- config load
- slog tee handler + SSE log broadcaster
- SQLite open + migrations
- athlete profile bootstrap
- FIT files directory creation
- Wahoo auth handler, client, syncer, webhook handler
- athlete config seeding
- FIT processor
- Claude provider and report orchestrator
- optional Telegram delivery service and inbound Telegram bot
- optional cron scheduler
- HTTP router

## Current Config Surface

### Core env vars

- `WAHOO_CLIENT_ID`
- `WAHOO_CLIENT_SECRET`
- `WAHOO_REDIRECT_URI`
- `WAHOO_WEBHOOK_SECRET`
- `TELEGRAM_BOT_TOKEN`
- `TELEGRAM_CHAT_ID`
- `ANTHROPIC_API_KEY`
- `ANTHROPIC_MODEL`
- `SERVER_ADDR`
- `BASE_URL`
- `DATABASE_PATH`
- `FIT_FILES_PATH`
- `ATHLETE_PROFILE_PATH`

### Scheduler env vars

- `CRON_SYNC`
- `CRON_FIT_PROCESSING`
- `CRON_WEEKLY_REPORT`

Important: scheduled jobs are disabled unless their corresponding cron env var is set.

## Athlete Profile

The canonical athlete profile is a markdown file at `ATHLETE_PROFILE_PATH`, defaulting to `/data/athlete-profile.md`.

Current implementation details:

- on first startup, `config/athlete-profile.default.md` is copied into place if the runtime file does not exist
- Claude receives the raw markdown as the system prompt for report and plan generation
- Telegram `/profile` returns the current file
- Telegram `/profile set` replaces the file after downloading the attached markdown
- `/api/profile/evolve` uses recent weekly reports to rewrite the markdown via Claude

Important limitation: the code does not currently parse the markdown back into `athlete_config`. Numeric training values used by analysis come from `athlete_config`, which is seeded in `cmd/server/main.go`.

## Current Telegram Behavior

Implemented inbound commands:

- `/help`
- `/start`
- `/ride`
- `/note`
- `/weight`
- `/bodyfat`
- `/muscle`
- `/profile`
- `/profile set`

Not implemented despite older docs:

- `/status`
- `/week`
- `/plan`

## Current HTTP Surface

Implemented routes:

- `/health`
- `/admin`
- `/wahoo/authorize`
- `/wahoo/callback`
- `/wahoo/webhook`
- `/reports/{id}`
- `/plans/{id}`
- `/api/sync`
- `/api/process`
- `/api/workout/reset-fit`
- `/api/workout/ignore`
- `/api/report`
- `/api/report/send`
- `/api/report/{id}`
- `/api/profile/evolve`
- `/api/body-metrics`
- `/api/notes`
- `/api/notes/{id}`
- `/api/logs/stream`

Not implemented despite older docs:

- `GET /api/profile`
- `POST /api/profile`
- `POST /api/profile/reload`

## Current Rendering Path

The active report/plan rendering path is [`internal/reporting/renderer.go`](/Users/ananchev/Development/cycling-coach/internal/reporting/renderer.go), which builds HTML from an inline template and stores it in `reports.full_html`.

The files under `templates/` and `static/` are not the active renderer today, even though they are still copied into the Docker image.

## Conventions

- Use standard Go formatting
- Wrap errors with context
- Use structured logging with `slog`
- No global application state
- Keep dependencies injected through constructors/struct fields
- Use `context.Context` for external and long-running operations
- Prefer tests alongside the implementation
- Use `database/sql` directly

## Testing

Relevant current coverage exists for:

- Wahoo client, sync, and webhook flows
- FIT parsing
- analysis metrics and processor behavior
- storage CRUD and migrations
- Claude provider and report assembly/orchestration
- Telegram sender and RPE parsing
- SSE log stream

Run:

```bash
go test ./...
```

## Cleanup Guidance

When cleaning up docs or dead assets:

- align docs to the current implementation
- do not revive outdated design assumptions in docs
- explicitly label inactive assets and stale plans as inactive
- remove dead files only in focused cleanup changes, not mixed with behavior work
