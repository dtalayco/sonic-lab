/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestAPIWithLink(t *testing.T) *API {
	t.Helper()
	api := newTestAPI(t)
	l := Link{Endpoints: []Endpoint{{Device: "sw1", Port: "Ethernet0"}, {Device: "host1", Port: "eth1"}}}
	api.rec.desired[l.Key()] = l
	api.rec.applied[l.Key()] = l
	return api
}

func TestStatsSnapshot(t *testing.T) {
	ts := httptest.NewServer(newTestAPIWithLink(t).Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var s StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatal(err)
	}
	if s.TimeMs == 0 || !s.Healthy || s.Desired != 1 || s.Applied != 1 {
		t.Errorf("snapshot = %+v", s)
	}
	if len(s.Links) != 1 || s.Links[0].A != "sw1:Ethernet0" || s.Links[0].B != "host1:eth1" || !s.Links[0].Up {
		t.Errorf("links = %+v", s.Links)
	}
}

func TestMetricsFormat(t *testing.T) {
	ts := httptest.NewServer(newTestAPIWithLink(t).Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{
		"# TYPE webmesh_link_bytes_total counter\n",
		"webmesh_links_applied 1\n",
		`webmesh_link_up{link="sw1:Ethernet0<->host1:eth1"} 1`,
		`webmesh_link_bytes_total{link="sw1:Ethernet0<->host1:eth1",from="sw1:Ethernet0",to="host1:eth1"} 0`,
		`webmesh_link_packets_total{link="sw1:Ethernet0<->host1:eth1",from="host1:eth1",to="sw1:Ethernet0"} 0`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("metrics missing %q\n%s", want, body)
		}
	}
}

func TestLabelValueEscapes(t *testing.T) {
	if got := labelValue("a\"b\\c\nd"); got != `"a\"b\\c\nd"` {
		t.Errorf("labelValue = %s", got)
	}
}

func TestUIRendersLiveView(t *testing.T) {
	ts := httptest.NewServer(newTestAPIWithLink(t).Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{`data-key="sw1:Ethernet0&lt;-&gt;host1:eth1"`, `data-up="true"`, `id="total-chart"`, "fetch('/v1/stats')"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("UI missing %q", want)
		}
	}
}
