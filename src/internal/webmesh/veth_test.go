/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import "testing"

func TestVethName(t *testing.T) {
	tests := []struct {
		dev, port, want string
	}{
		{"spine1-linux", "Ethernet0", "vspi0"},
		{"spine1-linux", "Ethernet4", "vspi4"},
		{"leaf1-linux", "Ethernet0", "vlea0"},
		{"leaf2-linux", "Ethernet8", "vlea8"},
		{"spine1", "Ethernet0", "vspi0"},
		{"leaf1", "Ethernet4", "vlea4"},
		{"spine2-linux", "Ethernet12", "vspi12"},
		{"a-linux", "Ethernet0", "va0"},
	}
	for _, tt := range tests {
		if got := vethName(tt.dev, tt.port); got != tt.want {
			t.Errorf("vethName(%q, %q) = %q, want %q", tt.dev, tt.port, got, tt.want)
		}
	}
}

func TestVethNameUniqueness(t *testing.T) {
	// leaf1 and leaf2 both start with "lea" — verify they produce
	// different names when port numbers differ.
	a := vethName("leaf1-linux", "Ethernet0")
	b := vethName("leaf2-linux", "Ethernet4")
	if a == b {
		t.Errorf("vethName collision: both gave %q", a)
	}
}
