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
</style>
</head>
<body>
<h1>webmesh</h1>
<p class="subtitle">SONiC Virtual Switch Lab — eBPF Patch Panel &middot; v{{.Version}}</p>

<div class="status-bar">
  <span class="badge {{if .Healthy}}healthy{{else}}unhealthy{{end}}">
    {{if .Healthy}}● Healthy{{else}}● Unhealthy{{end}}
  </span>
  <span class="badge info">Desired: {{.Desired}}</span>
  <span class="badge info">Applied: {{.Applied}}</span>
</div>

{{if .Links}}
<table>
<thead>
<tr>
  <th>Link</th>
  <th>Endpoint A</th>
  <th>Endpoint B</th>
  <th>State</th>
  <th class="num">Packets A→B</th>
  <th class="num">Bytes A→B</th>
  <th class="num">Packets B→A</th>
  <th class="num">Bytes B→A</th>
  <th>Actions</th>
</tr>
</thead>
<tbody>
{{range .Links}}
<tr>
  <td><code>{{.Key}}</code></td>
  <td>{{.EndpointA}}</td>
  <td>{{.EndpointB}}</td>
  <td class="state-{{.State}}">{{.StateLabel}}</td>
  <td class="num">{{.StatsAToB.Packets}}</td>
  <td class="num">{{.StatsAToB.Bytes}}</td>
  <td class="num">{{.StatsBToA.Packets}}</td>
  <td class="num">{{.StatsBToA.Bytes}}</td>
  <td class="actions">
    {{if eq .State "up"}}
    <button class="danger" onclick="flap('{{.Key}}','down')">Down</button>
    {{else}}
    <button onclick="flap('{{.Key}}','up')">Up</button>
    {{end}}
    <button class="danger" onclick="removeLink('{{.Key}}')">Delete</button>
  </td>
</tr>
{{end}}
</tbody>
</table>
{{else}}
<div class="empty">No links. Push a topology via <code>PUT /v1/topology</code> or the topology file.</div>
{{end}}

<div class="footer">
  Auto-refreshes every 2s &middot; API: <code>GET /v1/status</code> <code>GET /v1/links</code> <code>PUT /v1/topology</code>
</div>

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
setInterval(() => location.reload(), 2000);
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
