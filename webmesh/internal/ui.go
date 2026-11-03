/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import (
	"html/template"
	"net/http"
	"sort"
)

// uiTemplate is the HTML template for the webmesh dashboard.
var uiTemplate = template.Must(template.New("dashboard").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>webmesh — SONiC VS Lab</title>
<style>
:root { --bg:#0d1117; --card:#161b22; --border:#30363d; --text:#e6edf3; --dim:#7d8590;
        --green:#3fb950; --red:#f85149; --blue:#58a6ff; --yellow:#d29922; }
* { box-sizing:border-box; margin:0; padding:0; }
body { background:var(--bg); color:var(--text); font:14px/1.5 -apple-system,BlinkMacSystemFont,sans-serif;
       min-height:100vh; padding:2rem; }
h1 { font-size:1.4rem; margin-bottom:0.25rem; }
.subtitle { color:var(--dim); margin-bottom:1.5rem; font-size:0.85rem; }
.status-bar { display:flex; gap:1rem; margin-bottom:1.5rem; flex-wrap:wrap; }
.badge { padding:0.25rem 0.75rem; border-radius:12px; font-size:0.8rem; font-weight:600; }
.badge.healthy { background:rgba(63,185,80,0.15); color:var(--green); }
.badge.unhealthy { background:rgba(248,81,73,0.15); color:var(--red); }
.badge.info { background:rgba(88,166,255,0.15); color:var(--blue); }
table { width:100%; border-collapse:collapse; background:var(--card); border-radius:8px; overflow:hidden; }
th, td { padding:0.6rem 0.9rem; text-align:left; border-bottom:1px solid var(--border); }
th { background:var(--card); color:var(--dim); font-size:0.75rem; text-transform:uppercase; letter-spacing:0.05em; }
tr:last-child td { border-bottom:none; }
tr:hover td { background:rgba(255,255,255,0.02); }
.state-up { color:var(--green); font-weight:600; }
.state-down { color:var(--red); font-weight:600; }
.num { text-align:right; font-variant-numeric:tabular-nums; }
.actions { display:flex; gap:0.4rem; }
button { background:var(--card); border:1px solid var(--border); color:var(--text);
         padding:0.25rem 0.6rem; border-radius:4px; cursor:pointer; font-size:0.75rem; }
button:hover { border-color:var(--blue); }
button.danger:hover { border-color:var(--red); }
.empty { text-align:center; color:var(--dim); padding:2rem; }
.footer { margin-top:1.5rem; color:var(--dim); font-size:0.75rem; }
:root { --series-1:#3987e5; --series-2:#d95926; --grid:#30363d; }
.table-wrap { overflow-x:auto; border-radius:8px; }
.chart { background:var(--card); border:1px solid var(--border); border-radius:8px;
         padding:0.9rem 1rem; margin-bottom:1.5rem; }
.chart-head { display:flex; justify-content:space-between; align-items:baseline; gap:1rem; flex-wrap:wrap; }
.chart-title { font-weight:600; }
.chart-value { font-size:1.4rem; font-variant-numeric:tabular-nums; }
.chart svg { display:block; width:100%; height:120px; margin-top:0.5rem; }
.axis { display:flex; justify-content:space-between; color:var(--dim); font-size:0.7rem; }
.spark { width:180px; height:36px; display:block; }
.swatch { display:inline-block; width:10px; height:3px; border-radius:2px; vertical-align:middle; margin:0 0.25rem 0 0.5rem; }
.legend { color:var(--dim); font-size:0.7rem; text-transform:none; letter-spacing:0; font-weight:400; }
.dir { white-space:nowrap; }
#tip { position:fixed; pointer-events:none; background:#010409; border:1px solid var(--border);
       border-radius:6px; padding:0.35rem 0.55rem; font-size:0.75rem; display:none; z-index:10;
       font-variant-numeric:tabular-nums; }
</style>
</head>
<body>
<h1>webmesh</h1>
<p class="subtitle">SONiC Virtual Switch Lab — eBPF Patch Panel &middot; v{{.Version}}</p>

<div class="status-bar">
  <span id="health" class="badge {{if .Healthy}}healthy{{else}}unhealthy{{end}}">
    {{if .Healthy}}● Healthy{{else}}● Unhealthy{{end}}
  </span>
  <span id="desired" class="badge info">Desired: {{.Desired}}</span>
  <span id="applied" class="badge info">Applied: {{.Applied}}</span>
</div>

{{if .Links}}
<div class="chart">
  <div class="chart-head">
    <span class="chart-title">Forwarded, all links and directions</span>
    <span class="chart-value" id="total-now">–</span>
  </div>
  <svg id="total-chart" viewBox="0 0 600 120" preserveAspectRatio="none" role="img"
       aria-label="Total throughput over the last two minutes"></svg>
  <div class="axis"><span>2 min ago</span><span id="total-max"></span><span>now</span></div>
</div>

<div class="table-wrap">
<table>
<thead>
<tr>
  <th>Link</th>
  <th>State</th>
  <th>Last 2 min <span class="legend"><span class="swatch" style="background:var(--series-1)"></span>A→B<span class="swatch" style="background:var(--series-2)"></span>B→A</span></th>
  <th class="num">A→B now</th>
  <th class="num">B→A now</th>
  <th class="num">Total A→B</th>
  <th class="num">Total B→A</th>
  <th>Actions</th>
</tr>
</thead>
<tbody>
{{range .Links}}
<tr data-key="{{.Key}}" data-up="{{if eq .State "up"}}true{{else}}false{{end}}">
  <td><code>{{.Key}}</code></td>
  <td class="state-{{.State}}">{{.StateLabel}}</td>
  <td><svg class="spark" viewBox="0 0 180 36" preserveAspectRatio="none"></svg></td>
  <td class="num dir rate-ab">–</td>
  <td class="num dir rate-ba">–</td>
  <td class="num dir total-ab" title="{{.StatsAToB.Packets}} packets">{{.StatsAToB.Bytes}} B</td>
  <td class="num dir total-ba" title="{{.StatsBToA.Packets}} packets">{{.StatsBToA.Bytes}} B</td>
  <td><div class="actions">
    {{if eq .State "up"}}
    <button class="danger" onclick="flap('{{.Key}}','down')">Down</button>
    {{else}}
    <button onclick="flap('{{.Key}}','up')">Up</button>
    {{end}}
    <button class="danger" onclick="removeLink('{{.Key}}')">Delete</button>
  </div></td>
</tr>
{{end}}
</tbody>
</table>
</div>
{{else}}
<div class="empty">No links. Push a topology via <code>PUT /v1/topology</code> or the topology file.</div>
{{end}}

<div class="footer">
  Live, sampled every second &middot; API: <code>GET /v1/status</code> <code>GET /v1/links</code>
  <code>GET /v1/stats</code> <code>GET /metrics</code> <code>PUT /v1/topology</code>
</div>
<div id="tip"></div>

<script>
function flap(key, action) {
  fetch('/v1/links/' + encodeURIComponent(key) + '/' + action, {method:'POST'})
    .then(r => { if(!r.ok) alert('Error: ' + r.status) })
    .catch(e => alert('Request failed: ' + e));
}
function removeLink(key) {
  if(!confirm('Delete link ' + key + '?')) return;
  fetch('/v1/links/' + encodeURIComponent(key), {method:'DELETE'})
    .then(r => { if(!r.ok) alert('Error: ' + r.status) })
    .catch(e => alert('Request failed: ' + e));
}

// Live throughput: poll /v1/stats, turn counter deltas into bits/s, and
// keep two minutes of history per link direction.
const HIST = 120;
const hist = {};
const totals = [];
let prev = null;

function fmtRate(bps) {
  if (bps >= 1e9) return (bps / 1e9).toFixed(2) + ' Gb/s';
  if (bps >= 1e6) return (bps / 1e6).toFixed(1) + ' Mb/s';
  if (bps >= 1e3) return (bps / 1e3).toFixed(0) + ' kb/s';
  return bps.toFixed(0) + ' b/s';
}
function fmtBytes(b) {
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (b >= 1000 && i < u.length - 1) { b /= 1000; i++; }
  return (i ? b.toFixed(1) : b) + ' ' + u[i];
}
function push(arr, v) { arr.push(v); if (arr.length > HIST) arr.shift(); }

// Draw series (arrays of equal length) as lines on a shared y scale.
function draw(svg, series, colors, w, h) {
  const n = Math.max(...series.map(s => s.length));
  const max = Math.max(1, ...series.flat());
  let out = '<line x1="0" y1="' + (h - 1) + '" x2="' + w + '" y2="' + (h - 1) +
            '" stroke="var(--grid)" stroke-width="1" vector-effect="non-scaling-stroke"/>';
  series.forEach((s, k) => {
    if (s.length < 2) return;
    const off = HIST - s.length;
    const pts = s.map((v, i) => ((off + i) / (HIST - 1) * w).toFixed(1) + ',' +
                               (h - 2 - v / max * (h - 6)).toFixed(1)).join(' ');
    out += '<polyline points="' + pts + '" fill="none" stroke="' + colors[k] +
           '" stroke-width="2" stroke-linejoin="round" stroke-linecap="round" vector-effect="non-scaling-stroke"/>';
  });
  out += '<line class="cross" x1="0" y1="0" x2="0" y2="' + h + '" stroke="var(--dim)" stroke-width="1"' +
         ' vector-effect="non-scaling-stroke" visibility="hidden"/>';
  svg.innerHTML = out;
  return max;
}

// Crosshair and tooltip; text(i) describes history index i.
function hover(svg, w, text) {
  const tip = document.getElementById('tip');
  svg.onmousemove = e => {
    const r = svg.getBoundingClientRect();
    const i = Math.round((e.clientX - r.left) / r.width * (HIST - 1));
    const t = text(i);
    const cross = svg.querySelector('.cross');
    if (!t) { tip.style.display = 'none'; if (cross) cross.setAttribute('visibility', 'hidden'); return; }
    if (cross) {
      const x = (i / (HIST - 1) * w).toFixed(1);
      cross.setAttribute('x1', x); cross.setAttribute('x2', x); cross.setAttribute('visibility', 'visible');
    }
    tip.innerHTML = t;
    tip.style.display = 'block';
    tip.style.left = Math.min(e.clientX + 12, window.innerWidth - tip.offsetWidth - 8) + 'px';
    tip.style.top = (e.clientY + 12) + 'px';
  };
  svg.onmouseleave = () => {
    tip.style.display = 'none';
    const cross = svg.querySelector('.cross');
    if (cross) cross.setAttribute('visibility', 'hidden');
  };
}

function ago(i) {
  const s = HIST - 1 - i;
  return s === 0 ? 'now' : s + 's ago';
}
function at(arr, i) { const j = i - (HIST - arr.length); return j >= 0 ? arr[j] : undefined; }

async function poll() {
  let s;
  try { s = await (await fetch('/v1/stats')).json(); } catch (e) { return; }

  // Re-render when the set of links or their state changes
  const rows = [...document.querySelectorAll('tr[data-key]')];
  const want = s.links.map(l => l.key + (l.up ? '+' : '-')).join('|');
  const have = rows.map(r => r.dataset.key + (r.dataset.up === 'true' ? '+' : '-')).join('|');
  if (want !== have) { location.reload(); return; }

  const health = document.getElementById('health');
  health.className = 'badge ' + (s.healthy ? 'healthy' : 'unhealthy');
  health.textContent = s.healthy ? '● Healthy' : '● Unhealthy';
  document.getElementById('desired').textContent = 'Desired: ' + s.desired;
  document.getElementById('applied').textContent = 'Applied: ' + s.applied;

  if (prev) {
    const dt = (s.time_ms - prev.time_ms) / 1000;
    const before = {};
    prev.links.forEach(l => before[l.key] = l);
    let sum = 0;
    s.links.forEach(l => {
      const p = before[l.key];
      if (!p || dt <= 0) return;
      // Counters reset when a link is re-created; treat that as zero
      const ab = Math.max(0, (l.a_to_b.bytes - p.a_to_b.bytes) * 8 / dt);
      const ba = Math.max(0, (l.b_to_a.bytes - p.b_to_a.bytes) * 8 / dt);
      const h = hist[l.key] = hist[l.key] || { ab: [], ba: [] };
      push(h.ab, ab); push(h.ba, ba);
      sum += ab + ba;
    });
    push(totals, sum);
  }
  prev = s;

  s.links.forEach((l, k) => {
    const row = rows[k];
    const h = hist[l.key] || { ab: [], ba: [] };
    row.querySelector('.rate-ab').textContent = h.ab.length ? fmtRate(h.ab[h.ab.length - 1]) : '–';
    row.querySelector('.rate-ba').textContent = h.ba.length ? fmtRate(h.ba[h.ba.length - 1]) : '–';
    const tab = row.querySelector('.total-ab'), tba = row.querySelector('.total-ba');
    tab.textContent = fmtBytes(l.a_to_b.bytes); tab.title = l.a_to_b.packets + ' packets';
    tba.textContent = fmtBytes(l.b_to_a.bytes); tba.title = l.b_to_a.packets + ' packets';
    const svg = row.querySelector('.spark');
    draw(svg, [h.ab, h.ba], ['var(--series-1)', 'var(--series-2)'], 180, 36);
    hover(svg, 180, i => {
      const a = at(h.ab, i), b = at(h.ba, i);
      if (a === undefined) return '';
      return ago(i) + '<br>A→B ' + fmtRate(a) + '<br>B→A ' + fmtRate(b);
    });
  });

  const chart = document.getElementById('total-chart');
  if (chart) {
    draw(chart, [totals], ['var(--series-1)'], 600, 120);
    document.getElementById('total-now').textContent = totals.length ? fmtRate(totals[totals.length - 1]) : '–';
    document.getElementById('total-max').textContent = totals.length ? 'peak ' + fmtRate(Math.max(...totals)) : '';
    hover(chart, 600, i => {
      const v = at(totals, i);
      return v === undefined ? '' : ago(i) + '<br>' + fmtRate(v);
    });
  }
}
poll();
setInterval(poll, 1000);
</script>
</body>
</html>
`))

// uiLinkRow is the per-link data passed to the UI template.
type uiLinkRow struct {
	Key         string
	EndpointA   string
	EndpointB   string
	State       string // "up" or "down"
	StateLabel  string // "UP" or "DOWN"
	StatsAToB   LinkStats
	StatsBToA   LinkStats
}

// uiData is the top-level data for the dashboard template.
type uiData struct {
	Version string
	Healthy bool
	Desired int
	Applied int
	Links   []uiLinkRow
}

// handleUI serves the webmesh dashboard at /.
func (a *API) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	links := a.rec.AppliedLinks()
	states := a.rec.LinkStates()

	// Sort for stable display
	sort.Slice(links, func(i, j int) bool {
		return links[i].Key() < links[j].Key()
	})

	rows := make([]uiLinkRow, 0, len(links))
	for _, l := range links {
		key := l.Key()
		epA, epB := l.Endpoints[0], l.Endpoints[1]
		state := states[key]
		if state == "" {
			state = "up"
		}

		row := uiLinkRow{
			Key:        key,
			EndpointA:  epA.Device + ":" + epA.Port,
			EndpointB:  epB.Device + ":" + epB.Port,
			State:      state,
			StateLabel: formatLinkState(state),
		}

		// Fetch stats if eBPF is available
		if stats, err := a.rec.GetLinkStats(key); err == nil {
			row.StatsAToB = stats.AToB
			row.StatsBToA = stats.BToA
		}

		rows = append(rows, row)
	}

	desired := a.rec.DesiredCount()
	applied := a.rec.AppliedCount()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = uiTemplate.Execute(w, uiData{
		Version: a.version,
		Healthy: desired > 0 && desired == applied,
		Desired: desired,
		Applied: applied,
		Links:   rows,
	})
}
