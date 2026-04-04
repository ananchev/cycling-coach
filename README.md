# cycling-coach

Personal cycling training assistant. It ingests workouts from Wahoo, stores subjective notes from Telegram and the admin UI, computes ride metrics from FIT files, generates weekly reports and plans with Claude, and can deliver summaries through Telegram.

This is my first vibe-coded app. The implementation was still reviewed carefully for architecture decisions, code patterns, and runtime behavior, but the project started from that workflow and keeps that spirit.

This file describes the implementation that exists in the repository today. When docs and code differ, the code is the source of truth.

## Core Coaching Loop

The intended use of the app is an AI-assisted weekly coaching cycle:

0. Maintain an athlete profile that describes the athlete, constraints, zones, training history, and coaching instructions
1. Generate a weekly plan with Claude before the week starts
2. Execute the week and let workouts arrive from Wahoo
3. Add notes and body metrics to explain what happened in real life
4. Generate the weekly report so Claude can analyze compliance, workout quality, fatigue signals, and trends
5. Use that report as context for the next weekly plan

That plan -> execution -> report -> next plan loop is the main feature of the app.

The athlete profile is the base context for this loop. It is stored as markdown, sent to Claude during report and plan generation, and acts as the long-lived coaching memory for the app. It describes things like goals, constraints, zone interpretation, training philosophy, warning flags, and current phase.

The profile can evolve over time. The app includes an "Evolve Profile" flow that uses recent weekly reports to refresh the training-history and current-phase sections while preserving the protected coaching structure.

```mermaid
flowchart LR
    P[Athlete Profile] --> W[Weekly Plan with Claude]
    W --> E[Execute Training Week]
    E --> N[Workouts, Notes, Body Metrics]
    N --> R[Weekly Report with Claude]
    R --> U[Evolve Profile / Update Context]
    U --> P
    R --> W
```

## User-Facing Flows

### 1. Connect Wahoo once

- Open `/wahoo/authorize`
- Complete the Wahoo OAuth flow
- The app stores the token and can then ingest workouts through webhook and sync

### 2. Use the admin UI as the main control surface

- Open `/admin`
- Review workouts, processing status, notes, reports, body metrics, and logs
- Trigger sync, FIT processing, report generation, delivery, and profile evolution from the UI
- Inspect per-workout details through the workout action icons

### 3. Keep daily context up to date

- Add ride notes and general notes from Telegram or from the admin UI
- Track weight, body fat, and muscle metrics over time
- Keep the athlete profile current so Claude has the right long-term context
- If no workout exists by 23:50 local time, the app creates a placeholder day entry so the timeline stays complete

### 4. Generate weekly outputs

- Create a weekly plan before the week starts
- Execute the week and capture notes/context as needed
- Create a weekly report for the completed week
- Review rendered HTML in the browser
- Optionally send the summary and link to Telegram

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
- Webhook ingestion explicitly maps Wahoo's documented nested webhook payload, where workout identity/start time are under `workout_summary.workout` and the FIT URL is under `workout_summary.file`

### Analysis

- FIT parsing with `github.com/muktihari/fit`
- Per-ride metrics stored in `ride_metrics`
- Reprocessing, FIT reset, and FIT ignore flows through the admin/API layer
- FIT time-series CSV export from stored FIT files

### Reporting

- Weekly plan first workflow
- Weekly report generation
- Weekly plan generation
- Claude analysis of completed weeks using workouts, metrics, notes, and athlete profile context
- HTML rendering stored in `reports.full_html`
- Telegram delivery with persisted delivery state and retry support
- Athlete profile evolution from recent weekly reports

### Athlete Profile

- Markdown-based long-lived coaching context
- Used as base prompt context for both weekly plans and weekly reports
- Stored outside the database as a runtime file at `ATHLETE_PROFILE_PATH`
- Bootstrapped from `config/athlete-profile.default.md` on first startup
- Can be updated manually or evolved from recent reports through the admin UI
- Contains protected sections required by the profile-evolution flow

### Telegram

- Inbound commands: `/help`, `/start`, `/ride`, `/note`, `/weight`, `/bodyfat`, `/muscle`, `/profile`, `/profile set`
- Outbound report delivery through the Bot API
- Linking ride/note entries to the most recent workout within the last 12 hours

### Admin / HTTP

- Admin UI at `/admin`
- Health endpoint at `/health`
- APIs for sync, processing, report generation, report sending, report deletion, note management, body metrics, log streaming, and profile evolution
- SSE log stream at `/api/logs/stream`
- Workout admin actions for note state, summary-row preview, per-ride zone preview, and FIT time-series download
- Body-metrics charts with date-range filtering

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
- `POST /api/notes`
- `GET /api/notes`
- `PUT /api/notes/{id}`
- `DELETE /api/notes/{id}`
- `GET /api/workouts/{id}/data`
- `GET /api/workouts/{id}/timeseries.csv`
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

One scheduler job is always registered in code and is not env-controlled:

- `23:50 Europe/Amsterdam`: create a manual placeholder workout for that day when no workout exists yet

If a real Wahoo workout for that same day arrives later, the placeholder workout is automatically reconciled away and any notes linked to it are moved onto the real workout.

## Main Interaction: Admin UI

After configuration, the admin UI is the primary way to use the app.

Open:

```text
http://localhost:8080/admin
```

From there you can:

- review workouts and their processing state
- open ride notes and general notes
- inspect the summary row sent to Claude
- inspect the per-ride zone detail sent to Claude
- download FIT time-series CSV data
- trigger sync and FIT processing
- generate weekly reports and plans
- send reports through Telegram
- inspect logs live through SSE-backed log streaming
- review body metrics with date filtering
- review and evolve the athlete profile
- evolve the athlete profile

The backend HTTP endpoints still exist and are useful for automation or debugging, but they are secondary to the admin UI for day-to-day use.

## Quick Start

```bash
docker compose up -d
```

On first startup:

- the SQLite database is created and migrated
- the FIT files directory is created
- `config/athlete-profile.default.md` is copied to `ATHLETE_PROFILE_PATH` if no runtime athlete profile exists
- default athlete config values are seeded into `athlete_config` if keys are missing

Then:

```text
1. Open http://localhost:8080/wahoo/authorize
2. Complete Wahoo authorization
3. Open http://localhost:8080/admin
```

## Admin UI Walkthrough

### Workouts

- See each day’s workout row, keyed visually by external `wahoo_id`
- Use a single data/action column for ride notes, general notes, summary preview, zone preview, and FIT CSV download
- Grey icons indicate that a note or workout-derived artifact is not available for that day
- Placeholder rows fill in days with no recorded workout yet

### Notes

- Add, edit, and delete notes directly from the admin UI
- Use notes to explain skipped sessions, changed workouts, fatigue, travel, or other context
- Ride-linked and general notes are both visible from the workout/day context

### Body Metrics

- Review weight, body fat, and muscle mass over time
- Filter charts by `From` / `To` date range

### Reports and Plans

- Generate weekly plans from the current athlete profile and recent context
- Generate weekly reports that compare completed training against intent and actual outcomes
- Open rendered HTML pages in the browser
- Send generated outputs to Telegram when delivery is configured

### Athlete Profile

- Treat the athlete profile as the coaching baseline for the AI
- Edit it when goals, constraints, or coaching guidance change
- Use "Evolve Profile" when you want the app to refresh the long-term narrative from recent reports

### Logs

- Watch application logs live from the admin UI
- Use this to confirm webhook arrival, sync behavior, processing, and report generation

## API / Curl Examples

The HTTP API remains available for automation, testing, and debugging.

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

Webhook note:

- the polling API and webhook payloads are not treated as identical
- polling uses top-level workout fields plus nested `workout_summary`
- the webhook uses nested workout fields under `workout_summary.workout`
- the webhook FIT URL is read from `workout_summary.file.url`
- the current implementation converts the webhook payload into the shared ingestion shape before inserting/downloading

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

### Create a note from the admin/API side

```bash
curl -X POST http://localhost:8080/api/notes \
  -H 'Content-Type: application/json' \
  -d '{"type":"note","note":"Travel day, skipped training","workout_id":1421}'
```

### Filter body metrics by date

```bash
curl "http://localhost:8080/api/body-metrics?from=2026-03-01&to=2026-03-31"
```

### Download workout FIT time-series data

```bash
curl -O http://localhost:8080/api/workouts/1421/timeseries.csv
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

The `workouts` table now also stores synthetic manual placeholder rows for days where no workout was recorded by 23:50 local time. These placeholders are created with source `manual`, are marked processed immediately, and can later be replaced automatically by a real Wahoo workout from the same day.

Migrations are defined in [`internal/storage/db.go`](/Users/ananchev/Development/cycling-coach/internal/storage/db.go).

## Rendering Note

Report HTML is currently rendered by inline Go code in [`internal/reporting/renderer.go`](/Users/ananchev/Development/cycling-coach/internal/reporting/renderer.go) and stored in the database.

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
