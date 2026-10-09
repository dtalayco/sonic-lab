/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package writer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sonic-lab/tgen/internal/topo"
)

func TestKernelPortName(t *testing.T) {
	tests := []struct {
		port string
		want string
	}{
		{"Ethernet0", "eth0"},
		{"Ethernet4", "eth1"},
		{"Ethernet8", "eth2"},
		{"Ethernet28", "eth7"},
		{"Ethernet116", "eth29"},
	}
	for _, tc := range tests {
		if got := kernelPortName(tc.port); got != tc.want {
			t.Errorf("kernelPortName(%q) = %q, want %q", tc.port, got, tc.want)
		}
	}
}

func mustGenerate(t *testing.T, opts topo.Options) *topo.Topology {
	t.Helper()
	tt, err := topo.Generate(opts)
	if err != nil {
		t.Fatalf("Generate(%+v) error: %v", opts, err)
	}
	return tt
}

func TestGenerateFRRConfig_SpineLeaf(t *testing.T) {
	tt := mustGenerate(t, topo.Options{Spines: 1, Leaves: 2})
	w := New("/tmp/test", tt)

	// Test spine1 config
	var spine *topo.Switch
	for i := range tt.Switches {
		if tt.Switches[i].Name == "spine1" {
			spine = &tt.Switches[i]
		}
	}
	if spine == nil {
		t.Fatal("spine1 not found")
	}

	cfg := w.generateFRRConfig(*spine)

	// Should contain the router bgp statement with correct ASN
	if !strings.Contains(cfg, "router bgp 65000") {
		t.Errorf("spine1 config missing 'router bgp 65000'")
	}

	// Should contain the VTEP address (ASN 65000 = 0xFDE8)
	if !strings.Contains(cfg, "fd00:0:0:fde8::1/128") {
		t.Errorf("spine1 config missing VTEP address")
	}

	// Should have BGP unnumbered neighbor for leaf1 (eth0) and leaf2 (eth1)
	if !strings.Contains(cfg, "neighbor eth0 remote-as 65100") {
		t.Errorf("spine1 config missing 'neighbor eth0 remote-as 65100'")
	}
	if !strings.Contains(cfg, "neighbor eth0 unnumbered") {
		t.Errorf("spine1 config missing 'neighbor eth0 unnumbered'")
	}
	if !strings.Contains(cfg, "neighbor eth1 remote-as 65101") {
		t.Errorf("spine1 config missing 'neighbor eth1 remote-as 65101'")
	}
	if !strings.Contains(cfg, "neighbor eth1 unnumbered") {
		t.Errorf("spine1 config missing 'neighbor eth1 unnumbered'")
	}

	// Should have IPv6 address family with activate and network
	if !strings.Contains(cfg, "address-family ipv6") {
		t.Errorf("spine1 config missing 'address-family ipv6'")
	}
	if !strings.Contains(cfg, "neighbor eth0 activate") {
		t.Errorf("spine1 config missing 'neighbor eth0 activate'")
	}
	if !strings.Contains(cfg, "network fd00:0:0:fde8::1/128") {
		t.Errorf("spine1 config missing network statement")
	}
}

func TestGenerateFRRConfig_Leaf(t *testing.T) {
	tt := mustGenerate(t, topo.Options{Spines: 2, Leaves: 1})
	w := New("/tmp/test", tt)

	var leaf *topo.Switch
	for i := range tt.Switches {
		if tt.Switches[i].Name == "leaf1" {
			leaf = &tt.Switches[i]
		}
	}
	if leaf == nil {
		t.Fatal("leaf1 not found")
	}

	cfg := w.generateFRRConfig(*leaf)

	// Leaf1 connects to spine1 (eth0) and spine2 (eth1)
	if !strings.Contains(cfg, "neighbor eth0 remote-as 65000") {
		t.Errorf("leaf1 config missing 'neighbor eth0 remote-as 65000'")
	}
	if !strings.Contains(cfg, "neighbor eth1 remote-as 65001") {
		t.Errorf("leaf1 config missing 'neighbor eth1 remote-as 65001'")
	}

	// Should have correct VTEP address for leaf1 (ASN 65100 = 0xFE4C)
	if !strings.Contains(cfg, "fd00:0:0:fe4c::1/128") {
		t.Errorf("leaf1 config missing VTEP address fd00:0:0:fe4c::1/128")
	}
}

func TestGenerateFRRConfig_Mesh(t *testing.T) {
	tt := mustGenerate(t, topo.Options{Mesh: true, Nodes: 3})
	w := New("/tmp/test", tt)

	// node1 connects to node2 (eth0) and node3 (eth1)
	var node1 *topo.Switch
	for i := range tt.Switches {
		if tt.Switches[i].Name == "node1" {
			node1 = &tt.Switches[i]
		}
	}
	cfg := w.generateFRRConfig(*node1)

	if !strings.Contains(cfg, "neighbor eth0 remote-as 65001") {
		t.Errorf("node1 config missing 'neighbor eth0 remote-as 65001' (node2)")
	}
	if !strings.Contains(cfg, "neighbor eth1 remote-as 65002") {
		t.Errorf("node1 config missing 'neighbor eth1 remote-as 65002' (node3)")
	}
}

func TestGenerateFRRConfig_Ring(t *testing.T) {
	tt := mustGenerate(t, topo.Options{Ring: true, Nodes: 3})
	w := New("/tmp/test", tt)

	// node1 connects to node2 (port Ethernet4 = eth1, "next") and node3 (port Ethernet0 = eth0, "prev")
	// Ring link: node[i]:Ethernet4 → node[(i+1)%n]:Ethernet0
	// node1:Ethernet4 → node2:Ethernet0
	// node3:Ethernet4 → node1:Ethernet0
	// So node1 has: Ethernet0 (connects to node3) and Ethernet4 (connects to node2)

	var node1 *topo.Switch
	for i := range tt.Switches {
		if tt.Switches[i].Name == "node1" {
			node1 = &tt.Switches[i]
		}
	}
	cfg := w.generateFRRConfig(*node1)

	// node1:Ethernet0 (eth0) connects to node3 (from ring wraparound: node3:Ethernet4 → node1:Ethernet0)
	// node1:Ethernet4 (eth1) connects to node2 (from node1:Ethernet4 → node2:Ethernet0)
	if !strings.Contains(cfg, "neighbor eth0 remote-as 65002") {
		t.Errorf("node1 config missing 'neighbor eth0 remote-as 65002' (node3 via ring wrap)")
	}
	if !strings.Contains(cfg, "neighbor eth1 remote-as 65001") {
		t.Errorf("node1 config missing 'neighbor eth1 remote-as 65001' (node2)")
	}
}

func TestGenerateFRRConfig_ExcludesHostLinks(t *testing.T) {
	// 1 spine, 1 leaf, 1 host per switch
	tt := mustGenerate(t, topo.Options{Spines: 1, Leaves: 1, HostsPerSwitch: 1})
	w := New("/tmp/test", tt)

	var spine *topo.Switch
	for i := range tt.Switches {
		if tt.Switches[i].Name == "spine1" {
			spine = &tt.Switches[i]
		}
	}

	cfg := w.generateFRRConfig(*spine)

	// spine1 has 2 ports: Ethernet0 (to leaf1) and Ethernet4 (to host1)
	// The FRR config should only include the leaf1 neighbor (eth0), not the host link (eth1)
	if !strings.Contains(cfg, "neighbor eth0 remote-as 65100") {
		t.Errorf("spine1 config missing 'neighbor eth0 remote-as 65100' (leaf1)")
	}
	if strings.Contains(cfg, "neighbor eth1") {
		t.Errorf("spine1 config should not include host link on eth1")
	}
}

func TestWriteFRRConfigs_CreatesFiles(t *testing.T) {
	tt := mustGenerate(t, topo.Options{Spines: 1, Leaves: 2})
	dir := t.TempDir()
	w := New(dir, tt)

	if err := w.writeFRRConfigs(); err != nil {
		t.Fatalf("writeFRRConfigs error: %v", err)
	}

	// Check that each switch has a config file
	for _, sw := range tt.Switches {
		path := filepath.Join(dir, "frr", sw.Name+".conf")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		if len(data) == 0 {
			t.Errorf("%s: config file is empty", sw.Name)
		}
	}
}

func TestWriteFRRConfigs_Content(t *testing.T) {
	tt := mustGenerate(t, topo.Options{Spines: 1, Leaves: 1})
	dir := t.TempDir()
	w := New(dir, tt)

	if err := w.writeFRRConfigs(); err != nil {
		t.Fatalf("writeFRRConfigs error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "frr", "spine1.conf"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(data)

	// Verify key sections
	if !strings.Contains(cfg, "frr defaults traditional") {
		t.Error("missing 'frr defaults traditional'")
	}
	if !strings.Contains(cfg, "interface Loopback0") {
		t.Error("missing 'interface Loopback0'")
	}
	if !strings.Contains(cfg, "router bgp 65000") {
		t.Error("missing 'router bgp 65000'")
	}
	if strings.Contains(cfg, "bgp router-id") {
		t.Error("should not contain 'bgp router-id' (invalid for IPv6)")
	}
	if !strings.Contains(cfg, "line vty") {
		t.Error("missing 'line vty'")
	}
}