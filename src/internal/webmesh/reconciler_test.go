/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestReconciler(t *testing.T) *Reconciler {
	t.Helper()
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	return NewReconciler(stateFile, "", time.Hour, nil) // long interval: no background ticks
}

func TestReconcilerSetDesiredInvalidLinks(t *testing.T) {
	r := newTestReconciler(t)

	top := Topology{
		Links: []Link{
			{Endpoints: []Endpoint{{Device: "a", Port: "b"}}}, // 1 endpoint: invalid
			{Endpoints: []Endpoint{}},                         // 0 endpoints: invalid
		},
	}
	created, deleted, errs := r.SetDesired(top)
	if created != 0 {
		t.Errorf("created = %d, want 0", created)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
	if len(errs) != 2 {
		t.Errorf("len(errs) = %d, want 2", len(errs))
	}
	if r.DesiredCount() != 0 {
		t.Errorf("DesiredCount = %d, want 0", r.DesiredCount())
	}
}

func TestReconcilerSetDesiredValid(t *testing.T) {
	r := newTestReconciler(t)

	top := Topology{
		Links: []Link{
			{
				Endpoints: []Endpoint{
					{Device: "spine1-linux", Port: "Ethernet0"},
					{Device: "leaf1-linux", Port: "Ethernet0"},
				},
			},
		},
	}
	r.SetDesired(top)
	if r.DesiredCount() != 1 {
		t.Errorf("DesiredCount = %d, want 1", r.DesiredCount())
	}
}

func TestReconcilerAddLinkValidation(t *testing.T) {
	r := newTestReconciler(t)

	// Invalid: 1 endpoint
	err := r.AddLink(Link{Endpoints: []Endpoint{{Device: "a", Port: "b"}}})
	if err == nil {
		t.Error("AddLink with 1 endpoint: expected error, got nil")
	}

	// Invalid: 0 endpoints
	err = r.AddLink(Link{Endpoints: []Endpoint{}})
	if err == nil {
		t.Error("AddLink with 0 endpoints: expected error, got nil")
	}
}

func TestReconcilerRemoveNonExistent(t *testing.T) {
	r := newTestReconciler(t)
	// Removing a key that was never added should not error
	if err := r.RemoveLink("nonexistent"); err != nil {
		t.Errorf("RemoveLink(nonexistent) = %v, want nil", err)
	}
}

func TestReconcilerStatePersistence(t *testing.T) {
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

	// Simulate applied state
	r.mu.Lock()
	r.applied[key] = link
	r.saveState()
	r.mu.Unlock()

	// Verify file exists
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("state file not created: %v", err)
	}

	// Load into a new reconciler
	r2 := NewReconciler(stateFile, "", time.Hour, nil)
	if r2.AppliedCount() != 1 {
		t.Errorf("r2.AppliedCount = %d, want 1", r2.AppliedCount())
	}
	links := r2.AppliedLinks()
	if len(links) != 1 || links[0].Key() != key {
		t.Errorf("r2.AppliedLinks() = %+v, want key %q", links, key)
	}
}

func TestReconcilerStatePersistenceEmpty(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	r := NewReconciler(stateFile, "", time.Hour, nil)
	if r.AppliedCount() != 0 {
		t.Errorf("AppliedCount with no state file = %d, want 0", r.AppliedCount())
	}
}

func TestReconcilerAppliedLinksEmpty(t *testing.T) {
	r := newTestReconciler(t)
	links := r.AppliedLinks()
	if links == nil {
		t.Error("AppliedLinks() returned nil, want empty slice")
	}
	if len(links) != 0 {
		t.Errorf("len(AppliedLinks()) = %d, want 0", len(links))
	}
}

func TestReconcilerGetLinkStatsNoEBPF(t *testing.T) {
	r := newTestReconciler(t)
	// Inject a link into applied set
	link := Link{
		Endpoints: []Endpoint{
			{Device: "spine1-linux", Port: "Ethernet0"},
			{Device: "leaf1-linux", Port: "Ethernet0"},
		},
	}
	r.mu.Lock()
	r.applied[link.Key()] = link
	r.mu.Unlock()

	// panel is nil, so GetLinkStats should return an error
	_, err := r.GetLinkStats(link.Key())
	if err == nil {
		t.Error("GetLinkStats with nil panel: expected error, got nil")
	}
}

func TestReconcilerGetLinkStatsNotFound(t *testing.T) {
	r := newTestReconciler(t)
	_, err := r.GetLinkStats("nonexistent")
	if err == nil {
		t.Error("GetLinkStats(nonexistent): expected error, got nil")
	}
}
