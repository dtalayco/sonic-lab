/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestAPI(t *testing.T) *API {
	t.Helper()
	dir := t.TempDir()
	rec := NewReconciler(dir+"/state.json", "", time.Hour, nil)
	return NewAPI(rec, "test")
}

func TestAPIStatus(t *testing.T) {
	api := newTestAPI(t)
	ts := httptest.NewServer(api.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Version != "test" {
		t.Errorf("version = %q, want %q", body.Version, "test")
	}
	if body.Healthy {
		t.Error("healthy = true with no links, want false")
	}
}

func TestAPITopologyPut(t *testing.T) {
	api := newTestAPI(t)
	ts := httptest.NewServer(api.Handler())
	defer ts.Close()

	body := `{"links":[{"endpoints":[{"device":"spine1-linux","port":"Ethernet0"},{"device":"leaf1-linux","port":"Ethernet0"}]}]}`
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/v1/topology", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out TopologyResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Desired != 1 {
		t.Errorf("desired = %d, want 1", out.Desired)
	}
}

func TestAPITopologyPutInvalidJSON(t *testing.T) {
	api := newTestAPI(t)
	ts := httptest.NewServer(api.Handler())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/v1/topology", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAPITopologyMethodNotAllowed(t *testing.T) {
	api := newTestAPI(t)
	ts := httptest.NewServer(api.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/topology")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestAPILinksGetEmpty(t *testing.T) {
	api := newTestAPI(t)
	ts := httptest.NewServer(api.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/links")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body LinksResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Links) != 0 {
		t.Errorf("len(links) = %d, want 0", len(body.Links))
	}
}

func TestAPILinksPostValidation(t *testing.T) {
	api := newTestAPI(t)
	ts := httptest.NewServer(api.Handler())
	defer ts.Close()

	// Invalid: 1 endpoint
	body := `{"endpoints":[{"device":"a","port":"b"}]}`
	resp, err := http.Post(ts.URL+"/v1/links", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}

func TestAPILinksDelete(t *testing.T) {
	api := newTestAPI(t)
	ts := httptest.NewServer(api.Handler())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/v1/links/some-key", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Deleting a non-existent link should succeed (idempotent)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
}

func TestAPILinkStatsNoEBPF(t *testing.T) {
	api := newTestAPI(t)
	ts := httptest.NewServer(api.Handler())
	defer ts.Close()

	// Stats endpoint should return 404 when the link doesn't exist
	resp, err := http.Get(ts.URL + "/v1/links/some-key/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestAPILinkStatsMethodNotAllowed(t *testing.T) {
	api := newTestAPI(t)
	ts := httptest.NewServer(api.Handler())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/v1/links/some-key/stats", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}
