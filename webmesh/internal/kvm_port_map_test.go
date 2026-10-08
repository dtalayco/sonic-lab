/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import (
	"testing"
)

func TestKernelIfname_EthernetToEth(t *testing.T) {
	tests := []struct {
		port string
		want string
	}{
		{"Ethernet0", "eth1"},
		{"Ethernet4", "eth2"},
		{"Ethernet8", "eth3"},
		{"Ethernet12", "eth4"},
		{"Ethernet16", "eth5"},
		{"Ethernet20", "eth6"},
		{"Ethernet24", "eth7"},
		{"Ethernet28", "eth8"},
		{"Ethernet32", "eth9"},
		{"Ethernet36", "eth10"},
		{"Ethernet40", "eth11"},
		{"Ethernet44", "eth12"},
		{"Ethernet48", "eth13"},
		{"Ethernet52", "eth14"},
		{"Ethernet56", "eth15"},
		{"Ethernet60", "eth16"},
		{"Ethernet64", "eth17"},
		{"Ethernet68", "eth18"},
		{"Ethernet72", "eth19"},
		{"Ethernet76", "eth20"},
		{"Ethernet80", "eth21"},
		{"Ethernet84", "eth22"},
		{"Ethernet88", "eth23"},
		{"Ethernet92", "eth24"},
		{"Ethernet96", "eth25"},
		{"Ethernet100", "eth26"},
		{"Ethernet104", "eth27"},
		{"Ethernet108", "eth28"},
		{"Ethernet112", "eth29"},
		{"Ethernet116", "eth30"},
	}
	for _, tt := range tests {
		t.Run(tt.port, func(t *testing.T) {
			got := kernelIfname(tt.port)
			if got != tt.want {
				t.Errorf("kernelIfname(%q) = %q, want %q", tt.port, got, tt.want)
			}
		})
	}
}

func TestKernelIfname_AlreadyEth(t *testing.T) {
	// Port names already in ethN form should pass through unchanged.
	tests := []string{"eth0", "eth1", "eth2", "eth30", "eth99"}
	for _, port := range tests {
		t.Run(port, func(t *testing.T) {
			got := kernelIfname(port)
			if got != port {
				t.Errorf("kernelIfname(%q) = %q, want passthrough %q", port, got, port)
			}
		})
	}
}

func TestKernelIfname_UnknownPort(t *testing.T) {
	// Unknown port names should pass through unchanged (with a warning).
	got := kernelIfname("Ethernet120")
	if got != "Ethernet120" {
		t.Errorf("kernelIfname(\"Ethernet120\") = %q, want passthrough", got)
	}
}

func TestKernelIfname_ReverseMapping(t *testing.T) {
	// Verify the mapping is consistent: each ethN maps to exactly one
	// EthernetN and vice versa (within the mapped range).
	seen := make(map[string]string)
	for logical, kernel := range kvmPortMap {
		if prev, ok := seen[kernel]; ok {
			t.Errorf("kernel %q maps to both %q and %q", kernel, prev, logical)
		}
		seen[kernel] = logical
	}
	if len(seen) != len(kvmPortMap) {
		t.Errorf("expected %d unique kernel names, got %d", len(kvmPortMap), len(seen))
	}
}