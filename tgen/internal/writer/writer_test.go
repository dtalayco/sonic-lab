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
		{"Ethernet0", "Ethernet0"},
		{"Ethernet4", "Ethernet4"},
		{"Ethernet8", "Ethernet8"},
		{"Ethernet28", "Ethernet28"},
		{"Ethernet116", "Ethernet116"},
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

	// Should have peer-group definition
	if !strings.Contains(cfg, "neighbor TRANSIT peer-group") {
		t.Errorf("spine1 config missing 'neighbor TRANSIT peer-group'")
	}
	if !strings.Contains(cfg, "neighbor TRANSIT remote-as external") {
		t.Errorf("spine1 config missing 'neighbor TRANSIT remote-as external'")
	}
	if !strings.Contains(cfg, "neighbor TRANSIT ebgp-multihop") {
		t.Errorf("spine1 config missing 'neighbor TRANSIT ebgp-multihop'")
	}

	// Should have unnumbered neighbors via interface keyword
	if !strings.Contains(cfg, "neighbor Ethernet0 interface peer-group TRANSIT") {
		t.Errorf("spine1 config missing 'neighbor Ethernet0 interface peer-group TRANSIT'")
	}
	if !strings.Contains(cfg, "neighbor Ethernet4 interface peer-group TRANSIT") {
		t.Errorf("spine1 config missing 'neighbor Ethernet4 interface peer-group TRANSIT'")
	}

	// Should have BGP tuning statements
	if !strings.Contains(cfg, "no bgp ebgp-requires-policy") {
		t.Errorf("spine1 config missing 'no bgp ebgp-requires-policy'")
	}
	if !strings.Contains(cfg, "bgp disable-ebgp-connected-route-check") {
		t.Errorf("spine1 config missing 'bgp disable-ebgp-connected-route-check'")
	}
	if !strings.Contains(cfg, "bgp bestpath as-path multipath-relax") {
		t.Errorf("spine1 config missing 'bgp bestpath as-path multipath-relax'")
	}

	// Should have IPv6 address family with activate and next-hop-self
	if !strings.Contains(cfg, "address-family ipv6") {
		t.Errorf("spine1 config missing 'address-family ipv6'")
	}
	if !strings.Contains(cfg, "neighbor TRANSIT activate") {
		t.Errorf("spine1 config missing 'neighbor TRANSIT activate'")
	}
	if !strings.Contains(cfg, "neighbor TRANSIT next-hop-self") {
		t.Errorf("spine1 config missing 'neighbor TRANSIT next-hop-self'")
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

	// Leaf1 connects to spine1 (Ethernet0) and spine2 (Ethernet4)
	if !strings.Contains(cfg, "neighbor Ethernet0 interface peer-group TRANSIT") {
		t.Errorf("leaf1 config missing 'neighbor Ethernet0 interface peer-group TRANSIT'")
	}
	if !strings.Contains(cfg, "neighbor Ethernet4 interface peer-group TRANSIT") {
		t.Errorf("leaf1 config missing 'neighbor Ethernet4 interface peer-group TRANSIT'")
	}

	// Should have correct VTEP address for leaf1 (ASN 65100 = 0xFE4C)
	if !strings.Contains(cfg, "fd00:0:0:fe4c::1/128") {
		t.Errorf("leaf1 config missing VTEP address fd00:0:0:fe4c::1/128")
	}
}

func TestGenerateFRRConfig_Mesh(t *testing.T) {
	tt := mustGenerate(t, topo.Options{Mesh: true, Nodes: 3})
	w := New("/tmp/test", tt)

	// node1 connects to node2 (Ethernet0) and node3 (Ethernet4)
	var node1 *topo.Switch
	for i := range tt.Switches {
		if tt.Switches[i].Name == "node1" {
			node1 = &tt.Switches[i]
		}
	}
	cfg := w.generateFRRConfig(*node1)

	if !strings.Contains(cfg, "neighbor Ethernet0 interface peer-group TRANSIT") {
		t.Errorf("node1 config missing 'neighbor Ethernet0 interface peer-group TRANSIT' (node2)")
	}
	if !strings.Contains(cfg, "neighbor Ethernet4 interface peer-group TRANSIT") {
		t.Errorf("node1 config missing 'neighbor Ethernet4 interface peer-group TRANSIT' (node3)")
	}
}

func TestGenerateFRRConfig_Ring(t *testing.T) {
	tt := mustGenerate(t, topo.Options{Ring: true, Nodes: 3})
	w := New("/tmp/test", tt)

	// node1:Ethernet0 (connects to node3) and Ethernet4 (connects to node2)
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

	// node1 should have exactly 2 neighbors (ring topology)
	if !strings.Contains(cfg, "neighbor Ethernet0 interface peer-group TRANSIT") {
		t.Errorf("node1 config missing 'neighbor Ethernet0 interface peer-group TRANSIT' (node3 via ring wrap)")
	}
	if !strings.Contains(cfg, "neighbor Ethernet4 interface peer-group TRANSIT") {
		t.Errorf("node1 config missing 'neighbor Ethernet4 interface peer-group TRANSIT' (node2)")
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
	// The FRR config should only include the leaf1 neighbor (Ethernet0), not the host link (Ethernet4)
	if !strings.Contains(cfg, "neighbor Ethernet0 interface peer-group TRANSIT") {
		t.Errorf("spine1 config missing 'neighbor Ethernet0 interface peer-group TRANSIT' (leaf1)")
	}
	if strings.Contains(cfg, "neighbor Ethernet4") {
		t.Errorf("spine1 config should not include host link on Ethernet4")
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

func TestSRv6_FRRConfig(t *testing.T) {
	tt := mustGenerate(t, topo.Options{Spines: 1, Leaves: 2, SRv6: true})
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

	// Verify SRv6 section is present
	if !strings.Contains(cfg, "segment-routing") {
		t.Error("missing 'segment-routing' section")
	}
	if !strings.Contains(cfg, "srv6") {
		t.Error("missing 'srv6' sub-section")
	}
	if !strings.Contains(cfg, "locator spine1") {
		t.Error("missing 'locator spine1'")
	}
	if !strings.Contains(cfg, "prefix fd00:0:0:fde8::/64") {
		t.Error("missing locator prefix fd00:0:0:fde8::/64")
	}
	if !strings.Contains(cfg, "behavior uN") {
		t.Error("missing 'behavior uN' (End SID)")
	}
	if !strings.Contains(cfg, "segment-routing srv6") {
		t.Error("missing 'segment-routing srv6' in BGP section")
	}
}

func TestSRv6_FRRConfig_Disabled(t *testing.T) {
	tt := mustGenerate(t, topo.Options{Spines: 1, Leaves: 2, SRv6: false})
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

	// Verify SRv6 section is NOT present
	if strings.Contains(cfg, "segment-routing") {
		t.Error("should not contain 'segment-routing' when SRv6 is disabled")
	}
}

func TestSRv6_Routes(t *testing.T) {
	tt := mustGenerate(t, topo.Options{Spines: 1, Leaves: 2, SRv6: true})
	dir := t.TempDir()
	w := New(dir, tt)

	if err := w.writeSRv6Routes(); err != nil {
		t.Fatalf("writeSRv6Routes error: %v", err)
	}

	// Verify route file exists for spine1
	data, err := os.ReadFile(filepath.Join(dir, "srv6", "spine1.routes"))
	if err != nil {
		t.Fatal(err)
	}
	routes := string(data)

	// Verify overlay prefix is added to Loopback0
	if !strings.Contains(routes, "2001:db8:fde8::1/128") {
		t.Error("missing overlay prefix /128 on Loopback0")
	}

	// Verify seg6 routes to remote VTEPs
	if !strings.Contains(routes, "encap seg6 mode encap segs") {
		t.Error("missing seg6 encap syntax")
	}
	// spine1 (ASN 65000) should have routes to leaf1 (ASN 65100) and leaf2 (ASN 65101)
	if !strings.Contains(routes, "2001:db8:fe4c::/64") {
		t.Error("missing route to leaf1 overlay prefix")
	}
	if !strings.Contains(routes, "fd00:0:0:fe4c::1") {
		t.Error("missing leaf1 VTEP as SRH segment")
	}
	if !strings.Contains(routes, "2001:db8:fe4d::/64") {
		t.Error("missing route to leaf2 overlay prefix")
	}
	if !strings.Contains(routes, "fd00:0:0:fe4d::1") {
		t.Error("missing leaf2 VTEP as SRH segment")
	}
	// Should NOT have a route to itself
	if strings.Contains(routes, "2001:db8:fde8::/64 encap") {
		t.Error("should not have seg6 route to own overlay prefix")
	}
}

func TestSRv6_Routes_Mesh(t *testing.T) {
	tt := mustGenerate(t, topo.Options{Mesh: true, Nodes: 3, SRv6: true})
	dir := t.TempDir()
	w := New(dir, tt)

	if err := w.writeSRv6Routes(); err != nil {
		t.Fatalf("writeSRv6Routes error: %v", err)
	}

	// In a mesh of 3, node1 should have routes to node2 and node3
	data, err := os.ReadFile(filepath.Join(dir, "srv6", "node1.routes"))
	if err != nil {
		t.Fatal(err)
	}
	routes := string(data)

	// node1 ASN = 65000, node2 ASN = 65001, node3 ASN = 65002
	if !strings.Contains(routes, "fd00:0:0:fde9::1") {
		t.Error("missing node2 VTEP as SRH segment")
	}
	if !strings.Contains(routes, "fd00:0:0:fdea::1") {
		t.Error("missing node3 VTEP as SRH segment")
	}
	// Should not route to itself
	if strings.Contains(routes, "fd00:0:0:fde8::1 encap") {
		t.Error("should not have seg6 route to own VTEP")
	}
}

func TestSRv6_ComposeMount(t *testing.T) {
	tt := mustGenerate(t, topo.Options{Spines: 1, Leaves: 1, SRv6: true})
	dir := t.TempDir()
	w := New(dir, tt)

	if err := w.writeCompose(); err != nil {
		t.Fatalf("writeCompose error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	compose := string(data)

	if !strings.Contains(compose, "./srv6/spine1.routes:/srv6.routes:ro") {
		t.Error("missing srv6 routes volume mount for spine1")
	}
	if !strings.Contains(compose, "./srv6/leaf1.routes:/srv6.routes:ro") {
		t.Error("missing srv6 routes volume mount for leaf1")
	}
}

func TestSRv6_ComposeMount_Disabled(t *testing.T) {
	tt := mustGenerate(t, topo.Options{Spines: 1, Leaves: 1, SRv6: false})
	dir := t.TempDir()
	w := New(dir, tt)

	if err := w.writeCompose(); err != nil {
		t.Fatalf("writeCompose error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	compose := string(data)

	if strings.Contains(compose, "./srv6/") {
		t.Error("should not contain srv6 volume mounts when SRv6 is disabled")
	}
}

func TestSRv6_EntryScript(t *testing.T) {
	tt := mustGenerate(t, topo.Options{Spines: 1, Leaves: 1, SRv6: true})
	dir := t.TempDir()
	w := New(dir, tt)

	if err := w.writeEntryScript(); err != nil {
		t.Fatalf("writeEntryScript error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "sonic_entry.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)

	// The entry script should conditionally execute /srv6.routes
	if !strings.Contains(script, "/srv6.routes") {
		t.Error("missing /srv6.routes execution in entry script")
	}
}