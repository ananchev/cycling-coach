package web

import (
	"database/sql"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
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
	"deliveryBadge": func(status *string) template.HTML {
		if status == nil {
			return `<span class="badge grey">—</span>`
		}
		switch *status {
		case "sent":
			return `<span class="badge green">sent ✓</span>`
		case "failed":
			return `<span class="badge red">failed ✗</span>`
		default:
			return `<span class="badge yellow">pending</span>`
		}
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
  .modal-overlay{display:none;position:fixed;inset:0;background:rgba(0,0,0,.4);z-index:100;justify-content:center;align-items:center}
  .modal-overlay.show{display:flex}
  .modal{background:#fff;border-radius:8px;padding:20px 24px;max-width:500px;width:90%;box-shadow:0 8px 32px rgba(0,0,0,.2)}
  .modal h3{margin-bottom:12px;font-size:1rem}
  .modal p{font-size:.9rem;line-height:1.5;white-space:pre-wrap}
  .modal button{margin-top:12px}
</style>
</head>
<body>
<h1>🚴 Cycling Coach — Admin</h1>

<div class="tabs">
  <div class="tab{{if eq .ActiveTab "actions"}} active{{end}}" onclick="switchTab('actions')">Actions</div>
  <div class="tab{{if eq .ActiveTab "workouts"}} active{{end}}" onclick="switchTab('workouts')">Workouts ({{len .Workouts}})</div>
  <div class="tab{{if eq .ActiveTab "reports"}} active{{end}}" onclick="switchTab('reports')">Reports ({{len .Reports}})</div>
  <div class="tab{{if eq .ActiveTab "body"}} active{{end}}" onclick="switchTab('body')">Body Metrics</div>
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

<!-- GENERATE REPORT -->
<div class="card">
  <h2>Generate Report</h2>
  <div class="row">
    <div class="field">
      <label>Type</label>
      <select id="report-type">
        <option value="weekly_report">Weekly Report</option>
        <option value="weekly_plan">Weekly Plan</option>
      </select>
    </div>
    <div class="field"><label>Week start</label><input type="date" id="report-week"></div>
    <button id="gen-btn" onclick="runGenerate()"><span class="spinner" id="gen-spin"></span>Generate</button>
  </div>
  <div id="user-prompt-field" style="display:none;margin-top:12px">
    <label>Constraints / notes for Claude (optional)</label>
    <textarea id="user-prompt" rows="3" style="width:100%;padding:8px 10px;border:1px solid #ccc;border-radius:4px;font-size:.9rem;font-family:inherit;resize:vertical" placeholder="e.g. Travelling Tuesday, only 30 min available on Wednesday, prefer outdoor ride on Saturday..."></textarea>
    <p class="hint">Free-text instructions passed to Claude when generating the plan.</p>
  </div>
  <div class="result" id="report-result"></div>
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
  <p class="hint">Sends the last N weekly reports to Claude and rewrites the athlete profile. The current profile is backed up with a timestamp suffix before being replaced.</p>
  <div class="result" id="evolve-result"></div>
</div>

</div><!-- /tab-actions -->

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
  {{if .Workouts}}
  <div style="overflow-x:auto">
  <table>
    <thead><tr>
      <th>#</th><th>Date</th><th>Type</th><th>Duration</th><th>Source</th>
      <th>Avg Power</th><th>Avg HR</th><th>NP</th><th>TSS</th><th>HR Drift</th>
      <th>FIT</th><th>Processed</th><th title="Ride notes">Ride</th><th title="General notes">Notes</th>
    </tr></thead>
    <tbody>
    {{range .Workouts}}
    <tr>
      <td>{{.ID}}</td>
      <td style="white-space:nowrap">{{fmtDate .StartedAt}}</td>
      <td>{{if .WorkoutType}}{{deref .WorkoutType}}{{else}}—{{end}}</td>
      <td class="num">{{fmtDuration .DurationSec}}</td>
      <td>{{.Source}}</td>
      <td class="num">{{fmtOpt .AvgPower "%.0f W"}}</td>
      <td class="num">{{fmtOpt .AvgHR "%.0f"}}</td>
      <td class="num">{{fmtOpt .NormalizedPower "%.0f W"}}</td>
      <td class="num">{{fmtOpt .TSS "%.0f"}}</td>
      <td class="num">{{fmtOpt .HRDriftPct "%.1f%%"}}</td>
      <td>{{if .FITFilePath}}<span class="status-dot yes"></span>{{else}}<span class="status-dot no"></span>{{end}}</td>
      <td>{{if .Processed}}<span class="status-dot yes"></span>{{else}}<span class="status-dot no"></span>{{end}}</td>
      <td>{{if .RideNotes}}<span class="note-icon" onclick="showNotes({{.ID}}, 'ride')" title="Ride notes">💬</span>{{end}}</td>
      <td>{{if .GeneralNotes}}<span class="note-icon" onclick="showNotes({{.ID}}, 'note')" title="General notes">📝</span>{{end}}</td>
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
  <h2>Reports</h2>
  <form method="get" action="/admin" class="filter-form">
    <input type="hidden" name="tab" value="reports">
    <div class="field"><label>From week</label><input type="date" name="from" value="{{.FilterFrom}}"></div>
    <div class="field"><label>To week</label><input type="date" name="to" value="{{.FilterTo}}"></div>
    <button type="submit">Filter</button>
    <a href="/admin?tab=reports"><button type="button" class="secondary">Reset</button></a>
  </form>
  {{if .Reports}}
  <table>
    <thead><tr>
      <th>#</th><th>Type</th><th>Week</th><th>Status</th><th>Actions</th>
    </tr></thead>
    <tbody>
    {{range .Reports}}
    <tr id="row-{{.ID}}">
      <td>{{.ID}}</td>
      <td>{{.Type}}</td>
      <td>{{fmtDate .WeekStart}}</td>
      <td>{{deliveryBadge .DeliveryStatus}}</td>
      <td>
        <button class="act-btn secondary" onclick="sendReport({{.ID}})">Send</button>
        {{if .FullHTML}}&nbsp;<a href="{{if eq (print .Type) "weekly_plan"}}/plans/{{else}}/reports/{{end}}{{.ID}}" target="_blank"><button class="act-btn secondary">View</button></a>{{end}}
        &nbsp;<button class="act-btn danger" onclick="deleteReport({{.ID}})">Delete</button>
      </td>
    </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}
  <p style="color:#888;font-size:.9rem">No reports found for this date range.</p>
  {{end}}
  <div class="result" id="send-result"></div>
</div>
</div><!-- /tab-reports -->

<!-- ==================== BODY METRICS TAB ==================== -->
<div class="tab-pane{{if eq .ActiveTab "body"}} active{{end}}" id="tab-body">
<div class="card">
  <h2>Body Metrics</h2>
  <p class="hint" style="margin-bottom:16px">Data logged via Telegram: /weight, /bodyfat, /muscle</p>
  <div id="body-charts">
    <canvas id="chart-weight" height="200"></canvas>
    <canvas id="chart-bodyfat" style="margin-top:20px" height="200"></canvas>
    <canvas id="chart-muscle" style="margin-top:20px" height="200"></canvas>
  </div>
  <p id="body-empty" style="display:none;color:#888;font-size:.9rem">No body metrics recorded yet. Use the Telegram bot to log weight, body fat, and muscle mass.</p>
</div>
</div><!-- /tab-body -->

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
    <div id="note-modal-body"></div>
    <div class="result" id="note-modal-result" style="margin-bottom:8px"></div>
    <button class="secondary" onclick="closeNoteModal()">Close</button>
  </div>
</div>

<script>
var tabNames = ['actions','workouts','reports','body','logs'];
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

// Show/hide user prompt textarea based on report type.
document.getElementById('report-type').addEventListener('change', function() {
  document.getElementById('user-prompt-field').style.display = this.value === 'weekly_plan' ? '' : 'none';
});

async function runGenerate() {
  const type  = document.getElementById('report-type').value;
  const week  = document.getElementById('report-week').value;
  if (!week) { showResult('report-result', false, 'Please choose a week start date.'); return; }
  setLoading('gen-btn', 'gen-spin', true);
  showResult('report-result', true, 'Calling Claude API — this takes ~30 seconds...');
  try {
    const body = {type, week_start: week};
    if (type === 'weekly_plan') {
      const up = document.getElementById('user-prompt').value.trim();
      if (up) body.user_prompt = up;
    }
    const r = await fetch('/api/report', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify(body)
    });
    const j = await r.json();
    if (!r.ok) { showResult('report-result', false, JSON.stringify(j)); return; }
    showResult('report-result', true, 'Report #' + j.id + ' generated. Reload page to see it.');
  } catch(e) { showResult('report-result', false, e.toString()); }
  finally { setLoading('gen-btn', 'gen-spin', false); }
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
    const row = document.getElementById('row-' + id);
    if (row) row.remove();
    showResult('send-result', true, 'Report #' + id + ' deleted.');
  } catch(e) { showResult('send-result', false, e.toString()); }
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

var currentNoteType = null;
async function showNotes(workoutId, noteType) {
  currentNoteWorkoutId = workoutId;
  currentNoteType = noteType;
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
function loadBodyCharts() {
  if (bodyChartsLoaded) return;
  bodyChartsLoaded = true;
  fetch('/api/body-metrics').then(r => r.json()).then(function(data) {
    if (!data || data.length === 0) {
      document.getElementById('body-charts').style.display = 'none';
      document.getElementById('body-empty').style.display = 'block';
      return;
    }
    var weightData = data.filter(d => d.weight_kg);
    var bfData = data.filter(d => d.body_fat_pct);
    var mmData = data.filter(d => d.muscle_mass_kg);

    if (weightData.length) drawChart('chart-weight', 'Weight (kg)', weightData.map(d => d.date), weightData.map(d => d.weight_kg), '#2563eb');
    else document.getElementById('chart-weight').style.display = 'none';

    if (bfData.length) drawChart('chart-bodyfat', 'Body Fat (%)', bfData.map(d => d.date), bfData.map(d => d.body_fat_pct), '#f59e0b');
    else document.getElementById('chart-bodyfat').style.display = 'none';

    if (mmData.length) drawChart('chart-muscle', 'Muscle Mass (kg)', mmData.map(d => d.date), mmData.map(d => d.muscle_mass_kg), '#22c55e');
    else document.getElementById('chart-muscle').style.display = 'none';
  });
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
    ctx.fillText(labels[i].slice(5), x - 12, H - 8);
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

// Auto-load charts when body tab is shown.
var origSwitchTab = switchTab;
switchTab = function(name) {
  origSwitchTab(name);
  if (name === 'body') loadBodyCharts();
};
if (tabNames.indexOf('body') >= 0 && document.getElementById('tab-body').classList.contains('active')) {
  loadBodyCharts();
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

type adminPageData struct {
	Reports    []storage.ReportWithDelivery
	Workouts   []storage.WorkoutWithMetrics
	FilterFrom string
	FilterTo   string
	ActiveTab  string // "actions", "workouts", "reports"
}

func adminHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var from, to time.Time

		fromStr := r.URL.Query().Get("from")
		toStr := r.URL.Query().Get("to")

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

		// Default: last 8 weeks when no filter is provided.
		filterFrom := fromStr
		filterTo := toStr
		if from.IsZero() && to.IsZero() {
			from = time.Now().UTC().AddDate(0, 0, -adminDefaultWeeks*7)
			filterFrom = from.Format("2006-01-02")
		}

		reports, err := storage.ListReportsWithDelivery(db, from, to, 100)
		if err != nil {
			slog.Error("adminHandler: list reports", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		workouts, err := storage.ListWorkoutsWithMetrics(db, from, to, 200)
		if err != nil {
			slog.Error("adminHandler: list workouts", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		tab := r.URL.Query().Get("tab")
		if tab == "" {
			tab = "actions"
		}

		data := adminPageData{
			Reports:    reports,
			Workouts:   workouts,
			FilterFrom: filterFrom,
			FilterTo:   filterTo,
			ActiveTab:  tab,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := adminTmpl.Execute(w, data); err != nil {
			slog.Error("adminHandler: render template", "err", err)
		}
	}
}
