/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Link Flapping ---

func TestFlapSetLinkDownNotFound(t *testing.T) {
	r := newTestReconciler(t)
	err := r.SetLinkDown("nonexistent")
	if err == nil {
		t.Error("SetLinkDown(nonexistent): expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want contains %q", err.Error(), "not found")
	}
}

func TestFlapSetLinkUpNotFound(t *testing.T) {
	r := newTestReconciler(t)
	err := r.SetLinkUp("nonexistent")
	if err == nil {
		t.Error("SetLinkUp(nonexistent): expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want contains %q", err.Error(), "not found")
	}
}

func TestFlapLinkStateDefaultUp(t *testing.T) {
	r := newTestReconciler(t)
	link := Link{
		Endpoints: []Endpoint{
			{Device: "spine1-linux", Port: "Ethernet0"},
			{Device: "leaf1-linux", Port: "Ethernet0"},
		},
	}
	r.mu.Lock()
	r.applied[link.Key()] = link
	r.mu.Unlock()

	state, ok := r.LinkState(link.Key())
	if !ok {
		t.Error("LinkState: ok = false, want true")
	}
	if state != "up" {
		t.Errorf("state = %q, want %q", state, "up")
	}
}

func TestFlapLinkStatesSnapshot(t *testing.T) {
	r := newTestReconciler(t)
	link1 := Link{
		Endpoints: []Endpoint{
			{Device: "spine1-linux", Port: "Ethernet0"},
			{Device: "leaf1-linux", Port: "Ethernet0"},
		},
	}
	link2 := Link{
		Endpoints: []Endpoint{
			{Device: "spine1-linux", Port: "Ethernet4"},
			{Device: "leaf2-linux", Port: "Ethernet0"},
		},
	}
	r.mu.Lock()
	r.applied[link1.Key()] = link1
	r.applied[link2.Key()] = link2
	r.linkStates[link1.Key()] = false // down
	r.mu.Unlock()

	states := r.LinkStates()
	if len(states) != 2 {
		t.Fatalf("len(states) = %d, want 2", len(states))
	}
	if states[link1.Key()] != "down" {
		t.Errorf("states[%s] = %q, want %q", link1.Key(), states[link1.Key()], "down")
	}
	if states[link2.Key()] != "up" {
		t.Errorf("states[%s] = %q, want %q", link2.Key(), states[link2.Key()], "up")
	}
}

func TestFlapStatePersistence(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	r := NewReconciler(stateFile, "", time.Hour, nil)

	link := Link{
		Endpoints: []Endpoint{
			{Device: "spine1-linux", Port: "Ethernet0"},
			{Device: "leaf1-linux", Port: "Ethernet0"},
		},
	}
	key := link.Key()

	// Simulate: link applied and marked down
	r.mu.Lock()
	r.applied[key] = link
	r.linkStates[key] = false
	r.saveState()
	r.mu.Unlock()

	// Load into a new reconciler
	r2 := NewReconciler(stateFile, "", time.Hour, nil)
	state, ok := r2.LinkState(key)
	if !ok {
		t.Fatal("LinkState after reload: ok = false, want true")
	}
	if state != "down" {
		t.Errorf("state after reload = %q, want %q", state, "down")
	}
}

func TestFormatLinkState(t *testing.T) {
	if got := formatLinkState("up"); got != "UP" {
		t.Errorf("formatLinkState(up) = %q, want UP", got)
	}
	if got := formatLinkState("down"); got != "DOWN" {
		t.Errorf("formatLinkState(down) = %q, want DOWN", got)
	}
	if got := formatLinkState("DOWN"); got != "DOWN" {
		t.Errorf("formatLinkState(DOWN) = %q, want DOWN", got)
	}
}

// --- File Watcher ---

func TestReloadTopoFileNoChange(t *testing.T) {
	dir := t.TempDir()
	topoFile := filepath.Join(dir, "topology.yml")
	stateFile := filepath.Join(dir, "state.json")

	content := "links:\n  - endpoints:\n      - device: spine1-linux\n        port: Ethernet0\n      - device: leaf1-linux\n        port: Ethernet0\n"
	if err := os.WriteFile(topoFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewReconciler(stateFile, topoFile, time.Hour, nil)
	// Initial load in NewReconciler records mtime
	// A second call should detect no change
	changed := r.reloadTopoFile()
	if changed {
		t.Error("reloadTopoFile: changed = true, want false (no file modification)")
	}
}

func TestReloadTopoFileDetectsChange(t *testing.T) {
	dir := t.TempDir()
	topoFile := filepath.Join(dir, "topology.yml")
	stateFile := filepath.Join(dir, "state.json")

	content1 := "links:\n  - endpoints:\n      - device: spine1-linux\n        port: Ethernet0\n      - device: leaf1-linux\n        port: Ethernet0\n"
	if err := os.WriteFile(topoFile, []byte(content1), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewReconciler(stateFile, topoFile, time.Hour, nil)

	// Modify the file
	time.Sleep(1100 * time.Millisecond) // ensure mtime differs (1s granularity on some FS)
	content2 := "links: []\n"
	if err := os.WriteFile(topoFile, []byte(content2), 0o644); err != nil {
		t.Fatal(err)
	}
	// Force a future mtime to be safe
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(topoFile, future, future)

	changed := r.reloadTopoFile()
	if !changed {
		t.Error("reloadTopoFile: changed = false, want true (file was modified)")
	}
	// After reload, desired should be 0 (empty links)
	if r.DesiredCount() != 0 {
		t.Errorf("DesiredCount after reload = %d, want 0", r.DesiredCount())
	}
}

func TestReloadTopoFileEmptyPath(t *testing.T) {
	r := newTestReconciler(t)
	changed := r.reloadTopoFile()
	if changed {
		t.Error("reloadTopoFile with empty path: changed = true, want false")
	}
}

// --- Web UI ---

func TestUIEmptyTopology(t *testing.T) {
	api := newTestAPI(t)
	ts := httptest.NewServer(api.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

func TestUIWithLinks(t *testing.T) {
	api := newTestAPI(t)
	rec := api.rec
	link := Link{
		Endpoints: []Endpoint{
			{Device: "spine1-linux", Port: "Ethernet0"},
			{Device: "leaf1-linux", Port: "Ethernet0"},
		},
	}
	rec.mu.Lock()
	rec.applied[link.Key()] = link
	rec.mu.Unlock()

	ts := httptest.NewServer(api.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestUIMethodNotAllowed(t *testing.T) {
	api := newTestAPI(t)
	ts := httptest.NewServer(api.Handler())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

// --- API Flap Endpoints ---

func TestAPILinkFlapDownNotFound(t *testing.T) {
	api := newTestAPI(t)
	ts := httptest.NewServer(api.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/links/some-key/down", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}

func TestAPILinkFlapUpNotFound(t *testing.T) {
	api := newTestAPI(t)
	ts := httptest.NewServer(api.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/links/some-key/up", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}

func TestAPILinkFlapMethodNotAllowed(t *testing.T) {
	api := newTestAPI(t)
	ts := httptest.NewServer(api.Handler())
	defer ts.Close()

	// GET on /up should be 405
	resp, err := http.Get(ts.URL + "/v1/links/some-key/up")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /up: status = %d, want 405", resp.StatusCode)
	}

	// GET on /down should be 405
	resp, err = http.Get(ts.URL + "/v1/links/some-key/down")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /down: status = %d, want 405", resp.StatusCode)
	}
}
