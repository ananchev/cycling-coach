# cycling-coach

Personal cycling training assistant. Ingests ride data from Wahoo, collects subjective notes via Telegram, analyzes every ride, and generates weekly reports and training plans.

## How it works

1. **Wahoo KICKR** → ride data flows via webhook/API → FIT files stored and analyzed
2. **Telegram bot** → you log RPE, weight, sleep/stress notes with zero friction
3. **Analysis engine** → computes HR drift, power zones, NP, TSS, TRIMP per ride
4. **Claude API** → generates narrative weekly reports and structured training plans
5. **Delivery** → compact summaries in Telegram, full reports as web pages

## Quick start

```bash
cp .env.example .env
# Fill in your Wahoo, Telegram, and Anthropic credentials

docker compose up -d
```

On first run, the default athlete profile (`config/athlete-profile.default.md`) is copied to the data volume. Update it anytime via Telegram (`/profile set`) or the web API.

Then visit `https://coach.tonio.cc/wahoo/authorize` to connect your Wahoo account.

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md) for the full system design.

## Key files

| File | Purpose |
|---|---|
| `CLAUDE.md` | Claude Code dev instructions (read by Claude Code automatically) |
| `ARCHITECTURE.md` | Full system design, API docs, build phases |
| `config/athlete-profile.default.md` | Seed athlete profile (training zones, FTP, coaching philosophy) |
| `.env.example` | Environment variable template |

The athlete profile is runtime config — it lives in the data volume (`/data/athlete-profile.md`), not in the codebase. The seed file provides the initial version.

## Wahoo setup

### 1. Configure credentials

Register a developer app at [developers.wahooligan.com](https://developers.wahooligan.com) and populate `.env`:

```env
WAHOO_CLIENT_ID=<your client id>
WAHOO_CLIENT_SECRET=<your client secret>
WAHOO_REDIRECT_URI=https://coach.tonio.cc/wahoo/callback
```

For local development, set `WAHOO_REDIRECT_URI=http://localhost:8080/wahoo/callback`.

### 2. Authenticate

Start the server (`make dev`) and open:

```
http://localhost:8080/wahoo/authorize
```

You will be redirected to Wahoo's consent page. After granting access, the callback stores the OAuth2 token in SQLite. The token refreshes automatically before every API call (tokens expire every 2 hours).

### 3. Trigger a sync

```bash
curl -X POST http://localhost:8080/api/sync
```

Response:
```json
{"inserted": 47, "skipped": 0, "errors": null}
```

Subsequent calls are idempotent — workouts already in the database are skipped:
```json
{"inserted": 0, "skipped": 47, "errors": null}
```

FIT files are downloaded to `$FIT_FILES_PATH` (default `/data/fit_files/`) alongside each ingested workout. They are used by the analysis engine in a later phase.

### 4. Verify ingestion

```bash
sqlite3 /data/cycling.db "SELECT wahoo_id, started_at, avg_power, processed FROM workouts ORDER BY started_at DESC LIMIT 5;"
```

## Telegram delivery

Set credentials in `.env`:

```env
TELEGRAM_BOT_TOKEN=<your bot token from @BotFather>
TELEGRAM_CHAT_ID=<your numeric Telegram chat/user ID>
```

Delivery is **optional** — the server starts normally without these values, but `POST /api/report/send` will return 503 and the scheduled Sunday delivery job will be skipped.

### Manual send

Send a specific report by ID (generate one first with `POST /api/report`):

```bash
curl -X POST http://localhost:8080/api/report/send \
  -H 'Content-Type: application/json' \
  -d '{"report_id": 1}'
```

### View report HTML

Full report pages are served at:

```
GET /reports/{id}    → weekly report
GET /plans/{id}      → weekly training plan
```

These are protected by Cloudflare Access in production.

### Delivery state

Each send attempt is recorded in the `report_deliveries` table:

```bash
sqlite3 /data/cycling.db \
  "SELECT r.type, r.week_start, d.status, d.sent_at, d.error
   FROM report_deliveries d
   JOIN reports r ON r.id = d.report_id
   ORDER BY r.week_start DESC;"
```

| status | meaning |
|--------|---------|
| `pending` | delivery record created, not yet sent |
| `sent` | successfully delivered to Telegram |
| `failed` | last send attempt failed (error column has details) |

A failed delivery can be retried by calling `POST /api/report/send` again with the same `report_id`.

## Scheduled pipeline

The scheduler starts automatically with the server and runs three jobs:

| Job | Schedule | What |
|-----|----------|------|
| Wahoo sync | Every 4 hours | Polls for workouts missed by the webhook |
| FIT processing | Every 15 min | Reserved for Phase 6 analysis engine |
| Weekly report + delivery | Sunday 20:00 CET | Generates report + plan for current week, sends both via Telegram |

### Run the full pipeline manually

```bash
# 1. Sync rides
curl -X POST http://localhost:8080/api/sync

# 2. Generate report for a specific week
curl -X POST http://localhost:8080/api/report \
  -H 'Content-Type: application/json' \
  -d '{"type":"weekly_report","week_start":"2026-03-23"}'
# → {"id":1}

# 3. Send via Telegram
curl -X POST http://localhost:8080/api/report/send \
  -H 'Content-Type: application/json' \
  -d '{"report_id":1}'
# → {"status":"sent"}
```

## Development

```bash
make dev     # Run locally
make test    # Run tests
make lint    # Run linter
make build   # Build Docker image
make run     # Start with docker compose
```
