package web

import (
	"database/sql"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"cycling-coach/internal/storage"
)

// adminDefaultWeeks is how many weeks back the reports table shows by default.
const adminDefaultWeeks = 8

var adminTmpl = template.Must(template.New("admin").Funcs(template.FuncMap{
	"fmtDate": func(t time.Time) string { return t.Format("2006-01-02") },
	"deref": func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	},
	"fmtOpt": func(v *float64, format string) template.HTML {
		if v == nil {
			return `<span style="color:#ccc">—</span>`
		}
		return template.HTML(fmt.Sprintf(format, *v))
	},
	"fmtDuration": func(sec *int64) string {
		if sec == nil {
			return "—"
		}
		m := *sec / 60
		return fmt.Sprintf("%d:%02d", m/60, m%60)
	},
}).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Cycling Coach — Admin</title>
<style>
  *{box-sizing:border-box;margin:0;padding:0}
  body{font-family:system-ui,sans-serif;background:#f5f5f5;color:#222;padding:24px}
  h1{font-size:1.4rem;margin-bottom:24px}
  h2{font-size:1rem;font-weight:600;margin-bottom:12px;text-transform:uppercase;letter-spacing:.05em;color:#555}
  .card{background:#fff;border:1px solid #ddd;border-radius:8px;padding:20px;margin-bottom:20px}
  label{display:block;font-size:.85rem;font-weight:500;margin-bottom:4px;color:#444}
  input[type=date],input[type=text],select{
    padding:6px 10px;border:1px solid #ccc;border-radius:4px;font-size:.9rem;width:160px}
  .row{display:flex;gap:12px;align-items:flex-end;flex-wrap:wrap}
  .field{display:flex;flex-direction:column}
  button{padding:7px 16px;background:#2563eb;color:#fff;border:none;border-radius:4px;
    font-size:.9rem;cursor:pointer;height:34px;display:inline-flex;align-items:center;gap:6px}
  button:hover{background:#1d4ed8}
  button:disabled{background:#93c5fd;cursor:not-allowed}
  button.secondary{background:#e5e7eb;color:#222}
  button.secondary:hover{background:#d1d5db}
  button.danger{background:#fee2e2;color:#991b1b;border:1px solid #fca5a5}
  button.danger:hover{background:#fecaca}
  .spinner{display:none;width:14px;height:14px;border:2px solid #fff;border-top-color:transparent;border-radius:50%;animation:spin .7s linear infinite}
  @keyframes spin{to{transform:rotate(360deg)}}
  .result{margin-top:10px;font-size:.85rem;padding:8px 12px;border-radius:4px;display:none}
  .result.ok{background:#dcfce7;color:#166534}
  .result.err{background:#fee2e2;color:#991b1b}
  table{width:100%;border-collapse:collapse;font-size:.88rem}
  th{text-align:left;padding:8px 10px;border-bottom:2px solid #e5e7eb;color:#555;font-weight:600}
  td{padding:8px 10px;border-bottom:1px solid #f0f0f0;vertical-align:middle}
  tr:last-child td{border-bottom:none}
  .badge{display:inline-block;padding:2px 8px;border-radius:10px;font-size:.78rem;font-weight:600}
  .badge.green{background:#dcfce7;color:#166534}
  .badge.red{background:#fee2e2;color:#991b1b}
  .badge.yellow{background:#fef9c3;color:#854d0e}
  .badge.grey{background:#f3f4f6;color:#6b7280}
  .act-btn{padding:4px 10px;font-size:.8rem;height:auto}
  .filter-form{display:flex;gap:10px;align-items:flex-end;flex-wrap:wrap;margin-bottom:16px}
  .hint{font-size:.8rem;color:#888;margin-top:4px}
  .tabs{display:flex;gap:0;border-bottom:2px solid #e5e7eb;margin-bottom:20px}
  .tab{padding:10px 20px;cursor:pointer;font-weight:500;font-size:.95rem;color:#6b7280;border-bottom:2px solid transparent;margin-bottom:-2px;transition:all .15s}
  .tab:hover{color:#222}
  .tab.active{color:#2563eb;border-bottom-color:#2563eb}
  .tab-pane{display:none}
  .tab-pane.active{display:block}
  td.num{text-align:right;font-variant-numeric:tabular-nums}
  .status-dot{display:inline-block;width:8px;height:8px;border-radius:50%;margin-right:4px}
  .status-dot.yes{background:#22c55e}
  .status-dot.no{background:#d1d5db}
  .note-icon{cursor:pointer;font-size:1rem}
  .icon-row{display:flex;gap:8px;align-items:center}
  .icon-btn{font-size:1rem;text-decoration:none;cursor:pointer;display:inline-flex;align-items:center}
  .icon-btn.muted{opacity:.28;cursor:default}
  .icon-btn.active:hover{transform:translateY(-1px)}
  .modal-overlay{display:none;position:fixed;inset:0;background:rgba(0,0,0,.4);z-index:100;justify-content:center;align-items:center}
  .modal-overlay.show{display:flex}
  .modal{background:#fff;border-radius:8px;padding:20px 24px;max-width:500px;width:90%;box-shadow:0 8px 32px rgba(0,0,0,.2)}
  .modal h3{margin-bottom:12px;font-size:1rem}
  .modal p{font-size:.9rem;line-height:1.5;white-space:pre-wrap}
  .modal button{margin-top:12px}
  .progress-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:12px;margin:16px 0}
  .progress-kpi{border:1px solid #e5e7eb;border-radius:8px;padding:14px;background:#fafafa;min-height:220px}
  .progress-kpi h3{font-size:.95rem;margin-bottom:4px}
  .progress-kpi .explain{font-size:.82rem;color:#666;line-height:1.45;margin-bottom:10px}
  .progress-kpi .metric-label{font-size:.72rem;color:#888;text-transform:uppercase;letter-spacing:.04em;margin-top:8px;margin-bottom:2px}
  .progress-kpi .value{font-size:1.5rem;font-weight:700;line-height:1.15}
  .progress-kpi .trend-text{font-size:1.05rem;font-weight:700;line-height:1.2}
  .progress-kpi .delta{font-size:.82rem;color:#555}
  .progress-kpi .empty-note{font-size:.8rem;color:#8b5e00;line-height:1.4;margin-top:6px}
  .trend-up{color:#166534}
  .trend-down{color:#991b1b}
  .trend-steady{color:#6b7280}
  .progress-analysis{background:#f8fafc;border:1px solid #e2e8f0;border-radius:8px;padding:14px;line-height:1.6;font-size:.92rem}
  .progress-analysis h1,.progress-analysis h2,.progress-analysis h3{margin:16px 0 8px;font-size:1.05rem;color:#111}
  .progress-analysis h1{font-size:1.15rem}
  .progress-analysis p{margin-bottom:10px}
  .progress-analysis ul,.progress-analysis ol{margin:0 0 12px 20px}
  .progress-analysis li{margin-bottom:4px}
  .progress-analysis strong{font-weight:700}
  .progress-analysis em{font-style:italic}
  .progress-analysis code{background:#eef2f7;padding:1px 4px;border-radius:3px;font-size:.88em}
</style>
</head>
<body>
<h1>🚴 Cycling Coach — Admin</h1>

<div class="tabs">
  <div class="tab{{if eq .ActiveTab "actions"}} active{{end}}" onclick="switchTab('actions')">Actions</div>
  <div class="tab{{if eq .ActiveTab "workouts"}} active{{end}}" onclick="switchTab('workouts')">Workouts</div>
  <div class="tab{{if eq .ActiveTab "reports"}} active{{end}}" onclick="switchTab('reports')">Reports & Plans</div>
  <div class="tab{{if eq .ActiveTab "wyze"}} active{{end}}" onclick="switchTab('wyze')">Wyze Sync</div>
  <div class="tab{{if eq .ActiveTab "body"}} active{{end}}" onclick="switchTab('body')">Body Metrics</div>
  <div class="tab{{if eq .ActiveTab "progress"}} active{{end}}" onclick="switchTab('progress')">Progress</div>
  <div class="tab{{if eq .ActiveTab "logs"}} active{{end}}" onclick="switchTab('logs')">Logs</div>
</div>

<!-- ==================== ACTIONS TAB ==================== -->
<div class="tab-pane{{if eq .ActiveTab "actions"}} active{{end}}" id="tab-actions">

<!-- WAHOO SYNC -->
<div class="card">
  <h2>Wahoo Sync</h2>
  <div class="row">
    <div class="field"><label>From (optional)</label><input type="date" id="sync-from"></div>
    <div class="field"><label>To (optional)</label><input type="date" id="sync-to"></div>
    <button id="sync-btn" onclick="runSync()"><span class="spinner" id="sync-spin"></span>Sync Now</button>
  </div>
  <div class="result" id="sync-result"></div>
</div>

<!-- FIT PROCESSING -->
<div class="card">
  <h2>FIT Processing</h2>
  <div class="row">
    <div class="field">
      <label>Mode</label>
      <select id="proc-mode" onchange="updateProcFields()">
        <option value="new">Process new (unprocessed only)</option>
        <option value="range">Process date range</option>
        <option value="all">Process all (reprocess everything)</option>
      </select>
    </div>
    <div class="field" id="proc-from-field" style="display:none"><label>From</label><input type="date" id="proc-from"></div>
    <div class="field" id="proc-to-field" style="display:none"><label>To</label><input type="date" id="proc-to"></div>
    <button id="proc-btn" onclick="runProcess()"><span class="spinner" id="proc-spin"></span>Process Now</button>
  </div>
  <p class="hint" id="proc-hint">Processes workouts that have not yet been analysed.</p>
  <div class="result" id="proc-result"></div>
</div>

<!-- CLOSE BLOCK -->
<div class="card">
  <h2>Close Block & Generate Next Plan</h2>
  <div style="margin-bottom:8px">
    {{if .PreviousBlockStart}}
    <p class="hint" id="close-block-prev">Previous closed block: {{.PreviousBlockStart}} to {{.PreviousBlockEnd}}</p>
    {{else}}
    <p class="hint" id="close-block-prev">Previous closed block: none yet</p>
    {{end}}
    <p class="hint" id="close-block-interval">{{.CurrentIntervalText}}</p>
  </div>
  <div class="row">
    <div class="field">
      <label>Block start</label>
      <input type="date" id="close-block-start" value="{{.InferredBlockStart}}" {{if .HasPreviousClosedReport}}disabled{{end}}>
    </div>
    <div class="field"><label>Block end</label><input type="date" id="close-block-end" value="{{.CurrentDate}}"></div>
    <button id="close-block-btn" onclick="runCloseBlock()"><span class="spinner" id="close-block-spin"></span>Close Block</button>
  </div>
  {{if .HasPreviousClosedReport}}
  <p class="hint" style="margin-top:8px">Block start is inferred automatically from the day after the last closed report.</p>
  {{else}}
  <p class="hint" style="margin-top:8px">Block start is needed only for the first close-block run, before any previous closed report exists.</p>
  {{end}}
  <div style="margin-top:12px">
    <label>Clarification for the next plan (optional)</label>
    <textarea id="close-block-prompt" rows="3" style="width:100%;padding:8px 10px;border:1px solid #ccc;border-radius:4px;font-size:.9rem;font-family:inherit;resize:vertical" placeholder="e.g. Shift the hard session later in the week, keep Saturday flexible, travel on Tuesday..."></textarea>
    <p class="hint">Usually only the block end date is needed. The block start is inferred from the last saved report, then the finished-block report and the next 7-day plan are generated automatically. Block start is only needed for the first close-block run, before any previous report exists.</p>
  </div>
  <div class="result" id="close-block-result"></div>
</div>

<!-- ATHLETE PROFILE -->
<div class="card">
  <h2>Athlete Profile</h2>
  <div class="row">
    <div class="field">
      <label>Base on last N reports</label>
      <input type="number" id="evolve-n" value="8" min="1" max="52" style="width:80px">
    </div>
    <button id="evolve-btn" onclick="runEvolveProfile()"><span class="spinner" id="evolve-spin"></span>Evolve Profile</button>
  </div>
  <p class="hint">Sends the last N reports to Claude and rewrites the athlete profile. The current profile is backed up with a timestamp suffix before being replaced.</p>
  <div class="result" id="evolve-result"></div>
</div>

</div><!-- /tab-actions -->

<!-- ==================== WYZE TAB ==================== -->
<div class="tab-pane{{if eq .ActiveTab "wyze"}} active{{end}}" id="tab-wyze">
<div class="card">
  <h2>Wyze Scale Sync</h2>
  <div class="row">
    <div class="field"><label>From</label><input type="date" id="wyze-from"></div>
    <div class="field"><label>To</label><input type="date" id="wyze-to"></div>
    <button id="wyze-sync-btn" onclick="runWyzeSync()"><span class="spinner" id="wyze-sync-spin"></span>Sync Wyze</button>
  </div>
  <p class="hint" style="margin-top:8px">Imports Wyze scale records for the selected period. If a nearby manual body-metric entry exists, the Wyze row is still imported and tracked as <code>conflict_with_manual</code>.</p>
  <div class="result" id="wyze-sync-result"></div>
</div>

<div class="card">
  <h2>Wyze Records</h2>
  <form method="get" action="/admin" class="filter-form">
    <input type="hidden" name="tab" value="wyze">
    <div class="field"><label>From</label><input type="date" name="wyze_from" value="{{.WyzeFilterFrom}}"></div>
    <div class="field"><label>To</label><input type="date" name="wyze_to" value="{{.WyzeFilterTo}}"></div>
    <button type="submit">Filter</button>
    <a href="/admin?tab=wyze"><button type="button" class="secondary">Reset</button></a>
  </form>
  <p class="hint" style="margin-bottom:12px">All imported Wyze rows are listed here. If a record has a nearby manual body-metric entry, it is marked as a conflict and can be resolved with the delete actions.</p>
  <div style="overflow-x:auto">
  <table>
    <thead><tr>
      <th>ID</th><th>Measured</th><th>Source</th><th>Record / Note</th><th>Entry</th><th>Conflict</th><th>Delete</th>
    </tr></thead>
    <tbody id="wyze-conflicts-body">
    {{if .WyzeRecords}}
    {{range .WyzeRecords}}
    <tr id="wyze-record-{{.Note.ID}}">
      <td>#{{.Note.ID}}</td>
      <td style="white-space:nowrap">{{fmtDate .Note.Timestamp}}</td>
      <td><span class="badge {{if eq .Source "wyze"}}green{{else}}grey{{end}}">{{.Source}}</span></td>
      <td style="white-space:nowrap">{{if eq .Source "wyze"}}{{if .WyzeRecordID}}{{.WyzeRecordID}}<br>{{end}}{{end}}<span class="hint">note #{{.Note.ID}}</span></td>
      <td>
        {{fmtOpt .Note.WeightKG "%.1f kg"}}
        · {{fmtOpt .Note.BodyFatPct "%.1f%% bf"}}
        · {{fmtOpt .Note.MuscleMassKG "%.1f kg muscle"}}
        · {{fmtOpt .Note.BodyWaterPct "%.1f%% water"}}
        · {{fmtOpt .Note.BMRKcal "%.0f bmr"}}
      </td>
      <td>
        {{if .ConflictID}}
        <span class="badge yellow">#{{.ConflictID}} conflict</span>
        {{else if .Counterpart}}
        <span class="badge yellow">duplicate of #{{.Counterpart.ID}}</span>
        {{else}}
        <span class="badge grey">none</span>
        {{end}}
      </td>
      <td>
        {{if or .ConflictID .Counterpart}}
        <button class="act-btn danger" onclick="deleteWyzeRecord({{.Note.ID}}, '{{.Source}}')">Delete</button>
        {{else}}
        <span style="color:#888">—</span>
        {{end}}
      </td>
    </tr>
    {{end}}
    {{else}}
    <tr id="wyze-conflicts-empty-row">
      <td colspan="7" style="color:#888;font-size:.9rem">No body-metric records found for this filter.</td>
    </tr>
    {{end}}
    </tbody>
  </table>
  </div>
  <div class="result" id="wyze-conflict-result"></div>
</div>
</div><!-- /tab-wyze -->

<!-- ==================== WORKOUTS TAB ==================== -->
<div class="tab-pane{{if eq .ActiveTab "workouts"}} active{{end}}" id="tab-workouts">
<div class="card">
  <h2>Workouts</h2>
  <form method="get" action="/admin" class="filter-form">
    <input type="hidden" name="tab" value="workouts">
    <div class="field"><label>From</label><input type="date" name="from" value="{{.FilterFrom}}"></div>
    <div class="field"><label>To</label><input type="date" name="to" value="{{.FilterTo}}"></div>
    <button type="submit">Filter</button>
    <a href="/admin?tab=workouts"><button type="button" class="secondary">Reset</button></a>
  </form>
  <p class="hint" style="margin-bottom:12px">Default view is the last 8 weeks when no filter is set.</p>
  {{if .Workouts}}
  <div style="overflow-x:auto">
  <table>
    <thead><tr>
      <th>Workout ID</th><th>Date</th><th>Type</th><th>Duration</th>
      <th>Avg Power</th><th>Avg HR</th><th>NP</th><th>TSS</th><th>HR Drift</th>
      <th>FIT</th><th>Processed</th><th title="Workout data and notes">Data</th>
    </tr></thead>
    <tbody>
    {{range .Workouts}}
    <tr>
      <td title="internal row id {{.ID}}">{{.WahooID}}</td>
      <td style="white-space:nowrap">{{fmtDate .StartedAt}}</td>
      <td>{{if .WorkoutType}}{{deref .WorkoutType}}{{else}}—{{end}}</td>
      <td class="num">{{fmtDuration .DurationSec}}</td>
      <td class="num">{{fmtOpt .AvgPower "%.0f W"}}</td>
      <td class="num">{{fmtOpt .AvgHR "%.0f"}}</td>
      <td class="num">{{fmtOpt .NormalizedPower "%.0f W"}}</td>
      <td class="num">{{fmtOpt .TSS "%.0f"}}</td>
      <td class="num">{{fmtOpt .HRDriftPct "%.1f%%"}}</td>
      <td>{{if .FITFilePath}}<span class="status-dot yes"></span>{{else}}<span class="status-dot no"></span>{{end}}</td>
      <td>{{if .Processed}}<span class="status-dot yes"></span>{{else}}<span class="status-dot no"></span>{{end}}</td>
      <td>
        <div class="icon-row">
          <span class="icon-btn {{if .RideNotes}}active{{else}}muted{{end}}" onclick="showNotes({{.ID}}, 'ride')" title="Ride notes">💬</span>
          <span class="icon-btn {{if .GeneralNotes}}active{{else}}muted{{end}}" onclick="showNotes({{.ID}}, 'note')" title="General notes">📝</span>
          {{if ne .Source "manual"}}
          <span class="icon-btn active" onclick="showWorkoutData({{.ID}}, 'summary')" title="View summary row">📊</span>
          {{else}}
          <span class="icon-btn muted" title="No workout stored for this day">📊</span>
          {{end}}
          {{if or .PwrZ1Pct .ZoneTimeline}}
          <span class="icon-btn active" onclick="showWorkoutData({{.ID}}, 'zones')" title="View zone detail">🧩</span>
          {{else}}
          <span class="icon-btn muted" title="No per-ride zone detail available">🧩</span>
          {{end}}
          {{if .FITFilePath}}
          <a class="icon-btn active" href="/api/workouts/{{.ID}}/timeseries.csv" title="Download time-phased data">⬇️</a>
          {{else}}
          <span class="icon-btn muted" title="No FIT time-series data available">⬇️</span>
          {{end}}
        </div>
      </td>
    </tr>
    {{end}}
    </tbody>
  </table>
  </div>
  {{else}}
  <p style="color:#888;font-size:.9rem">No workouts found for this date range.</p>
  {{end}}
</div>
</div><!-- /tab-workouts -->

<!-- ==================== REPORTS TAB ==================== -->
<div class="tab-pane{{if eq .ActiveTab "reports"}} active{{end}}" id="tab-reports">
<div class="card">
  <h2>Reports & Plans</h2>
  <form method="get" action="/admin" class="filter-form">
    <input type="hidden" name="tab" value="reports">
    <div class="field"><label>From start date</label><input type="date" name="from" value="{{.FilterFrom}}"></div>
    <div class="field"><label>To start date</label><input type="date" name="to" value="{{.FilterTo}}"></div>
    <button type="submit">Filter</button>
    <a href="/admin?tab=reports"><button type="button" class="secondary">Reset</button></a>
  </form>
  <p class="hint" style="margin-bottom:12px">Each row pairs the plan and report for a single period — the plan prescribes, the report analyzes what actually happened.</p>
  {{if .ReportPeriods}}
  <table>
    <thead><tr>
      <th>Period</th><th>Plan</th><th>Report</th>
    </tr></thead>
    <tbody>
    {{range .ReportPeriods}}
    <tr>
      <td style="white-space:nowrap">{{fmtDate .Start}} → {{fmtDate .End}}</td>
      <td id="cell-{{if .Plan}}{{.Plan.ID}}{{else}}plan-{{fmtDate .Start}}{{end}}">{{template "artifactCell" .Plan}}</td>
      <td id="cell-{{if .Report}}{{.Report.ID}}{{else}}report-{{fmtDate .Start}}{{end}}">{{template "artifactCell" .Report}}</td>
    </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}
  <p style="color:#888;font-size:.9rem">No reports found for this date range.</p>
  {{end}}
{{define "artifactCell"}}{{if .}}<span style="font-weight:500">#{{.ID}}</span>
        <span class="icon-btn {{if .HasSystemPrompt}}active{{else}}muted{{end}}" onclick="showReportPrompt({{.ID}}, 'system')" title="View system prompt">🧠</span>
        <span class="icon-btn {{if .HasUserPrompt}}active{{else}}muted{{end}}" onclick="showReportPrompt({{.ID}}, 'user')" title="View user prompt">📨</span>
        &nbsp;
        <button class="act-btn secondary" onclick="sendReport({{.ID}})">Send</button>
        {{if .FullHTML}}&nbsp;<a href="{{if eq (print .Type) "weekly_plan"}}/plans/{{else}}/reports/{{end}}{{.ID}}" target="_blank"><button class="act-btn secondary">View</button></a>{{end}}
        &nbsp;<button class="act-btn danger" onclick="deleteReport({{.ID}})">Delete</button>{{else}}<span style="color:#bbb">—</span>{{end}}{{end}}
  <div class="result" id="send-result"></div>
</div>
</div><!-- /tab-reports -->

<!-- ==================== BODY METRICS TAB ==================== -->
<div class="tab-pane{{if eq .ActiveTab "body"}} active{{end}}" id="tab-body">
<div class="card">
  <h2>Body Metrics</h2>
  <div class="row" style="margin-bottom:12px">
    <div class="field"><label>From</label><input type="date" id="body-from"></div>
    <div class="field"><label>To</label><input type="date" id="body-to"></div>
    <button type="button" onclick="applyBodyFilters()">Apply</button>
    <button type="button" class="secondary" onclick="resetBodyFilters()">Reset</button>
  </div>
  <p class="hint" style="margin-bottom:16px">Data logged via Telegram: /weight, /bodyfat, /muscle</p>
  <div id="body-charts">
    <canvas id="chart-weight" height="200"></canvas>
    <canvas id="chart-bodyfat" style="margin-top:20px" height="200"></canvas>
    <canvas id="chart-muscle" style="margin-top:20px" height="200"></canvas>
    <canvas id="chart-water" style="margin-top:20px" height="200"></canvas>
    <canvas id="chart-bmr" style="margin-top:20px" height="200"></canvas>
  </div>
  <p id="body-empty" style="display:none;color:#888;font-size:.9rem">No body metrics recorded yet. Use Telegram or Wyze sync to log weight, body fat, muscle mass, hydration, and BMR.</p>
</div>
</div><!-- /tab-body -->

<!-- ==================== PROGRESS TAB ==================== -->
<div class="tab-pane{{if eq .ActiveTab "progress"}} active{{end}}" id="tab-progress">
<div class="card">
  <h2>Progress</h2>
  <div class="row" style="margin-bottom:8px">
    <div class="field"><label>From</label><input type="date" id="progress-from"></div>
    <div class="field">
      <label><input type="checkbox" id="progress-ef-filter" checked style="width:auto;margin-right:6px">Use endurance rides only for EF</label>
      <span class="hint">Hard interval rides usually have high IF, and heart rate on those sessions lags, spikes, and recovers less steadily. That makes EF noisier and less useful as a pure aerobic-base signal.</span>
    </div>
  </div>
  <div class="row" style="margin-bottom:12px">
    <button type="button" onclick="loadProgress(true)">Apply</button>
    <button type="button" class="secondary" onclick="resetProgressFilters()">Reset</button>
    <button type="button" id="progress-interpret-btn" onclick="interpretProgress()"><span class="spinner" id="progress-interpret-spin"></span>Interpret Trends</button>
  </div>
  <p class="hint" id="progress-period-hint" style="margin-bottom:8px"></p>
  <div class="result" id="progress-result"></div>
  <div id="progress-kpis" class="progress-grid"></div>
  <div id="progress-load-wrap" style="margin-top:18px">
    <p class="hint" style="margin-bottom:10px">Weekly load is shown for both the selected period and the immediately preceding comparison period.</p>
    <canvas id="chart-progress-load" height="220"></canvas>
  </div>
</div>
<div class="card">
  <h2>Saved Interpretation</h2>
  <p class="hint" id="progress-saved-meta" style="margin-bottom:12px">
    <span id="progress-saved-period">No saved interpretation yet.</span>
    <span id="progress-prompt-actions" style="display:none">
      <span style="margin:0 8px">|</span>
      <span class="icon-btn active" onclick="showProgressPrompt('system')" title="View system prompt">🧠</span>
      <span class="icon-btn active" onclick="showProgressPrompt('user')" title="View user prompt">📨</span>
    </span>
  </p>
  <div id="progress-analysis" class="progress-analysis" style="display:none"></div>
</div>
</div><!-- /tab-progress -->

<!-- ==================== LOGS TAB ==================== -->
<div class="tab-pane{{if eq .ActiveTab "logs"}} active{{end}}" id="tab-logs">
<div class="card">
  <h2>Live Logs &nbsp;<span id="log-dot" style="font-size:.75rem;font-weight:400;color:#6b7280">&#9679; connecting</span></h2>
  <div style="display:flex;gap:8px;margin-bottom:8px">
    <button class="secondary act-btn" onclick="clearLogs()">Clear</button>
    <button class="secondary act-btn" id="log-pause-btn" onclick="toggleLogPause()">Pause</button>
  </div>
  <pre id="log-panel" style="background:#1a1a2e;color:#e2e8f0;font-family:'Courier New',monospace;font-size:.78rem;padding:12px;border-radius:6px;height:300px;overflow-y:auto;white-space:pre-wrap;word-break:break-all;margin:0"></pre>
</div>
</div><!-- /tab-logs -->

<div class="modal-overlay" id="note-modal" onclick="if(event.target===this)closeNoteModal()">
  <div class="modal" style="max-width:600px">
    <h3>Athlete Notes</h3>
    <div style="margin-bottom:10px">
      <textarea id="note-add-text" rows="3" style="width:100%;padding:6px;border:1px solid #ccc;border-radius:4px;font-size:.9rem;font-family:inherit;resize:vertical" placeholder="Add a note for this workout/day..."></textarea>
      <div style="display:flex;gap:8px;margin-top:8px">
        <button onclick="createNote()">Add Note</button>
      </div>
    </div>
    <div id="note-modal-body"></div>
    <div class="result" id="note-modal-result" style="margin-bottom:8px"></div>
    <button class="secondary" onclick="closeNoteModal()">Close</button>
  </div>
</div>

<div class="modal-overlay" id="workout-data-modal" onclick="if(event.target===this)closeWorkoutDataModal()">
  <div class="modal" style="max-width:1100px;width:min(96vw,1100px)">
    <h3 id="workout-data-title">Workout Data</h3>
    <pre id="workout-data-body" style="background:#f8fafc;color:#111;font-family:'Courier New',monospace;font-size:.85rem;padding:12px;border-radius:6px;max-height:420px;overflow:auto;white-space:pre"></pre>
    <button class="secondary" onclick="closeWorkoutDataModal()">Close</button>
  </div>
</div>

<script>
var tabNames = ['actions','workouts','reports','wyze','body','progress','logs'];
function switchTab(name) {
  document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
  document.querySelectorAll('.tab-pane').forEach(p => p.classList.remove('active'));
  document.getElementById('tab-' + name).classList.add('active');
  var idx = tabNames.indexOf(name);
  if (idx >= 0) document.querySelectorAll('.tab')[idx].classList.add('active');
  var url = new URL(window.location);
  url.searchParams.set('tab', name);
  history.replaceState(null, '', url);
}

function showResult(id, ok, msg) {
  const el = document.getElementById(id);
  el.textContent = msg;
  el.className = 'result ' + (ok ? 'ok' : 'err');
  el.style.display = 'block';
}

function setLoading(btnId, spinId, loading) {
  const btn = document.getElementById(btnId);
  const spin = document.getElementById(spinId);
  btn.disabled = loading;
  spin.style.display = loading ? 'inline-block' : 'none';
}

function updateProcFields() {
  const mode = document.getElementById('proc-mode').value;
  document.getElementById('proc-from-field').style.display = mode === 'range' ? '' : 'none';
  document.getElementById('proc-to-field').style.display  = mode === 'range' ? '' : 'none';
  const hints = {
    new:   'Processes workouts that have not yet been analysed.',
    range: 'Re-processes workouts started in the selected date range.',
    all:   '⚠ Re-processes every workout. May take a while.'
  };
  document.getElementById('proc-hint').textContent = hints[mode];
}

async function runSync() {
  const from = document.getElementById('sync-from').value;
  const to   = document.getElementById('sync-to').value;
  const body = {};
  if (from) body.from = from;
  if (to)   body.to   = to;
  setLoading('sync-btn', 'sync-spin', true);
  try {
    const r = await fetch('/api/sync', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify(body)
    });
    const j = await r.json();
    if (!r.ok) { showResult('sync-result', false, JSON.stringify(j)); return; }
    const errs = j.errors && j.errors.length ? ' | errors: ' + j.errors.join(', ') : '';
    if (pendingResets.length > 0) {
      const ids = pendingResets.join(', ');
      pendingResets = [];
      showResult('sync-result', true,
        'FIT file(s) re-downloaded for: ' + ids + '. Inserted: ' + j.inserted + ' Skipped: ' + j.skipped +
        errs + ' — now click Process New to recompute metrics.');
    } else {
      showResult('sync-result', true, 'Inserted: ' + j.inserted + '  Skipped: ' + j.skipped + errs);
    }
  } catch(e) { showResult('sync-result', false, e.toString()); }
  finally { setLoading('sync-btn', 'sync-spin', false); }
}

async function runWyzeSync() {
  const from = document.getElementById('wyze-from').value;
  const to = document.getElementById('wyze-to').value;
  if (!from || !to) {
    showResult('wyze-sync-result', false, 'Please set both From and To dates.');
    return;
  }
  setLoading('wyze-sync-btn', 'wyze-sync-spin', true);
  try {
    const r = await fetch('/api/wyze/sync', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({from: from, to: to})
    });
    const text = await r.text();
    let j = {};
    try { j = JSON.parse(text); } catch (_) {}
    if (!r.ok) {
      showResult('wyze-sync-result', false, (text || 'Wyze sync failed.'));
      return;
    }
    showResult(
      'wyze-sync-result',
      true,
      'Inserted: ' + j.inserted + ' Updated: ' + j.updated + ' Skipped: ' + j.skipped +
      ' conflict_with_manual: ' + j.conflict_with_manual
    );
    await refreshWyzeConflicts();
    loadBodyCharts(true);
  } catch(e) {
    showResult('wyze-sync-result', false, e.toString());
  } finally {
    setLoading('wyze-sync-btn', 'wyze-sync-spin', false);
  }
}

async function deleteWyzeConflict(id, side) {
  if (!confirm('Delete the ' + side + ' entry for conflict #' + id + '?')) return;
  try {
    const r = await fetch('/api/wyze/conflicts/' + id + '?side=' + side, {method: 'DELETE'});
    const text = await r.text();
    if (!r.ok) {
      showResult('wyze-conflict-result', false, text || 'Delete failed.');
      return;
    }
    await refreshWyzeConflicts();
    showResult('wyze-conflict-result', true, 'Deleted ' + side + ' entry for conflict #' + id + '.');
  } catch(e) {
    showResult('wyze-conflict-result', false, e.toString());
  }
}

async function deleteWyzeRecord(id, source) {
  if (!confirm('Delete ' + source + ' record #' + id + '? This cannot be undone.')) return;
  try {
    const r = await fetch('/api/wyze/records/' + id + '?source=' + source, {method: 'DELETE'});
    const text = await r.text();
    if (!r.ok) {
      showResult('wyze-conflict-result', false, text || 'Delete failed.');
      return;
    }
    await refreshWyzeConflicts();
    loadBodyCharts(true);
    showResult('wyze-conflict-result', true, 'Deleted ' + source + ' record #' + id + '.');
  } catch(e) {
    showResult('wyze-conflict-result', false, e.toString());
  }
}

function fmtWyzeMetric(value, suffix, decimals) {
  if (value === null || value === undefined) return '<span style="color:#ccc">—</span>';
  return Number(value).toFixed(decimals) + suffix;
}

function renderWyzeConflicts(conflicts) {
  const tbody = document.getElementById('wyze-conflicts-body');
  if (!tbody) return;
  if (!conflicts || conflicts.length === 0) {
    tbody.innerHTML = '<tr id="wyze-conflicts-empty-row"><td colspan="7" style="color:#888;font-size:.9rem">No body-metric records found for this filter.</td></tr>';
    return;
  }

  tbody.innerHTML = conflicts.map(function(c) {
    const manual = c.manual || {};
    const wyze = c.wyze || {};
    const measuredAt = c.measured_at ? String(c.measured_at).slice(0, 10) : '—';
    return '<tr id="wyze-record-' + (c.note_id || 'x') + '">'
      + '<td>#' + (c.note_id || '—') + '</td>'
      + '<td style="white-space:nowrap">' + escHtml(measuredAt) + '</td>'
      + '<td><span class="badge ' + (c.source === 'wyze' ? 'green' : 'grey') + '">' + escHtml(c.source || 'manual') + '</span></td>'
      + '<td style="white-space:nowrap">' + (c.source === 'wyze' && c.wyze_record_id ? escHtml(c.wyze_record_id) + '<br>' : '') + '<span class="hint">note #' + (wyze.id || '—') + '</span></td>'
      + '<td>'
      + fmtWyzeMetric(wyze.weight_kg, ' kg', 1) + ' · '
      + fmtWyzeMetric(wyze.body_fat_pct, '% bf', 1) + ' · '
      + fmtWyzeMetric(wyze.muscle_mass_kg, ' kg muscle', 1) + ' · '
      + fmtWyzeMetric(wyze.body_water_pct, '% water', 1) + ' · '
      + fmtWyzeMetric(wyze.bmr_kcal, ' bmr', 0) + '</td>'
      + '<td>' + (c.conflict_id ? '<span class="badge yellow">#' + c.conflict_id + ' conflict</span>' : (manual.id ? '<span class="badge yellow">duplicate of #' + manual.id + '</span>' : '<span class="badge grey">none</span>')) + '</td>'
      + '<td>' + ((c.conflict_id || manual.id) ? '<button class="act-btn danger" onclick="deleteWyzeRecord(' + (c.note_id || '0') + ', \'' + escHtml(c.source || 'manual') + '\')">Delete</button>' : '<span style="color:#888">—</span>') + '</td>'
      + '</tr>';
  }).join('');
}

async function refreshWyzeConflicts() {
  try {
    const qs = new URLSearchParams();
    const from = document.querySelector('input[name="wyze_from"]');
    const to = document.querySelector('input[name="wyze_to"]');
    if (from && from.value) qs.set('from', from.value);
    if (to && to.value) qs.set('to', to.value);
    const r = await fetch('/api/wyze/records' + (qs.toString() ? '?' + qs.toString() : ''));
    const conflicts = await r.json();
    if (!r.ok) {
      showResult('wyze-conflict-result', false, 'Failed to refresh Wyze records.');
      return;
    }
    renderWyzeConflicts(conflicts);
  } catch (e) {
    showResult('wyze-conflict-result', false, e.toString());
  }
}

async function runProcess() {
  const mode  = document.getElementById('proc-mode').value;
  const from  = document.getElementById('proc-from').value;
  const to    = document.getElementById('proc-to').value;
  const body  = {};
  if (mode === 'range') {
    if (!from || !to) { showResult('proc-result', false, 'Please set both From and To dates.'); return; }
    body.from = from;
    body.to   = to;
  } else if (mode === 'all') {
    body.reprocess_all = true;
  }
  setLoading('proc-btn', 'proc-spin', true);
  try {
    const r = await fetch('/api/process', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify(body)
    });
    const j = await r.json();
    if (!r.ok) { showResult('proc-result', false, JSON.stringify(j)); return; }

    let msg = 'Processed: ' + j.processed + '  |  No FIT file: ' + j.skipped_no_fit;
    const el = document.getElementById('proc-result');

    if (j.parse_errors && j.parse_errors.length) {
      msg += '  |  Parse errors: ' + j.parse_errors.length;
      el.innerHTML = msg + '<br><br><strong>Corrupt FIT files — reset to re-download, or ignore if permanently corrupt:</strong><ul style="margin:8px 0 0 16px">'
        + j.parse_errors.map(e =>
            '<li style="margin-bottom:6px">' + e.wahoo_id + ': ' + e.error.split(':').pop().trim()
            + ' &nbsp;<button class="act-btn danger" onclick="resetFIT(\'' + e.wahoo_id + '\')">Reset FIT</button>'
            + ' &nbsp;<button class="act-btn secondary" onclick="ignoreFIT(\'' + e.wahoo_id + '\', this)">Ignore</button></li>'
          ).join('')
        + '</ul>';
      el.className = 'result err';
      el.style.display = 'block';
    } else {
      const dbErrs = j.errors && j.errors.length ? '  |  DB errors: ' + j.errors.length : '';
      showResult('proc-result', true, msg + dbErrs);
    }
  } catch(e) { showResult('proc-result', false, e.toString()); }
  finally { setLoading('proc-btn', 'proc-spin', false); }
}

var pendingResets = [];
async function resetFIT(wahooId) {
  try {
    const r = await fetch('/api/workout/reset-fit', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({wahoo_id: wahooId})
    });
    const j = await r.json();
    if (!r.ok) { alert('Reset failed for ' + wahooId); return; }
    pendingResets.push(wahooId);
    if (j.started_at) {
      document.getElementById('sync-from').value = j.started_at;
      document.getElementById('sync-to').value   = j.started_at;
      showResult('sync-result', true,
        'Ready to re-sync ' + wahooId + ' (' + j.started_at + '). ' +
        'Inserted will be 0 — that is expected, only the FIT file is re-downloaded. ' +
        'Click Sync Now, then Process New.');
    } else {
      alert('Reset OK for ' + wahooId + '. Re-sync and process to recompute metrics.');
    }
  } catch(e) { alert(e.toString()); }
}

async function runCloseBlock() {
  const blockEnd = document.getElementById('close-block-end').value;
  const startField = document.getElementById('close-block-start');
  const initialBlockStart = startField.disabled ? '' : startField.value;
  if (!blockEnd) { showResult('close-block-result', false, 'Please choose a block end date.'); return; }
  setLoading('close-block-btn', 'close-block-spin', true);
  showResult('close-block-result', true, 'Generating the closing report and the next plan. This may take a little while...');
  try {
    const body = {block_end: blockEnd};
    if (initialBlockStart) body.initial_block_start = initialBlockStart;
    const up = document.getElementById('close-block-prompt').value.trim();
    if (up) body.user_prompt = up;
    const r = await fetch('/api/report/close-block', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify(body)
    });
    const text = await r.text();
    let j = {};
    try { j = JSON.parse(text); } catch (_) {}
    if (!r.ok) {
      showResult('close-block-result', false, j.error || text || 'Close block flow failed.');
      return;
    }
    showResult('close-block-result', true,
      'Closed block ' + j.block_start + ' to ' + j.block_end +
      ' and created the next plan for ' + j.plan_start + ' to ' + j.plan_end +
      '. Reload page to see both entries.');
  } catch(e) {
    showResult('close-block-result', false, e.toString());
  } finally {
    setLoading('close-block-btn', 'close-block-spin', false);
  }
}

async function sendReport(id) {
  try {
    const r = await fetch('/api/report/send', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({report_id: id})
    });
    const j = await r.json();
    if (!r.ok) { showResult('send-result', false, 'Report #' + id + ': ' + (j.error || JSON.stringify(j))); return; }
    showResult('send-result', true, 'Report #' + id + ' sent. Reload to see updated status.');
  } catch(e) { showResult('send-result', false, e.toString()); }
}

async function deleteReport(id) {
  if (!confirm('Delete report #' + id + '? This cannot be undone.')) return;
  try {
    const r = await fetch('/api/report/' + id, {method: 'DELETE'});
    if (!r.ok) { showResult('send-result', false, 'Delete failed for report #' + id); return; }
    const cell = document.getElementById('cell-' + id);
    if (cell) cell.innerHTML = '<span style="color:#bbb">—</span>';
    showResult('send-result', true, 'Report #' + id + ' deleted.');
  } catch(e) { showResult('send-result', false, e.toString()); }
}

async function showReportPrompt(id, kind) {
  var title = document.getElementById('workout-data-title');
  var body = document.getElementById('workout-data-body');
  body.style.whiteSpace = 'pre-wrap';
  body.style.wordBreak = 'break-word';
  body.style.overflowWrap = 'anywhere';
  title.textContent = kind === 'system' ? 'Report / Plan System Prompt' : 'Report / Plan User Prompt';
  body.textContent = 'Loading...';
  document.getElementById('workout-data-modal').classList.add('show');
  try {
    var r = await fetch('/api/report/' + id + '/prompts');
    if (!r.ok) {
      body.textContent = 'Failed to load saved prompts.';
      return;
    }
    var j = await r.json();
    var prompt = kind === 'system' ? j.system_prompt : j.user_prompt;
    body.textContent = prompt || 'No saved ' + kind + ' prompt for this report.';
  } catch(e) {
    body.textContent = e.toString();
  }
}

async function ignoreFIT(wahooId, btn) {
  if (!confirm('Mark ' + wahooId + ' as ignored? The workout stays in the DB but will never be processed again. Use only when the FIT file is permanently corrupt on the server.')) return;
  btn.disabled = true;
  try {
    const r = await fetch('/api/workout/ignore', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({wahoo_id: wahooId})
    });
    if (!r.ok) { alert('Ignore failed for ' + wahooId); btn.disabled = false; return; }
    // Remove the whole list item from the parse errors UI.
    const li = btn.closest('li');
    if (li) li.remove();
  } catch(e) { alert(e.toString()); btn.disabled = false; }
}

async function runEvolveProfile() {
  const n = parseInt(document.getElementById('evolve-n').value) || 8;
  setLoading('evolve-btn', 'evolve-spin', true);
  showResult('evolve-result', true, 'Calling Claude API — this may take ~30 seconds...');
  try {
    const r = await fetch('/api/profile/evolve', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({last_n: n})
    });
    const j = await r.json();
    if (r.status === 422) {
      showResult('evolve-result', false,
        'Validation failed: ' + j.reason + '\n\n' +
        'Claude\'s output was saved as "' + j.rejected + '" in the profile directory. ' +
        'Review it, fix the missing section, then copy it over the live profile manually.');
      return;
    }
    if (!r.ok) { showResult('evolve-result', false, JSON.stringify(j)); return; }
    showResult('evolve-result', true, 'Profile updated. Old profile backed up as: ' + j.backup);
  } catch(e) { showResult('evolve-result', false, e.toString()); }
  finally { setLoading('evolve-btn', 'evolve-spin', false); }
}

// --- note modal (per-workout, with edit/delete) ---
var currentNoteWorkoutId = null;
function closeNoteModal() {
  document.getElementById('note-modal').classList.remove('show');
  document.getElementById('note-modal-result').style.display = 'none';
}

function closeWorkoutDataModal() {
  document.getElementById('workout-data-modal').classList.remove('show');
}

async function showWorkoutData(workoutId, kind) {
  var title = document.getElementById('workout-data-title');
  var body = document.getElementById('workout-data-body');
  body.style.whiteSpace = 'pre';
  body.style.wordBreak = 'normal';
  body.style.overflowWrap = 'normal';
  title.textContent = kind === 'summary' ? 'Workout Summary Row' : 'Per-Ride Zone Detail';
  body.textContent = 'Loading...';
  document.getElementById('workout-data-modal').classList.add('show');
  try {
    var r = await fetch('/api/workouts/' + workoutId + '/data');
    if (!r.ok) {
      body.textContent = 'Failed to load workout data.';
      return;
    }
    var j = await r.json();
    var text = kind === 'summary' ? j.summary_table : j.zone_detail;
    body.textContent = text || 'No data available for this workout yet.';
  } catch(e) {
    body.textContent = e.toString();
  }
}

var currentNoteType = null;
async function showNotes(workoutId, noteType) {
  currentNoteWorkoutId = workoutId;
  currentNoteType = noteType;
  document.getElementById('note-add-text').value = '';
  var body = document.getElementById('note-modal-body');
  var title = document.querySelector('#note-modal .modal h3');
  title.textContent = noteType === 'ride' ? 'Ride Notes' : 'General Notes';
  body.innerHTML = '<p style="color:#888">Loading...</p>';
  document.getElementById('note-modal-result').style.display = 'none';
  document.getElementById('note-modal').classList.add('show');
  try {
    var r = await fetch('/api/notes?workout_id=' + workoutId + '&type=' + noteType);
    var notes = await r.json();
    if (!notes || notes.length === 0) {
      body.innerHTML = '<p style="color:#888">No notes found.</p>';
      return;
    }
    body.innerHTML = notes.map(function(n) {
      return '<div class="note-item" id="note-item-' + n.id + '" style="padding:10px 0;border-bottom:1px solid #eee">'
        + '<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:4px">'
        + '<span style="font-size:.8rem;color:#888">' + n.timestamp + ' [' + n.type + ']'
        + (n.rpe ? ' RPE=' + n.rpe : '') + '</span>'
        + '<span>'
        + '<button class="act-btn secondary" onclick="editNote(' + n.id + ')">Edit</button> '
        + '<button class="act-btn danger" onclick="deleteNote(' + n.id + ')">Delete</button>'
        + '</span></div>'
        + '<div id="note-text-' + n.id + '">'
        + '<p style="margin:0;font-size:.9rem;white-space:pre-wrap">' + escHtml(n.note || '') + '</p>'
        + '</div>'
        + '<input type="hidden" id="note-type-' + n.id + '" value="' + n.type + '">'
        + '<input type="hidden" id="note-rpe-' + n.id + '" value="' + (n.rpe || '') + '">'
        + '<input type="hidden" id="note-weight-' + n.id + '" value="' + (n.weight_kg || '') + '">'
        + '<input type="hidden" id="note-bf-' + n.id + '" value="' + (n.body_fat_pct || '') + '">'
        + '<input type="hidden" id="note-mm-' + n.id + '" value="' + (n.muscle_mass_kg || '') + '">'
        + '</div>';
    }).join('');
  } catch(e) { body.innerHTML = '<p style="color:#991b1b">Failed to load notes.</p>'; }
}

async function createNote() {
  var text = document.getElementById('note-add-text').value.trim();
  if (!text) {
    showResult('note-modal-result', false, 'Enter a note first.');
    return;
  }
  try {
    var r = await fetch('/api/notes', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({
        type: currentNoteType,
        note: text,
        workout_id: currentNoteWorkoutId
      })
    });
    if (!r.ok) { showResult('note-modal-result', false, 'Create failed.'); return; }
    document.getElementById('note-add-text').value = '';
    showNotes(currentNoteWorkoutId, currentNoteType);
  } catch(e) { showResult('note-modal-result', false, e.toString()); }
}

function escHtml(s) {
  var d = document.createElement('div'); d.textContent = s; return d.innerHTML;
}

function editNote(id) {
  var container = document.getElementById('note-text-' + id);
  var type = document.getElementById('note-type-' + id).value;
  var rpe = document.getElementById('note-rpe-' + id).value;
  var text = container.querySelector('p') ? container.querySelector('p').textContent : '';
  container.innerHTML =
    '<textarea id="note-edit-' + id + '" rows="3" style="width:100%;padding:6px;border:1px solid #ccc;border-radius:4px;font-size:.9rem;font-family:inherit;resize:vertical">' + escHtml(text) + '</textarea>'
    + '<div style="display:flex;gap:6px;margin-top:6px">'
    + '<button class="act-btn" onclick="saveNote(' + id + ')">Save</button>'
    + '<button class="act-btn secondary" onclick="showNotes(' + currentNoteWorkoutId + ',\'' + currentNoteType + '\')">Cancel</button>'
    + '</div>';
  document.getElementById('note-edit-' + id).focus();
}

async function saveNote(id) {
  var text = document.getElementById('note-edit-' + id).value;
  var type = document.getElementById('note-type-' + id).value;
  var rpe = document.getElementById('note-rpe-' + id).value;
  var weight = document.getElementById('note-weight-' + id).value;
  var bf = document.getElementById('note-bf-' + id).value;
  var mm = document.getElementById('note-mm-' + id).value;
  var body = {type: type, note: text};
  if (rpe) body.rpe = parseInt(rpe);
  if (weight) body.weight_kg = parseFloat(weight);
  if (bf) body.body_fat_pct = parseFloat(bf);
  if (mm) body.muscle_mass_kg = parseFloat(mm);
  try {
    var r = await fetch('/api/notes/' + id, {
      method: 'PUT',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify(body)
    });
    if (!r.ok) { showResult('note-modal-result', false, 'Save failed.'); return; }
    showNotes(currentNoteWorkoutId, currentNoteType);
  } catch(e) { showResult('note-modal-result', false, e.toString()); }
}

async function deleteNote(id) {
  if (!confirm('Delete this note? This cannot be undone.')) return;
  try {
    var r = await fetch('/api/notes/' + id, {method: 'DELETE'});
    if (!r.ok) { showResult('note-modal-result', false, 'Delete failed.'); return; }
    var el = document.getElementById('note-item-' + id);
    if (el) el.remove();
    var remaining = document.getElementById('note-modal-body').querySelectorAll('.note-item');
    if (remaining.length === 0) {
      document.getElementById('note-modal-body').innerHTML = '<p style="color:#888">All notes deleted.</p>';
    }
  } catch(e) { showResult('note-modal-result', false, e.toString()); }
}

// --- body metrics charts ---
var bodyChartsLoaded = false;
function loadBodyCharts(forceReload) {
  if (bodyChartsLoaded && !forceReload) return;
  bodyChartsLoaded = true;

  var from = document.getElementById('body-from').value;
  var to = document.getElementById('body-to').value;
  var qs = new URLSearchParams();
  if (from) qs.set('from', from);
  if (to) qs.set('to', to);
  var url = '/api/body-metrics' + (qs.toString() ? '?' + qs.toString() : '');

  document.getElementById('chart-weight').style.display = '';
  document.getElementById('chart-bodyfat').style.display = '';
  document.getElementById('chart-muscle').style.display = '';
  document.getElementById('chart-water').style.display = '';
  document.getElementById('chart-bmr').style.display = '';
  document.getElementById('body-charts').style.display = '';
  document.getElementById('body-empty').style.display = 'none';

  fetch(url).then(r => r.json()).then(function(data) {
    if (!data || data.length === 0) {
      document.getElementById('body-charts').style.display = 'none';
      document.getElementById('body-empty').style.display = 'block';
      return;
    }
    var weightData = data.filter(d => d.weight_kg);
    var bfData = data.filter(d => d.body_fat_pct);
    var mmData = data.filter(d => d.muscle_mass_kg);
    var waterData = data.filter(d => d.body_water_pct);
    var bmrData = data.filter(d => d.bmr_kcal);

    if (weightData.length) drawChart('chart-weight', 'Weight (kg)', weightData.map(d => d.date), weightData.map(d => d.weight_kg), '#2563eb');
    else document.getElementById('chart-weight').style.display = 'none';

    if (bfData.length) drawChart('chart-bodyfat', 'Body Fat (%)', bfData.map(d => d.date), bfData.map(d => d.body_fat_pct), '#f59e0b');
    else document.getElementById('chart-bodyfat').style.display = 'none';

    if (mmData.length) drawChart('chart-muscle', 'Muscle Mass (kg)', mmData.map(d => d.date), mmData.map(d => d.muscle_mass_kg), '#22c55e');
    else document.getElementById('chart-muscle').style.display = 'none';

    if (waterData.length) drawChart('chart-water', 'Hydration / Body Water (%)', waterData.map(d => d.date), waterData.map(d => d.body_water_pct), '#0891b2');
    else document.getElementById('chart-water').style.display = 'none';

    if (bmrData.length) drawChart('chart-bmr', 'BMR (kcal)', bmrData.map(d => d.date), bmrData.map(d => d.bmr_kcal), '#a16207');
    else document.getElementById('chart-bmr').style.display = 'none';
  });
}

function applyBodyFilters() {
  loadBodyCharts(true);
}

function resetBodyFilters() {
  document.getElementById('body-from').value = '';
  document.getElementById('body-to').value = '';
  loadBodyCharts(true);
}

function drawChart(canvasId, label, labels, values, color) {
  var canvas = document.getElementById(canvasId);
  var ctx = canvas.getContext('2d');
  var W = canvas.parentElement.offsetWidth - 2;
  var H = 200;
  canvas.width = W; canvas.height = H;
  var pad = {top:30, right:20, bottom:30, left:55};
  var cw = W - pad.left - pad.right;
  var ch = H - pad.top - pad.bottom;

  var min = Math.min.apply(null, values);
  var max = Math.max.apply(null, values);
  var range = max - min || 1;
  min -= range * 0.1; max += range * 0.1; range = max - min;

  ctx.clearRect(0, 0, W, H);

  // Title
  ctx.fillStyle = '#555'; ctx.font = '600 13px system-ui'; ctx.fillText(label, pad.left, 18);

  // Grid
  ctx.strokeStyle = '#e5e7eb'; ctx.lineWidth = 1;
  for (var i = 0; i <= 4; i++) {
    var y = pad.top + ch - (i / 4) * ch;
    ctx.beginPath(); ctx.moveTo(pad.left, y); ctx.lineTo(W - pad.right, y); ctx.stroke();
    ctx.fillStyle = '#999'; ctx.font = '11px system-ui';
    ctx.fillText((min + (i / 4) * range).toFixed(1), 4, y + 4);
  }

  // X labels
  ctx.fillStyle = '#999'; ctx.font = '11px system-ui';
  var step = Math.max(1, Math.floor(labels.length / 6));
  for (var i = 0; i < labels.length; i += step) {
    var x = pad.left + (i / (labels.length - 1 || 1)) * cw;
    ctx.fillText(formatShortDate(labels[i]), x - 12, H - 8);
  }

  // Line + dots
  ctx.strokeStyle = color; ctx.lineWidth = 2; ctx.beginPath();
  for (var i = 0; i < values.length; i++) {
    var x = pad.left + (i / (values.length - 1 || 1)) * cw;
    var y = pad.top + ch - ((values[i] - min) / range) * ch;
    if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
  }
  ctx.stroke();

  ctx.fillStyle = color;
  for (var i = 0; i < values.length; i++) {
    var x = pad.left + (i / (values.length - 1 || 1)) * cw;
    var y = pad.top + ch - ((values[i] - min) / range) * ch;
    ctx.beginPath(); ctx.arc(x, y, 3, 0, Math.PI * 2); ctx.fill();
  }
}

function formatShortDate(dateStr) {
  if (!dateStr || dateStr.length < 10) return dateStr;
  return dateStr.slice(8,10) + '-' + dateStr.slice(5,7);
}

// Auto-load charts when body tab is shown.
var origSwitchTab = switchTab;
switchTab = function(name) {
  origSwitchTab(name);
  if (name === 'body') loadBodyCharts();
  if (name === 'progress') loadProgress();
};
if (tabNames.indexOf('body') >= 0 && document.getElementById('tab-body').classList.contains('active')) {
  loadBodyCharts();
}
if (tabNames.indexOf('progress') >= 0 && document.getElementById('tab-progress').classList.contains('active')) {
  loadProgress();
}

// --- progress tab ---
var progressLoaded = false;
var currentSavedProgressAnalysis = null;
function defaultProgressFrom() {
  var d = new Date();
  d.setDate(d.getDate() - 55);
  return d.toISOString().slice(0, 10);
}

function loadProgress(forceReload) {
  if (progressLoaded && !forceReload) return;
  progressLoaded = true;
  if (!document.getElementById('progress-from').value) {
    document.getElementById('progress-from').value = defaultProgressFrom();
  }
  var from = document.getElementById('progress-from').value;
  var aerobicOnly = document.getElementById('progress-ef-filter').checked;
  var qs = new URLSearchParams();
  qs.set('from', from);
  if (!aerobicOnly) qs.set('aerobic_only_ef', '0');
  fetch('/api/progress?' + qs.toString()).then(function(r) {
    if (!r.ok) throw new Error('Failed to load progress data.');
    return r.json();
  }).then(renderProgress).catch(function(e) {
    showResult('progress-result', false, e.toString());
  });
}

function resetProgressFilters() {
  document.getElementById('progress-from').value = defaultProgressFrom();
  document.getElementById('progress-ef-filter').checked = true;
  loadProgress(true);
}

function renderProgress(data) {
  document.getElementById('progress-period-hint').textContent =
    'Selected period: ' + data.selected_range.from + ' to ' + data.selected_range.to +
    ' (' + data.selected_range.days + ' days). Calculated symmetric previous period for comparison: ' +
    data.prior_range.from + ' to ' + data.prior_range.to + '.';

  var kpiWrap = document.getElementById('progress-kpis');
  kpiWrap.innerHTML = (data.kpis || []).map(function(kpi) {
    var arrow = kpi.trend === 'up' ? '↑' : (kpi.trend === 'down' ? '↓' : '→');
    var trendClass = 'trend-' + kpi.trend;
    var emptyNote = '';
    if (kpi.key === 'endurance_durability' && (kpi.current === null || kpi.current === undefined)) {
      emptyNote = '<div class="empty-note">For this KPI, decoupling is only averaged from rides longer than 90 minutes. No qualifying long rides in selected period.</div>';
    }
    if (kpi.key === 'average_weight_kg' && ((kpi.current === null || kpi.current === undefined) || (kpi.prior === null || kpi.prior === undefined))) {
      emptyNote = '<div class="empty-note">Average weight is only shown when both the selected period and the prior period have at least 3 recorded weight entries.</div>';
    }
    return '<div class="progress-kpi">'
      + '<h3>' + escHtml(kpi.title) + '</h3>'
      + '<div class="explain">' + escHtml(kpi.explanation) + '</div>'
      + '<div class="metric-label">Value</div>'
      + '<div class="value">' + formatProgressValue(kpi.key, kpi.current) + '</div>'
      + '<div class="metric-label">Trend</div>'
      + '<div class="trend-text ' + trendClass + '">' + arrow + ' ' + formatTrendText(kpi.trend) + '</div>'
      + '<div class="metric-label">Comparison</div>'
      + '<div class="delta">Prior: ' + formatProgressValue(kpi.key, kpi.prior)
      + ' · Change: ' + formatProgressDelta(kpi.key, kpi.delta, kpi.delta_pct) + '</div>'
      + emptyNote
      + '</div>';
  }).join('');

  if ((data.weekly_load && data.weekly_load.length) || (data.prior_weekly_load && data.prior_weekly_load.length)) {
    drawComparisonLoadChart('chart-progress-load', data.weekly_load || [], data.prior_weekly_load || []);
    document.getElementById('progress-load-wrap').style.display = '';
  } else {
    document.getElementById('progress-load-wrap').style.display = 'none';
  }

  if (data.saved_analysis) {
    currentSavedProgressAnalysis = data.saved_analysis;
    document.getElementById('progress-saved-period').textContent =
      'Saved interpretation period: ' + data.saved_analysis.from + ' to ' + data.saved_analysis.to +
      ' · updated ' + data.saved_analysis.updated_at.replace('T', ' ').slice(0, 16);
    document.getElementById('progress-prompt-actions').style.display = '';
    var analysis = document.getElementById('progress-analysis');
    analysis.innerHTML = data.saved_analysis.html || '';
    analysis.style.display = '';
  } else {
    currentSavedProgressAnalysis = null;
    document.getElementById('progress-saved-period').textContent = 'No saved interpretation yet.';
    document.getElementById('progress-prompt-actions').style.display = 'none';
    document.getElementById('progress-analysis').style.display = 'none';
    document.getElementById('progress-analysis').innerHTML = '';
  }
}

function showProgressPrompt(kind) {
  if (!currentSavedProgressAnalysis) return;
  var title = document.getElementById('workout-data-title');
  var body = document.getElementById('workout-data-body');
  body.style.whiteSpace = 'pre-wrap';
  body.style.wordBreak = 'break-word';
  body.style.overflowWrap = 'anywhere';
  if (kind === 'system') {
    title.textContent = 'Progress Interpretation System Prompt';
    body.textContent = currentSavedProgressAnalysis.system_prompt || 'No system prompt saved.';
  } else {
    title.textContent = 'Progress Interpretation User Prompt';
    body.textContent = currentSavedProgressAnalysis.user_prompt || 'No user prompt saved.';
  }
  document.getElementById('workout-data-modal').classList.add('show');
}

async function interpretProgress() {
  var from = document.getElementById('progress-from').value;
  if (!from) {
    showResult('progress-result', false, 'Please choose a From date first.');
    return;
  }
  setLoading('progress-interpret-btn', 'progress-interpret-spin', true);
  showResult('progress-result', true, 'Calling Claude API — this may take ~30 seconds...');
  try {
    var r = await fetch('/api/progress/interpret', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({
        from: from,
        aerobic_only_ef: document.getElementById('progress-ef-filter').checked
      })
    });
    var text = await r.text();
    var j = {};
    try { j = JSON.parse(text); } catch (_) {}
    if (!r.ok) {
      showResult('progress-result', false, (j.error || text || 'Interpretation failed.'));
      return;
    }
    showResult('progress-result', true, 'Interpretation saved for ' + j.from + ' to ' + j.to + '.');
    loadProgress(true);
  } catch(e) {
    showResult('progress-result', false, e.toString());
  } finally {
    setLoading('progress-interpret-btn', 'progress-interpret-spin', false);
  }
}

function formatProgressValue(key, value) {
  if (value === null || value === undefined) return '—';
  if (key === 'active_calories') return value.toFixed(0) + ' kcal';
  if (key === 'completion_rate') return value.toFixed(1) + '%';
  if (key === 'average_intensity_factor' || key === 'aerobic_efficiency' || key === 'average_weight_kg') return value.toFixed(2);
  if (key === 'endurance_durability') return value.toFixed(1) + '%';
  return value.toFixed(0);
}

function formatProgressDelta(key, delta, deltaPct) {
  if ((key === 'endurance_durability' || key === 'completion_rate') && delta !== null && delta !== undefined) {
    return (delta > 0 ? '+' : '') + delta.toFixed(1) + ' pp';
  }
  if (deltaPct !== null && deltaPct !== undefined) {
    if (key === 'average_weight_kg' || key === 'average_intensity_factor' || key === 'aerobic_efficiency') {
      var pct = deltaPct * 100;
      return (pct > 0 ? '+' : '') + pct.toFixed(1) + '%';
    }
    var pct = deltaPct * 100;
    return (pct > 0 ? '+' : '') + pct.toFixed(1) + '%';
  }
  if (delta === null || delta === undefined) return '—';
  if (key === 'active_calories') return (delta > 0 ? '+' : '') + delta.toFixed(0) + ' kcal';
  if (key === 'average_intensity_factor' || key === 'aerobic_efficiency' || key === 'average_weight_kg') return (delta > 0 ? '+' : '') + delta.toFixed(2);
  return (delta > 0 ? '+' : '') + delta.toFixed(0);
}

function formatTrendText(trend) {
  if (trend === 'up') return 'Up';
  if (trend === 'down') return 'Down';
  return 'Steady';
}

function drawComparisonLoadChart(canvasId, currentSeries, priorSeries) {
  var canvas = document.getElementById(canvasId);
  var ctx = canvas.getContext('2d');
  var W = canvas.parentElement.offsetWidth - 2;
  var H = 220;
  canvas.width = W; canvas.height = H;
  var pad = {top:30, right:20, bottom:30, left:55};
  var cw = W - pad.left - pad.right;
  var ch = H - pad.top - pad.bottom;
  var labels = [];
  var series = [];
  if (currentSeries.length) {
    labels = currentSeries.map(function(d) { return d.week_start; });
    series.push({label: 'Selected TSS', color: '#2563eb', values: currentSeries.map(function(d) { return d.tss; })});
    series.push({label: 'Selected TRIMP', color: '#dc2626', values: currentSeries.map(function(d) { return d.trimp; })});
  }
  if (priorSeries.length) {
    if (!labels.length) labels = priorSeries.map(function(d) { return d.week_start; });
    series.push({label: 'Prior TSS', color: '#93c5fd', values: priorSeries.map(function(d) { return d.tss; })});
    series.push({label: 'Prior TRIMP', color: '#fca5a5', values: priorSeries.map(function(d) { return d.trimp; })});
  }
  var allValues = [];
  series.forEach(function(s) { allValues = allValues.concat(s.values); });
  var min = Math.min.apply(null, allValues);
  var max = Math.max.apply(null, allValues);
  var range = max - min || 1;
  min -= range * 0.1; max += range * 0.1; range = max - min;
  ctx.clearRect(0, 0, W, H);
  ctx.fillStyle = '#555'; ctx.font = '600 13px system-ui'; ctx.fillText('Weekly Load Comparison', pad.left, 18);
  ctx.strokeStyle = '#e5e7eb'; ctx.lineWidth = 1;
  for (var i = 0; i <= 4; i++) {
    var y = pad.top + ch - (i / 4) * ch;
    ctx.beginPath(); ctx.moveTo(pad.left, y); ctx.lineTo(W - pad.right, y); ctx.stroke();
    ctx.fillStyle = '#999'; ctx.font = '11px system-ui';
    ctx.fillText((min + (i / 4) * range).toFixed(0), 4, y + 4);
  }
  ctx.fillStyle = '#999'; ctx.font = '11px system-ui';
  for (var j = 0; j < labels.length; j++) {
    var x = pad.left + (j / (labels.length - 1 || 1)) * cw;
    ctx.fillText(formatShortDate(labels[j]), x - 18, H - 8);
  }
  series.forEach(function(s) {
    ctx.strokeStyle = s.color; ctx.lineWidth = 2; ctx.beginPath();
    for (var i = 0; i < s.values.length; i++) {
      var x = pad.left + (i / (s.values.length - 1 || 1)) * cw;
      var y = pad.top + ch - ((s.values[i] - min) / range) * ch;
      if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
    }
    ctx.stroke();
  });
  var legendX = W - pad.right - 130;
  series.forEach(function(s, idx) {
    ctx.fillStyle = s.color;
    ctx.fillRect(legendX, 14 + idx * 16, 10, 10);
    ctx.fillStyle = '#555';
    ctx.font = '11px system-ui';
    ctx.fillText(s.label, legendX + 16, 23 + idx * 16);
  });
}

// --- live log stream ---
var logPaused = false;
var logEs = null;
function initLogStream() {
  var dot = document.getElementById('log-dot');
  if (logEs) { logEs.close(); logEs = null; }
  dot.textContent = '\u25CF connecting'; dot.style.color = '#6b7280';
  logEs = new EventSource('/api/logs/stream');
  logEs.onopen = function() { dot.textContent = '\u25CF live'; dot.style.color = '#22c55e'; };
  logEs.onerror = function() { dot.textContent = '\u25CF reconnecting'; dot.style.color = '#f59e0b'; };
  logEs.onmessage = function(e) {
    if (logPaused) return;
    var panel = document.getElementById('log-panel');
    var line = e.data;
    var span = document.createElement('span');
    span.textContent = line + '\n';
    if (line.indexOf(' ERROR') !== -1) span.style.color = '#f87171';
    else if (line.indexOf(' WARN ') !== -1) span.style.color = '#fbbf24';
    else if (line.indexOf(' DEBUG') !== -1) span.style.color = '#94a3b8';
    panel.appendChild(span);
    while (panel.childNodes.length > 400) panel.removeChild(panel.firstChild);
    panel.scrollTop = panel.scrollHeight;
  };
}
function clearLogs() { document.getElementById('log-panel').innerHTML = ''; }
function toggleLogPause() {
  logPaused = !logPaused;
  document.getElementById('log-pause-btn').textContent = logPaused ? 'Resume' : 'Pause';
}
initLogStream();
</script>
</body>
</html>
`))

// reportPeriodRow groups a weekly_plan and the weekly_report covering the same
// [WeekStart, WeekEnd] window, so the admin page can show plan vs. report
// side-by-side for each period.
type reportPeriodRow struct {
	Start  time.Time
	End    time.Time
	Plan   *storage.ReportWithDelivery
	Report *storage.ReportWithDelivery
}

// groupReportsByPeriod buckets reports by week_start into one row per period,
// splitting plan and report into separate fields. The plan and the matching
// report share the same week_start by construction (the report covers the block
// the plan prescribed), but their week_end may differ when execution extended
// past the planned window — in that case the row's End is taken from the report
// since the report reflects the actual block. Rows are sorted by Start
// descending (most recent period first).
func groupReportsByPeriod(reports []storage.ReportWithDelivery) []reportPeriodRow {
	index := map[string]*reportPeriodRow{}
	rows := []*reportPeriodRow{}
	for i := range reports {
		r := reports[i]
		k := r.WeekStart.Format("2006-01-02")
		row, ok := index[k]
		if !ok {
			row = &reportPeriodRow{Start: r.WeekStart, End: r.WeekEnd}
			index[k] = row
			rows = append(rows, row)
		}
		entry := r
		switch r.Type {
		case storage.ReportTypeWeeklyPlan:
			row.Plan = &entry
		case storage.ReportTypeWeeklyReport:
			row.Report = &entry
			// Report's window reflects the actual block; prefer it over the plan's
			// prescription window when both are present.
			row.End = r.WeekEnd
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Start.After(rows[j].Start)
	})
	out := make([]reportPeriodRow, len(rows))
	for i, r := range rows {
		out[i] = *r
	}
	return out
}

type adminPageData struct {
	Reports                 []storage.ReportWithDelivery
	ReportPeriods           []reportPeriodRow
	Workouts                []storage.WorkoutWithMetrics
	WyzeRecords             []storage.BodyMetricRecordDetail
	FilterFrom              string
	FilterTo                string
	WyzeFilterFrom          string
	WyzeFilterTo            string
	ActiveTab               string // "actions", "workouts", "reports"
	HasPreviousClosedReport bool
	InferredBlockStart      string
	PreviousBlockStart      string
	PreviousBlockEnd        string
	CurrentDate             string
	CurrentIntervalText     string
}

func adminHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var from, to time.Time
		var wyzeFrom, wyzeTo time.Time

		fromStr := r.URL.Query().Get("from")
		toStr := r.URL.Query().Get("to")
		wyzeFromStr := r.URL.Query().Get("wyze_from")
		wyzeToStr := r.URL.Query().Get("wyze_to")

		if fromStr != "" {
			if t, err := time.Parse("2006-01-02", fromStr); err == nil {
				from = t
			}
		}
		if toStr != "" {
			if t, err := time.Parse("2006-01-02", toStr); err == nil {
				to = t
			}
		}
		if wyzeFromStr != "" {
			if t, err := time.Parse("2006-01-02", wyzeFromStr); err == nil {
				wyzeFrom = t
			}
		}
		if wyzeToStr != "" {
			if t, err := time.Parse("2006-01-02", wyzeToStr); err == nil {
				wyzeTo = t.Add(24*time.Hour - time.Nanosecond)
			}
		}

		// Default: last 8 weeks when no filter is provided.
		filterFrom := fromStr
		filterTo := toStr
		if from.IsZero() && to.IsZero() {
			from = time.Now().UTC().AddDate(0, 0, -adminDefaultWeeks*7)
			filterFrom = from.Format("2006-01-02")
		}
		wyzeFilterFrom := wyzeFromStr
		wyzeFilterTo := wyzeToStr
		if wyzeFrom.IsZero() && wyzeTo.IsZero() {
			wyzeFrom = time.Now().UTC().AddDate(0, 0, -adminDefaultWeeks*7)
			wyzeFilterFrom = wyzeFrom.Format("2006-01-02")
		}

		reports, err := storage.ListReportsWithDelivery(db, from, to, 100)
		if err != nil {
			slog.Error("adminHandler: list reports", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		latestReport, latestReportErr := storage.GetLatestReport(db, storage.ReportTypeWeeklyReport)
		if latestReportErr != nil && latestReportErr != sql.ErrNoRows {
			slog.Error("adminHandler: latest weekly report", "err", latestReportErr)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		workouts, err := storage.ListWorkoutsWithMetrics(db, from, to, 200)
		if err != nil {
			slog.Error("adminHandler: list workouts", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		wyzeRecords, err := storage.ListBodyMetricRecords(db, wyzeFrom, wyzeTo, 200)
		if err != nil {
			slog.Error("adminHandler: list wyze records", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		tab := r.URL.Query().Get("tab")
		if tab == "" {
			tab = "actions"
		}

		inferredBlockStart := ""
		previousBlockStart := ""
		previousBlockEnd := ""
		currentDate := time.Now().Format("2006-01-02")
		currentIntervalText := "Current interval: —"
		if latestReportErr == nil {
			previousBlockStart = latestReport.WeekStart.Format("2006-01-02")
			previousBlockEnd = latestReport.WeekEnd.Format("2006-01-02")
			inferredBlockStart = latestReport.WeekEnd.AddDate(0, 0, 1).Format("2006-01-02")
			startDate, err := time.Parse("2006-01-02", inferredBlockStart)
			if err == nil {
				todayDate, err := time.Parse("2006-01-02", currentDate)
				if err == nil {
					days := int(todayDate.Sub(startDate).Hours()/24) + 1
					if days > 0 {
						currentIntervalText = fmt.Sprintf("Current interval: %d days starting on %s till today (%s)", days, inferredBlockStart, currentDate)
					} else {
						currentIntervalText = "Current interval: invalid date range"
					}
				}
			}
		}

		data := adminPageData{
			Reports:                 reports,
			ReportPeriods:           groupReportsByPeriod(reports),
			Workouts:                workouts,
			WyzeRecords:             wyzeRecords,
			FilterFrom:              filterFrom,
			FilterTo:                filterTo,
			WyzeFilterFrom:          wyzeFilterFrom,
			WyzeFilterTo:            wyzeFilterTo,
			ActiveTab:               tab,
			HasPreviousClosedReport: latestReportErr == nil,
			InferredBlockStart:      inferredBlockStart,
			PreviousBlockStart:      previousBlockStart,
			PreviousBlockEnd:        previousBlockEnd,
			CurrentDate:             currentDate,
			CurrentIntervalText:     currentIntervalText,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := adminTmpl.Execute(w, data); err != nil {
			slog.Error("adminHandler: render template", "err", err)
		}
	}
}
