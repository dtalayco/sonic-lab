/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Reconciler maintains the desired and applied link sets and drives
// create/delete operations to converge the two.
type Reconciler struct {
	mu           sync.Mutex
	desired      map[string]Link
	applied      map[string]Link
	linkStates   map[string]bool // key → true=up, false=down
	stateFile    string
	topologyFile string
	topoMtime    time.Time
	interval     time.Duration
	panel        *PatchPanel // may be nil if eBPF is unavailable
}

// NewReconciler creates a Reconciler that persists state to stateFile.
// If topologyFile is non-empty it will be loaded on first Run() call.
// If panel is nil, eBPF operations are skipped (fallback to Phase 1 mode).
func NewReconciler(stateFile, topologyFile string, interval time.Duration, panel *PatchPanel) *Reconciler {
	r := &Reconciler{
		desired:      make(map[string]Link),
		applied:      make(map[string]Link),
		linkStates:   make(map[string]bool),
		stateFile:    stateFile,
		topologyFile: topologyFile,
		interval:     interval,
		panel:        panel,
	}
	r.loadState()
	r.checkTopoFile()
	return r
}

// SetDesired replaces the desired topology and immediately reconciles.
func (r *Reconciler) SetDesired(t Topology) (created, deleted int, errs []string) {
	r.mu.Lock()
	newDesired := make(map[string]Link, len(t.Links))
	for _, l := range t.Links {
		if len(l.Endpoints) != 2 {
			errs = append(errs, fmt.Sprintf("skipping link %q: expected 2 endpoints, got %d", l.Key(), len(l.Endpoints)))
			continue
		}
		newDesired[l.Key()] = l
	}
	r.desired = newDesired
	r.mu.Unlock()

	created, deleted, recErrs := r.Reconcile()
	errs = append(errs, recErrs...)
	return created, deleted, errs
}

// AddLink adds a single link to the desired set and reconciles.
func (r *Reconciler) AddLink(l Link) error {
	if len(l.Endpoints) != 2 {
		return fmt.Errorf("link must have exactly 2 endpoints")
	}
	r.mu.Lock()
	r.desired[l.Key()] = l
	r.mu.Unlock()

	_, _, errs := r.Reconcile()
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// RemoveLink removes a link (by key) from the desired set and reconciles.
func (r *Reconciler) RemoveLink(key string) error {
	r.mu.Lock()
	delete(r.desired, key)
	r.mu.Unlock()

	_, _, errs := r.Reconcile()
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// Reconcile creates links that are desired but not yet applied, and deletes
// links that are applied but no longer desired.
func (r *Reconciler) Reconcile() (created, deleted int, errs []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Create missing links
	for key, link := range r.desired {
		if _, ok := r.applied[key]; ok {
			continue
		}
		a, b := link.Endpoints[0], link.Endpoints[1]
		if err := r.applyLink(a.Device, a.Port, b.Device, b.Port); err != nil {
			log.Printf("create %s: %v", key, err)
			errs = append(errs, fmt.Sprintf("create %s: %v", key, err))
			continue
		}
		r.applied[key] = link
		created++
	}

	// Delete extra links
	for key, link := range r.applied {
		if _, ok := r.desired[key]; ok {
			continue
		}
		a, b := link.Endpoints[0], link.Endpoints[1]
		r.unapplyLink(a.Device, a.Port, b.Device, b.Port)
		if err := deleteLink(a.Device, a.Port, b.Device, b.Port); err != nil {
			log.Printf("delete %s: %v", key, err)
			errs = append(errs, fmt.Sprintf("delete %s: %v", key, err))
			continue
		}
		delete(r.applied, key)
		deleted++
	}

	// Drift correction: verify applied links are actually UP
	r.verifyLinks()

	if created > 0 || deleted > 0 {
		r.saveState()
		log.Printf("reconcile: %d created, %d deleted", created, deleted)
	}
	return
}

// verifyLinks checks all applied links and corrects drift:
//   - If interfaces are missing (container restarted), removes from applied
//     so the next cycle re-creates them.
//   - If interfaces exist but are DOWN, brings them back UP
//     (unless explicitly downed via the flap API).
func (r *Reconciler) verifyLinks() {
	for key, link := range r.applied {
		// Respect explicit down state from the flap API
		if down, ok := r.linkStates[key]; ok && !down {
			continue
		}
		a, b := link.Endpoints[0], link.Endpoints[1]

		pidA, errA := containerPID(a.Device)
		pidB, errB := containerPID(b.Device)
		if errA != nil || errB != nil {
			// Container not running yet; skip
			continue
		}

		// Check if interfaces exist; if not, mark for re-creation
		if !linkInNS(pidA, a.Port) || !linkInNS(pidB, b.Port) {
			log.Printf("verify: link %s interfaces missing, will re-create", key)
			r.unapplyLink(a.Device, a.Port, b.Device, b.Port)
			delete(r.applied, key)
			r.saveState()
			continue
		}

		// Interfaces exist; ensure they are UP
		wmA := wmIfname(a.Device, a.Port)
		wmB := wmIfname(b.Device, b.Port)

		needUp := false
		if !linkIsUp(pidA, a.Port) {
			needUp = true
			runQuiet("nsenter", "-t", strconv.Itoa(pidA), "-n", "ip", "link", "set", a.Port, "up")
		}
		if !linkIsUp(pidB, b.Port) {
			needUp = true
			runQuiet("nsenter", "-t", strconv.Itoa(pidB), "-n", "ip", "link", "set", b.Port, "up")
		}
		if needUp {
			// Also ensure webmesh-side is up
			runQuiet("ip", "link", "set", wmA, "up")
			runQuiet("ip", "link", "set", wmB, "up")
			log.Printf("verify: brought link %s back up", key)
		}
	}
}

// applyLink creates the veth pairs and (if eBPF is available) configures
// the BPF map and TC attachments.
func (r *Reconciler) applyLink(devA, portA, devB, portB string) error {
	if err := createLink(devA, portA, devB, portB); err != nil {
		return err
	}

	// Configure eBPF (skip if not available)
	if r.panel == nil {
		return nil
	}

	wmA := wmIfname(devA, portA)
	wmB := wmIfname(devB, portB)

	ifIdxA, err := ifindexByName(wmA)
	if err != nil {
		return fmt.Errorf("resolve ifindex %s: %w", wmA, err)
	}
	ifIdxB, err := ifindexByName(wmB)
	if err != nil {
		return fmt.Errorf("resolve ifindex %s: %w", wmB, err)
	}

	if err := r.panel.UpdateLink(uint32(ifIdxA), uint32(ifIdxB)); err != nil {
		return fmt.Errorf("update BPF map: %w", err)
	}

	if err := r.panel.AttachTC(ifIdxA); err != nil {
		return fmt.Errorf("attach TC on %s: %w", wmA, err)
	}
	if err := r.panel.AttachTC(ifIdxB); err != nil {
		return fmt.Errorf("attach TC on %s: %w", wmB, err)
	}

	return nil
}

// unapplyLink detaches TC and removes BPF map entries before the veths
// are deleted.
func (r *Reconciler) unapplyLink(devA, portA, devB, portB string) {
	if r.panel == nil {
		return
	}

	wmA := wmIfname(devA, portA)
	wmB := wmIfname(devB, portB)

	idxA, aErr := ifindexByName(wmA)
	idxB, bErr := ifindexByName(wmB)

	if aErr == nil {
		r.panel.DetachTC(idxA)
	}
	if bErr == nil {
		r.panel.DetachTC(idxB)
	}
	if aErr == nil && bErr == nil {
		r.panel.RemoveLink(uint32(idxA), uint32(idxB))
	}
}

// checkTopoFile records the current mtime of the topology file so that
// subsequent calls can detect changes. Call once at startup.
func (r *Reconciler) checkTopoFile() {
	if r.topologyFile == "" {
		return
	}
	if info, err := os.Stat(r.topologyFile); err == nil {
		r.mu.Lock()
		r.topoMtime = info.ModTime()
		r.mu.Unlock()
	}
}

// reloadTopoFile checks whether the topology file has been modified since
// the last load. If so, it reloads the file and sets the desired topology.
// Returns true if the file was reloaded.
func (r *Reconciler) reloadTopoFile() bool {
	if r.topologyFile == "" {
		return false
	}
	info, err := os.Stat(r.topologyFile)
	if err != nil {
		return false
	}
	r.mu.Lock()
	changed := info.ModTime().After(r.topoMtime)
	if changed {
		r.topoMtime = info.ModTime()
	}
	r.mu.Unlock()

	if !changed {
		return false
	}
	top, err := LoadTopologyFile(r.topologyFile)
	if err != nil {
		log.Printf("reload topology error: %v", err)
		return false
	}
	log.Printf("topology file changed: loaded %d links from %s", len(top.Links), r.topologyFile)
	r.SetDesired(top)
	return true
}

// Run blocks forever, reconciling on a ticker, watching the topology file
// for changes, and reloading on SIGHUP. Call in a goroutine.
func (r *Reconciler) Run() {
	if r.topologyFile != "" {
		top, err := LoadTopologyFile(r.topologyFile)
		if err != nil {
			log.Printf("warning: could not load topology file %s: %v", r.topologyFile, err)
		} else {
			log.Printf("loaded %d links from %s", len(top.Links), r.topologyFile)
			r.SetDesired(top)
		}
	}
	r.checkTopoFile()

	// Initial reconcile
	r.Reconcile()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Watch for topology file changes
			r.reloadTopoFile()
			// Reconcile any drift
			r.Reconcile()
		case <-sigCh:
			log.Println("SIGHUP: reloading topology file")
			if r.topologyFile != "" {
				if top, err := LoadTopologyFile(r.topologyFile); err != nil {
					log.Printf("reload error: %v", err)
				} else {
					r.SetDesired(top)
				}
				r.checkTopoFile()
			}
		}
	}
}

// DesiredCount returns the number of links in the desired set.
func (r *Reconciler) DesiredCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.desired)
}

// AppliedCount returns the number of links in the applied set.
func (r *Reconciler) AppliedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.applied)
}

// AppliedLinks returns a slice of all applied links.
func (r *Reconciler) AppliedLinks() []Link {
	r.mu.Lock()
	defer r.mu.Unlock()
	links := make([]Link, 0, len(r.applied))
	for _, l := range r.applied {
		links = append(links, l)
	}
	return links
}

// DesiredLinks returns a slice of all desired links.
func (r *Reconciler) DesiredLinks() []Link {
	r.mu.Lock()
	defer r.mu.Unlock()
	links := make([]Link, 0, len(r.desired))
	for _, l := range r.desired {
		links = append(links, l)
	}
	return links
}

// Panel returns the PatchPanel (may be nil).
func (r *Reconciler) Panel() *PatchPanel {
	return r.panel
}

// LinkStatsResponse is the JSON response for GET /v1/links/{key}/stats.
type LinkStatsResponse struct {
	LinkID string   `json:"link_id"`
	AToB   LinkStats `json:"a_to_b"`
	BToA   LinkStats `json:"b_to_a"`
}

// GetLinkStats returns per-direction stats for the given link.
func (r *Reconciler) GetLinkStats(key string) (LinkStatsResponse, error) {
	r.mu.Lock()
	link, ok := r.applied[key]
	r.mu.Unlock()
	if !ok {
		return LinkStatsResponse{}, fmt.Errorf("link %q not found in applied set", key)
	}
	if r.panel == nil {
		return LinkStatsResponse{}, fmt.Errorf("eBPF not loaded")
	}

	a, b := link.Endpoints[0], link.Endpoints[1]
	wmA := wmIfname(a.Device, a.Port)
	wmB := wmIfname(b.Device, b.Port)

	ifIdxA, err := ifindexByName(wmA)
	if err != nil {
		return LinkStatsResponse{}, fmt.Errorf("resolve %s: %w", wmA, err)
	}
	ifIdxB, err := ifindexByName(wmB)
	if err != nil {
		return LinkStatsResponse{}, fmt.Errorf("resolve %s: %w", wmB, err)
	}

	aToB, _ := r.panel.GetStats(uint32(ifIdxA))
	bToA, _ := r.panel.GetStats(uint32(ifIdxB))

	return LinkStatsResponse{
		LinkID: key,
		AToB:   aToB,
		BToA:   bToA,
	}, nil
}

func (r *Reconciler) loadState() {
	data, err := os.ReadFile(r.stateFile)
	if err != nil {
		return
	}
	var s struct {
		Applied    []Link          `json:"applied"`
		LinkStates map[string]bool `json:"link_states,omitempty"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		log.Printf("warning: could not parse state file %s: %v", r.stateFile, err)
		return
	}
	for _, l := range s.Applied {
		r.applied[l.Key()] = l
	}
	if s.LinkStates != nil {
		for k, v := range s.LinkStates {
			r.linkStates[k] = v
		}
	}
	if len(r.applied) > 0 {
		log.Printf("restored %d applied links from %s", len(r.applied), r.stateFile)
	}
}

// saveState persists the current applied set and link states to disk.
// Caller must hold r.mu.
func (r *Reconciler) saveState() {
	links := make([]Link, 0, len(r.applied))
	for _, l := range r.applied {
		links = append(links, l)
	}
	// Only persist non-trivial states (down links)
	states := make(map[string]bool)
	for k, v := range r.linkStates {
		if !v {
			states[k] = v
		}
	}
	s := struct {
		Applied    []Link          `json:"applied"`
		LinkStates map[string]bool `json:"link_states,omitempty"`
	}{Applied: links, LinkStates: states}

	data, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(r.stateFile, data, 0o644)
}