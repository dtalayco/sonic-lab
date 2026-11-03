/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Counters are cumulative packet and byte counts for one direction of a link.
type Counters struct {
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

// LinkSample is the state and counters of one applied link.
type LinkSample struct {
	Key  string   `json:"key"`
	A    string   `json:"a"`
	B    string   `json:"b"`
	Up   bool     `json:"up"`
	AToB Counters `json:"a_to_b"`
	BToA Counters `json:"b_to_a"`
}

// StatsResponse is a timestamped snapshot of every applied link. Counters
// are raw totals; rates are left to the consumer.
type StatsResponse struct {
	TimeMs  int64        `json:"time_ms"`
	Healthy bool         `json:"healthy"`
	Desired int          `json:"desired"`
	Applied int          `json:"applied"`
	Links   []LinkSample `json:"links"`
}

func (a *API) snapshot() StatsResponse {
	links := a.rec.AppliedLinks()
	sort.Slice(links, func(i, j int) bool { return links[i].Key() < links[j].Key() })
	states := a.rec.LinkStates()

	resp := StatsResponse{
		TimeMs:  time.Now().UnixMilli(),
		Desired: a.rec.DesiredCount(),
		Applied: a.rec.AppliedCount(),
		Links:   make([]LinkSample, 0, len(links)),
	}
	resp.Healthy = resp.Desired > 0 && resp.Desired == resp.Applied
	for _, l := range links {
		key := l.Key()
		s := LinkSample{
			Key: key,
			A:   l.Endpoints[0].Device + ":" + l.Endpoints[0].Port,
			B:   l.Endpoints[1].Device + ":" + l.Endpoints[1].Port,
			Up:  states[key] != "down",
		}
		// Counters stay zero without eBPF
		if st, err := a.rec.GetLinkStats(key); err == nil {
			s.AToB = Counters(st.AToB)
			s.BToA = Counters(st.BToA)
		}
		resp.Links = append(resp.Links, s)
	}
	return resp
}

// --- /v1/stats ---

func (a *API) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, a.snapshot())
}

// --- /metrics (Prometheus text format) ---

func (a *API) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s := a.snapshot()
	var b strings.Builder

	gauge := func(name, help string, v int) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n", name, help, name, name, v)
	}
	gauge("webmesh_links_desired", "Links in the desired topology.", s.Desired)
	gauge("webmesh_links_applied", "Links currently wired.", s.Applied)

	b.WriteString("# HELP webmesh_link_up Whether a wired link is up (1) or taken down (0).\n")
	b.WriteString("# TYPE webmesh_link_up gauge\n")
	for _, l := range s.Links {
		up := 0
		if l.Up {
			up = 1
		}
		fmt.Fprintf(&b, "webmesh_link_up{link=%s} %d\n", labelValue(l.Key), up)
	}

	counter := func(name, help string, get func(Counters) uint64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s counter\n", name, help, name)
		for _, l := range s.Links {
			fmt.Fprintf(&b, "%s{link=%s,from=%s,to=%s} %d\n", name,
				labelValue(l.Key), labelValue(l.A), labelValue(l.B), get(l.AToB))
			fmt.Fprintf(&b, "%s{link=%s,from=%s,to=%s} %d\n", name,
				labelValue(l.Key), labelValue(l.B), labelValue(l.A), get(l.BToA))
		}
	}
	counter("webmesh_link_bytes_total", "Bytes forwarded on a link, by direction.",
		func(c Counters) uint64 { return c.Bytes })
	counter("webmesh_link_packets_total", "Packets forwarded on a link, by direction.",
		func(c Counters) uint64 { return c.Packets })

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

// labelValue quotes a Prometheus label value.
func labelValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return `"` + s + `"`
}
