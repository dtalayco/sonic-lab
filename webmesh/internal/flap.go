/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import (
	"fmt"
	"log"
	"strings"
)

// SetLinkDown brings both webmesh-side veth endpoints for the given link
// down, which also brings the switch-side peers down (veth peer semantics).
// The link is marked as down in the internal state map.
func (r *Reconciler) SetLinkDown(key string) error {
	r.mu.Lock()
	link, ok := r.applied[key]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("link %q not found in applied set", key)
	}
	r.linkStates[key] = false
	r.saveState()
	wmA, wmB := r.wmNames(link)
	r.mu.Unlock()

	if err := setLinkDown(wmA); err != nil {
		log.Printf("flap down %s: %v", wmA, err)
		return fmt.Errorf("bring %s down: %w", wmA, err)
	}
	if err := setLinkDown(wmB); err != nil {
		log.Printf("flap down %s: %v", wmB, err)
		// Best-effort: try to restore A
		_ = setLinkUp(wmA)
		return fmt.Errorf("bring %s down: %w", wmB, err)
	}

	log.Printf("link %s: DOWN", key)
	return nil
}

// SetLinkUp brings both webmesh-side veth endpoints for the given link
// back up. The link is marked as up in the internal state map.
func (r *Reconciler) SetLinkUp(key string) error {
	r.mu.Lock()
	link, ok := r.applied[key]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("link %q not found in applied set", key)
	}
	r.linkStates[key] = true
	r.saveState()
	wmA, wmB := r.wmNames(link)
	r.mu.Unlock()

	if err := setLinkUp(wmA); err != nil {
		log.Printf("flap up %s: %v", wmA, err)
		return fmt.Errorf("bring %s up: %w", wmA, err)
	}
	if err := setLinkUp(wmB); err != nil {
		log.Printf("flap up %s: %v", wmB, err)
		// Best-effort: try to restore A
		_ = setLinkDown(wmA)
		return fmt.Errorf("bring %s up: %w", wmB, err)
	}

	log.Printf("link %s: UP", key)
	return nil
}

// LinkState returns whether the given link is currently up (true) or down
// (false). Returns ("up", true) for links with no explicit state recorded.
func (r *Reconciler) LinkState(key string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if down, ok := r.linkStates[key]; ok && !down {
		return "down", true
	}
	return "up", true
}

// LinkStates returns a snapshot of all link states (key → "up"|"down").
func (r *Reconciler) LinkStates() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(r.applied))
	for key := range r.applied {
		if down, ok := r.linkStates[key]; ok && !down {
			out[key] = "down"
		} else {
			out[key] = "up"
		}
	}
	return out
}

// setLinkDown brings a single interface in the webmesh namespace down.
func setLinkDown(ifname string) error {
	return run("ip", "link", "set", ifname, "down")
}

// setLinkUp brings a single interface in the webmesh namespace up.
func setLinkUp(ifname string) error {
	return run("ip", "link", "set", ifname, "up")
}

// formatLinkState renders a human-readable state string for the UI.
func formatLinkState(state string) string {
	if strings.EqualFold(state, "down") {
		return "DOWN"
	}
	return "UP"
}