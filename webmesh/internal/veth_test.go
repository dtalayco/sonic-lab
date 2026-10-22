/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import "testing"

func TestWmIfname(t *testing.T) {
	tests := []struct {
		dev, port, want string
	}{
		{"spine1-linux", "Ethernet0", "wm-spine1-0"},
		{"spine1-linux", "Ethernet4", "wm-spine1-4"},
		{"leaf1-linux", "Ethernet0", "wm-leaf1-0"},
		{"leaf2-linux", "Ethernet8", "wm-leaf2-8"},
		{"spine1", "Ethernet0", "wm-spine1-0"},
		{"leaf1", "Ethernet4", "wm-leaf1-4"},
		{"spine2-linux", "Ethernet12", "wm-spine2-12"},
		{"a-linux", "Ethernet0", "wm-a-0"},
		{"spine1-net", "Ethernet40", "wm-spine1-40"},
		{"leaf1-ns", "Ethernet4", "wm-leaf1-4"},
		{"host1", "Ethernet0", "wm-host1-0"},
	}
	for _, tt := range tests {
		if got := wmIfname(tt.dev, tt.port); got != tt.want {
			t.Errorf("wmIfname(%q, %q) = %q, want %q", tt.dev, tt.port, got, tt.want)
		}
	}
}

func TestSwIfname(t *testing.T) {
	tests := []struct {
		dev, port, want string
	}{
		{"spine1-linux", "Ethernet0", "sw-spine1-0"},
		{"leaf1-linux", "Ethernet4", "sw-leaf1-4"},
		{"spine1", "Ethernet0", "sw-spine1-0"},
		{"spine1-net", "Ethernet60", "sw-spine1-60"},
	}
	for _, tt := range tests {
		if got := swIfname(tt.dev, tt.port); got != tt.want {
			t.Errorf("swIfname(%q, %q) = %q, want %q", tt.dev, tt.port, got, tt.want)
		}
	}
}

func TestWmIfnameUniqueness(t *testing.T) {
	// spine1 and spine2 must produce different webmesh-side names
	a := wmIfname("spine1-linux", "Ethernet0")
	b := wmIfname("spine2-linux", "Ethernet0")
	if a == b {
		t.Errorf("wmIfname collision: both gave %q", a)
	}
	// leaf1 and leaf2 with same port
	c := wmIfname("leaf1-linux", "Ethernet0")
	d := wmIfname("leaf2-linux", "Ethernet0")
	if c == d {
		t.Errorf("wmIfname collision: both gave %q", c)
	}
}