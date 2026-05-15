# /api/mcp/v1 — Frozen Integration Contract

> **WP0 gate. Do not modify without bumping the version path and updating both tracks.**
> App track (WP1/WP2) must produce responses that match these shapes.
> MCP track (WP3–WP7) must stub against the fixtures in `fixtures/`.
> If a discrepancy exists between this document and the fixtures, the fixtures win.

## Conventions

- **Date params**: `YYYY-MM-DD` strings.
- **Timestamps**: RFC 3339 / ISO 8601, e.g. `"2025-11-04T07:12:33Z"`.
- **Nullability**: optional numeric/string fields are `null` in JSON when absent — never omitted.
- **Boolean params**: query string form is `"true"` / `"false"`.
- **Window defaults**: when no date range is given, the window is
  `[today − MCP_DEFAULT_WINDOW_DAYS, today]` (default 28 days).
- **Row cap**: list endpoints apply `MCP_MAX_ROWS` (default 200); when truncated,
  `"truncated": true` and `"hint": "Narrow the date range to see fewer results."` are added.
- **Authentication**: mTLS client certificate at the Cloudflare edge. No additional header.
- **Read-only**: all endpoints are `GET`. No mutations.
- **Excluded fields** (§8 of spec): `wahoo_tokens`, `wyze_scale_imports.raw_payload_json`,
  `system_prompt`, `user_prompt`, `full_html`, any credential or env value.
  The DTO layer must never select these columns.

## Error envelope

All non-2xx responses return:

```json
{ "error": "<message>" }
```

| HTTP status | `error` value       | Trigger                                             |
|-------------|---------------------|-----------------------------------------------------|
| 404         | `"not found"`       | Resource does not exist                             |
| 400         | `"bad request"`     | Invalid / conflicting query params                  |
| 500         | `"internal error"`  | App-side failure                                    |
| 502/503     | `"app unavailable"` | MCP server cannot reach the app (MCP layer only)    |

---

## Endpoints

### 1. GET /api/mcp/v1/profile

Returns the raw athlete profile markdown file.

**Query params**: none

**Response 200**:
```json
{
  "markdown": "<string — raw file contents>"
}
```

**Fixtures**: `profile-normal.json`, `profile-empty.json`

---

### 2. GET /api/mcp/v1/zone-config

Returns FTP, HR max, and zone boundary arrays derived from `athlete_config` via
`analysis.LoadZoneConfig` (fallback: `DefaultZoneConfig` for missing keys).

**Query params**: none

**Response 200**:
```json
{
  "ftp_w": <int>,
  "hr_max_bpm": <int>,
  "hr_zone_bounds": [<Z1Max>, <Z2Max>, <Z3Max>, <Z4Max>],
  "power_zone_bounds": [<Z1Max>, <Z2Max>, <Z3Max>, <Z4Max>]
}
```

`hr_zone_bounds[i]` = upper BPM of zone i+1. Zone 5 = above `hr_zone_bounds[3]`.
Same convention for `power_zone_bounds` (watts).

**Fixture**: `zone-config.json`

---

### 3. GET /api/mcp/v1/workouts

Returns a compact list of workouts with computed metrics, ordered by `started_at DESC`.

**Query params**:

| Param       | Type       | Default            | Notes                                                                      |
|-------------|------------|--------------------|----------------------------------------------------------------------------|
| `from`      | YYYY-MM-DD | today − 28 days    | Inclusive lower bound on `started_at`                                      |
| `to`        | YYYY-MM-DD | today              | Inclusive upper bound on `started_at`                                      |
| `last_days` | int        | —                  | Alternative to `from`/`to`; sets `from = today − last_days`, `to = today` |
| `type`      | string     | —                  | Case-insensitive substring match on resolved workout type name              |
| `wahoo_id`  | string     | —                  | Exact match on `wahoo_id` (used by MCP tool for internal-id resolution)    |
| `limit`     | int        | 200 (MCP_MAX_ROWS) | Max rows returned                                                          |

**Response 200**:
```json
{
  "items": [
    {
      "id": <int>,
      "wahoo_id": <string>,
      "date": <YYYY-MM-DD>,
      "type": <string|null>,
      "duration_min": <float|null>,
      "avg_power_w": <float|null>,
      "np_w": <float|null>,
      "if": <float|null>,
      "vi": <float|null>,
      "avg_hr": <float|null>,
      "tss": <float|null>,
      "processed": <bool>
    }
  ],
  "truncated": <bool>,
  "hint": <string|null>
}
```

Notes:
- `id`: internal SQLite row id — use this in `GET /api/mcp/v1/workouts/{id}`.
- `date`: date portion of `started_at`.
- `duration_min`: `duration_sec / 60.0`; null when `duration_sec` is null.
- `if`: Intensity Factor.
- `vi`: Variability Index (NP / AvgPower); null when either component is null.
- All metric fields are null when `processed = false`.
- `hint` is non-null only when `truncated = true`.
- For the truncated fixture the `items` array must contain exactly `MCP_MAX_ROWS` (200) entries.
  Test helpers should generate the full array programmatically rather than hand-crafting it.

**Fixtures**: `workouts-list-normal.json`, `workouts-list-empty.json`, `workouts-list-truncated.json`

---

### 4. GET /api/mcp/v1/workouts/{id}

Returns full detail for a single workout including power/HR zone percentages,
cadence bands, formatted zone-detail markdown, and linked notes.

**Path param**: `id` — internal integer SQLite row id (from list endpoint `id` field).

**Response 200**:
```json
{
  "id": <int>,
  "wahoo_id": <string>,
  "date": <YYYY-MM-DD>,
  "type": <string|null>,
  "duration_min": <float|null>,
  "avg_power_w": <float|null>,
  "normalized_power_w": <float|null>,
  "intensity_factor": <float|null>,
  "variability_index": <float|null>,
  "avg_hr": <float|null>,
  "max_hr": <float|null>,
  "avg_cadence_rpm": <float|null>,
  "tss": <float|null>,
  "efficiency_factor": <float|null>,
  "hr_drift_pct": <float|null>,
  "processed": <bool>,
  "power_zones": {
    "z1_pct": <float|null>,
    "z2_pct": <float|null>,
    "z3_pct": <float|null>,
    "z4_pct": <float|null>,
    "z5_pct": <float|null>
  },
  "hr_zones": {
    "z1_pct": <float|null>,
    "z2_pct": <float|null>,
    "z3_pct": <float|null>,
    "z4_pct": <float|null>,
    "z5_pct": <float|null>
  },
  "cadence_bands": {
    "lt70_pct": <float|null>,
    "z70_85_pct": <float|null>,
    "z85_100_pct": <float|null>,
    "ge100_pct": <float|null>
  },
  "zone_detail_markdown": <string|null>,
  "ride_notes": <string|null>,
  "general_notes": <string|null>
}
```

Notes:
- `power_zones`, `hr_zones`, `cadence_bands`: all inner fields null when `processed = false`.
- `zone_detail_markdown`: output of `reporting.FormatWorkoutZoneDetail` — null when no zone data.
- `ride_notes`: `type='ride'` notes linked to this workout, concatenated with ` | `.
- `general_notes`: `type='note'` notes linked to this workout, concatenated with ` | `.

**Response 404**: `{ "error": "not found" }`

**Fixtures**: `workout-detail-normal.json`, `workout-detail-notfound.json`

---

### 5. GET /api/mcp/v1/block-context

Returns the assembled coaching markdown for a training window — the same grounding
text the report generator feeds to Claude, produced by `reporting.AssembleInput`.

**Query params**:

| Param       | Type       | Default                    | Notes                                                                                           |
|-------------|------------|----------------------------|-------------------------------------------------------------------------------------------------|
| `from`      | YYYY-MM-DD | —                          | Explicit window start                                                                           |
| `to`        | YYYY-MM-DD | —                          | Explicit window end                                                                             |
| `last_days` | int        | MCP_DEFAULT_WINDOW_DAYS    | Sets `from = today − last_days`, `to = today`                                                  |
| `block`     | `current`  | —                          | Infers window as day-after-latest-saved-report through today; mutually exclusive with date params |

When none of `from`/`to`/`last_days`/`block` are given, defaults to `last_days = MCP_DEFAULT_WINDOW_DAYS`.

**Response 200**:
```json
{
  "period": {
    "from": <YYYY-MM-DD>,
    "to": <YYYY-MM-DD>
  },
  "markdown": <string>,
  "truncated_chars": <bool>
}
```

Notes:
- `markdown`: capped at `MCP_BLOCK_CONTEXT_MAX_CHARS` (60000). Older rides are summarized
  when cap is hit; most-recent rides keep full per-ride detail.
- `truncated_chars: true` when the character cap was applied.

**Fixtures**: `block-context-normal.json`, `block-context-capped.json`

---

### 6. GET /api/mcp/v1/progress

Returns KPI snapshot (selected vs. prior equal-length window), weekly load series,
and the saved AI narrative if present.

**Query params**:

| Param              | Type       | Default     | Notes                               |
|--------------------|------------|-------------|-------------------------------------|
| `from`             | YYYY-MM-DD | today − 28  | Start of selected range (inclusive) |
| `aerobic_only_ef`  | bool       | `true`      | Filter EF to rides with IF < 0.8    |

`to` is always today. Prior range = equal-length window immediately before `from`.

**Response 200**:
```json
{
  "selected_range": { "from": <YYYY-MM-DD>, "to": <YYYY-MM-DD>, "days": <int> },
  "prior_range":    { "from": <YYYY-MM-DD>, "to": <YYYY-MM-DD>, "days": <int> },
  "aerobic_only_ef": <bool>,
  "kpis": {
    "aerobic_efficiency":   <ProgressMetric>,
    "endurance_durability": <ProgressMetric>,
    "cumulative_tss":       <ProgressMetric>,
    "cumulative_trimp":     <ProgressMetric>,
    "average_if":           <ProgressMetric>,
    "completion_rate":      <ProgressMetric>,
    "average_weight_kg":    <ProgressMetric>
  },
  "weekly_load": [
    { "week_start": <YYYY-MM-DD>, "tss": <float>, "trimp": <float> }
  ],
  "saved_narrative": <string|null>
}
```

**ProgressMetric shape**:
```json
{
  "current":   <float|null>,
  "prior":     <float|null>,
  "delta":     <float|null>,
  "delta_pct": <float|null>,
  "trend":     "up" | "down" | "steady"
}
```

Notes:
- `delta` and `delta_pct` are null when either `current` or `prior` is null.
- `delta_pct` is additionally null when `prior == 0`.
- `saved_narrative`: the stored `ProgressAnalysis.Narrative`; null if no analysis has been saved.
- `weekly_load` contains one entry per ISO week (Monday-anchored) in the selected range.

**Fixtures**: `progress-normal.json`, `progress-empty.json`

---

### 7. GET /api/mcp/v1/notes

Returns athlete notes (ride RPE, general notes, body weight entries) filtered by
date range, type, workout, or text query.

**Query params**:

| Param        | Type       | Default            | Notes                                              |
|--------------|------------|--------------------|----------------------------------------------------|
| `from`       | YYYY-MM-DD | today − 28 days    | Inclusive lower bound on `timestamp`               |
| `to`         | YYYY-MM-DD | today              | Inclusive upper bound on `timestamp`               |
| `last_days`  | int        | —                  | Alternative to from/to                             |
| `type`       | string     | —                  | One of `ride`, `note`, `weight`                    |
| `workout_id` | int        | —                  | Filter to notes linked to this internal workout id |
| `query`      | string     | —                  | Case-insensitive substring match on `note` text    |
| `limit`      | int        | 200 (MCP_MAX_ROWS) | Max rows returned                                  |

**Response 200**:
```json
{
  "items": [
    {
      "timestamp": <RFC3339>,
      "type": "ride" | "note" | "weight",
      "rpe": <int|null>,
      "text": <string|null>,
      "weight_kg": <float|null>,
      "body_fat_pct": <float|null>,
      "muscle_mass_kg": <float|null>,
      "body_water_pct": <float|null>,
      "bmr_kcal": <float|null>,
      "workout_id": <int|null>
    }
  ],
  "truncated": <bool>,
  "hint": <string|null>
}
```

Notes:
- `rpe`: meaningful for `type=ride`; null for other types.
- `text`: the `note` field; null for weight-only entries.
- Body metric fields: meaningful for `type=weight`; null for other types.
- `workout_id`: null for standalone notes not linked to a workout.
- `hint` is non-null only when `truncated = true`.

**Fixtures**: `notes-list-normal.json`, `notes-list-empty.json`, `notes-list-truncated.json`

---

### 8. GET /api/mcp/v1/body-metrics

Returns the body weight measurement series with first-to-last deltas. Applies the
Wyze-dedup logic from `storage.ListBodyMetrics` (explicit conflict exclusion plus
same-day manual-vs-Wyze dedup).

**Query params**:

| Param       | Type       | Default            | Notes                  |
|-------------|------------|--------------------|------------------------|
| `from`      | YYYY-MM-DD | today − 28 days    | Inclusive lower bound  |
| `to`        | YYYY-MM-DD | today              | Inclusive upper bound  |
| `last_days` | int        | —                  | Alternative to from/to |
| `limit`     | int        | 200 (MCP_MAX_ROWS) | Max rows returned      |

**Response 200**:
```json
{
  "items": [
    {
      "date": <YYYY-MM-DD>,
      "weight_kg": <float|null>,
      "body_fat_pct": <float|null>,
      "muscle_mass_kg": <float|null>,
      "body_water_pct": <float|null>,
      "bmr_kcal": <float|null>
    }
  ],
  "deltas": {
    "weight_kg": <float|null>,
    "body_fat_pct": <float|null>,
    "muscle_mass_kg": <float|null>,
    "body_water_pct": <float|null>,
    "bmr_kcal": <float|null>
  },
  "truncated": <bool>
}
```

Notes:
- `date`: date portion of the `timestamp` column.
- `deltas`: `last.field − first.field`; null when fewer than 2 data points or either endpoint is null.
- Items ordered by `timestamp ASC`.
- No `hint` field on this endpoint (body metrics do not benefit from the narrow-range prompt).

**Fixtures**: `body-metrics-normal.json`, `body-metrics-empty.json`

---

### 9. GET /api/mcp/v1/reports

Returns report/plan metadata. Three invocation forms share the same list-item shape.

| Invocation form                              | Effect                                                    |
|----------------------------------------------|-----------------------------------------------------------|
| `?type=all&limit=N` (default)                | Both types interleaved, ordered by `ListReportsWithDelivery` |
| `?type=weekly_report&limit=N`                | Weekly reports only, newest first                         |
| `?type=weekly_plan&limit=N`                  | Weekly plans only, newest first                           |
| `?latest=weekly_report`                      | Single most-recent weekly report (list with 1 item)       |
| `?latest=weekly_plan`                        | Single most-recent weekly plan (list with 1 item)         |
| `?type=weekly_report&week_start=YYYY-MM-DD`  | Single report by type + week_start (list with 0 or 1 item)|

**Query params**:

| Param        | Type                                    | Default | Notes                                   |
|--------------|-----------------------------------------|---------|-----------------------------------------|
| `type`       | `weekly_report`\|`weekly_plan`\|`all`   | `all`   | Filter by type                          |
| `limit`      | int                                     | 10      | Max rows (hard cap, not a budget guard) |
| `latest`     | `weekly_report`\|`weekly_plan`          | —       | Returns single most-recent of that type |
| `week_start` | YYYY-MM-DD                              | —       | Lookup by type + week_start             |

**Response 200**:
```json
{
  "items": [
    {
      "id": <int>,
      "type": "weekly_report" | "weekly_plan",
      "week_start": <YYYY-MM-DD>,
      "week_end": <YYYY-MM-DD>,
      "created_at": <RFC3339>,
      "has_summary": <bool>,
      "has_narrative": <bool>,
      "delivery_status": "sent" | "failed" | null
    }
  ]
}
```

Notes:
- No `truncated` field; `limit` is a hard cap, not a budget guard.
- `delivery_status`: null when no delivery record exists.
- `has_summary`: `summary_text IS NOT NULL AND summary_text != ''`.
- `has_narrative`: `narrative_text IS NOT NULL AND narrative_text != ''`.

**Fixtures**: `reports-list-normal.json`, `reports-list-empty.json`

---

### 10. GET /api/mcp/v1/reports/{id}

Returns full report content including narrative markdown.

**Path param**: `id` — integer row id.

The same response shape is returned by the query-param forms of endpoint 9:
- `GET /api/mcp/v1/reports?latest=weekly_report` → most-recent report detail
- `GET /api/mcp/v1/reports?type=weekly_report&week_start=2025-10-27` → lookup by type+week_start

The router distinguishes the list form (no `{id}`, returns `{ "items": [...] }`) from
the detail form (`{id}` present, or `?latest=` / `?type=&week_start=` present, returns the
detail shape directly — no wrapping `items` array).

**Response 200**:
```json
{
  "id": <int>,
  "type": "weekly_report" | "weekly_plan",
  "week_start": <YYYY-MM-DD>,
  "week_end": <YYYY-MM-DD>,
  "created_at": <RFC3339>,
  "summary_markdown": <string|null>,
  "narrative_markdown": <string|null>
}
```

Notes:
- `summary_markdown`: the `summary_text` column (compact 5-line Telegram summary).
- `narrative_markdown`: the `narrative_text` column (full coaching narrative).
- **Never returned**: `full_html`, `system_prompt`, `user_prompt`.

**Response 404**: `{ "error": "not found" }`

**Fixtures**: `report-detail-normal.json`, `report-detail-notfound.json`

