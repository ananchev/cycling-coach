# cycling-coach

Personal cycling training assistant. It ingests workouts from Wahoo, stores subjective notes from Telegram, computes ride metrics from FIT files, generates weekly reports and plans with Claude, and can deliver summaries through Telegram.

This file describes the implementation that exists in the repository today. When docs and code differ, the code is the source of truth.

## Current Runtime Flow

1. Wahoo OAuth is completed through `/wahoo/authorize` and `/wahoo/callback`.
2. Workouts arrive through the Wahoo webhook and optional manual/scheduled sync.
3. Workout rows are stored in SQLite and FIT files are downloaded to disk when available.
4. FIT processing computes per-ride metrics such as NP, IF, TSS, TRIMP, HR drift, decoupling, zone distribution, and a power-zone timeline.
5. Weekly reports and plans are assembled from workouts, computed metrics, notes, and the athlete profile markdown.
6. Claude returns structured JSON with a Telegram-sized summary plus a full narrative.
7. The app renders HTML, stores it in the database, serves it at `/reports/{id}` or `/plans/{id}`, and can send the summary + link to Telegram.

## Implemented Areas

### Wahoo

- OAuth2 token storage and refresh
- `POST /wahoo/webhook`
- Paginated workout sync through the Wahoo API
- FIT download when the API payload contains a file URL
- Idempotent workout ingestion keyed by `wahoo_id`

### Analysis

- FIT parsing with `github.com/muktihari/fit`
- Per-ride metrics stored in `ride_metrics`
- Reprocessing, FIT reset, and FIT ignore flows through the admin/API layer

### Reporting

- Weekly report generation
- Weekly plan generation
- HTML rendering stored in `reports.full_html`
- Telegram delivery with persisted delivery state and retry support
- Athlete profile evolution from recent weekly reports

### Telegram

- Inbound commands: `/help`, `/start`, `/ride`, `/note`, `/weight`, `/bodyfat`, `/muscle`, `/profile`, `/profile set`
- Outbound report delivery through the Bot API
- Linking ride/note entries to the most recent workout within the last 12 hours

### Admin / HTTP

- Admin UI at `/admin`
- Health endpoint at `/health`
- APIs for sync, processing, report generation, report sending, report deletion, note management, body metrics, log streaming, and profile evolution
- SSE log stream at `/api/logs/stream`

## Current Routes

### Public / integration routes

- `GET /health`
- `GET /wahoo/authorize`
- `GET /wahoo/callback`
- `POST /wahoo/webhook`

### Report pages

- `GET /reports/{id}`
- `GET /plans/{id}`

### Admin/API routes

- `GET /admin`
- `POST /api/sync`
- `POST /api/process`
- `POST /api/workout/reset-fit`
- `POST /api/workout/ignore`
- `POST /api/report`
- `POST /api/report/send`
- `DELETE /api/report/{id}`
- `POST /api/profile/evolve`
- `GET /api/body-metrics`
- `GET /api/notes`
- `PUT /api/notes/{id}`
- `DELETE /api/notes/{id}`
- `GET /api/logs/stream`

## Current Telegram Commands

- `/help`
- `/start`
- `/ride <text>`
- `/note <text>`
- `/weight <kg>`
- `/bodyfat <pct>`
- `/muscle <kg>`
- `/profile`
- `/profile set` with an attached `.md` file

Commands such as `/status`, `/week`, and `/plan` are mentioned in older design docs but are not implemented in the current codebase.

## Configuration

Copy `.env.example` to `.env` and fill in the required values.

```bash
cp .env.example .env
```

### Required for Wahoo integration

```env
WAHOO_CLIENT_ID=
WAHOO_CLIENT_SECRET=
WAHOO_REDIRECT_URI=http://localhost:8080/wahoo/callback
```

For production, set `WAHOO_REDIRECT_URI` to your public callback URL.

### Optional integrations

Telegram is optional. If `TELEGRAM_BOT_TOKEN` or `TELEGRAM_CHAT_ID` is missing, inbound bot handling and report delivery are disabled.

Claude-backed report generation requires `ANTHROPIC_API_KEY`.

### Scheduler

The scheduler exists in code, but jobs are registered only when cron environment variables are set:

```env
CRON_SYNC=
CRON_FIT_PROCESSING=
CRON_WEEKLY_REPORT=
```

If all three are empty, the scheduler starts with no active jobs.

## Quick Start

```bash
docker compose up -d
```

On first startup:

- the SQLite database is created and migrated
- the FIT files directory is created
- `config/athlete-profile.default.md` is copied to `ATHLETE_PROFILE_PATH` if no runtime athlete profile exists
- default athlete config values are seeded into `athlete_config` if keys are missing

Then open:

```text
http://localhost:8080/wahoo/authorize
```

## Manual Operations

### Sync workouts

```bash
curl -X POST http://localhost:8080/api/sync
```

Optional date range:

```bash
curl -X POST http://localhost:8080/api/sync \
  -H 'Content-Type: application/json' \
  -d '{"from":"2026-03-01","to":"2026-03-31"}'
```

### Process FIT files

```bash
curl -X POST http://localhost:8080/api/process
```

Reprocess everything:

```bash
curl -X POST http://localhost:8080/api/process \
  -H 'Content-Type: application/json' \
  -d '{"reprocess_all":true}'
```

### Generate a weekly report or plan

```bash
curl -X POST http://localhost:8080/api/report \
  -H 'Content-Type: application/json' \
  -d '{"type":"weekly_report","week_start":"2026-03-23"}'
```

```bash
curl -X POST http://localhost:8080/api/report \
  -H 'Content-Type: application/json' \
  -d '{"type":"weekly_plan","week_start":"2026-03-30","user_prompt":"Travelling Tuesday, keep Wednesday short"}'
```

### Send a generated report

```bash
curl -X POST http://localhost:8080/api/report/send \
  -H 'Content-Type: application/json' \
  -d '{"report_id":1}'
```

## Data Model

Main tables:

- `wahoo_tokens`
- `workouts`
- `ride_metrics`
- `athlete_notes`
- `athlete_config`
- `reports`
- `report_deliveries`
- `workout_types`

Migrations are defined in [`internal/storage/db.go`](/Users/ananchev/Development/cycling-coach/internal/storage/db.go).

## Rendering Note

Report HTML is currently rendered by inline Go code in [`internal/reporting/renderer.go`](/Users/ananchev/Development/cycling-coach/internal/reporting/renderer.go) and stored in the database.

The repo also still contains:

- [`templates/report.html`](/Users/ananchev/Development/cycling-coach/templates/report.html)
- [`templates/plan.html`](/Users/ananchev/Development/cycling-coach/templates/plan.html)
- [`static/style.css`](/Users/ananchev/Development/cycling-coach/static/style.css)

These files are packaged into the image but are not the active rendering path used by the current implementation.

## Development

```bash
make dev
make test
make lint
make build
make run
```

## Related Docs

- [`CLAUDE.md`](/Users/ananchev/Development/cycling-coach/CLAUDE.md) for repo-specific development guidance
- [`ARCHITECTURE.md`](/Users/ananchev/Development/cycling-coach/ARCHITECTURE.md) for an implementation-aligned architecture summary
