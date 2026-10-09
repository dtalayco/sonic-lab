/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package topo

import (
	"testing"
)

func mustGenerate(t *testing.T, opts Options) *Topology {
	t.Helper()
	tt, err := Generate(opts)
	if err != nil {
		t.Fatalf("Generate(%+v) error: %v", opts, err)
	}
	return tt
}

func TestSpineLeaf_Basic(t *testing.T) {
	tt := mustGenerate(t, Options{Spines: 1, Leaves: 2})

	if tt.Name != "spine-leaf-1x2" {
		t.Errorf("Name = %q, want %q", tt.Name, "spine-leaf-1x2")
	}
	if len(tt.Switches) != 3 {
		t.Fatalf("Switches = %d, want 3", len(tt.Switches))
	}
	if len(tt.Links) != 2 {
		t.Fatalf("Links = %d, want 2", len(tt.Links))
	}

	// Verify spine has 2 ports (one per leaf)
	var spine, leaf1, leaf2 *Switch
	for i := range tt.Switches {
		switch tt.Switches[i].Name {
		case "spine1":
			spine = &tt.Switches[i]
		case "leaf1":
			leaf1 = &tt.Switches[i]
		case "leaf2":
			leaf2 = &tt.Switches[i]
		}
	}

	if spine == nil || leaf1 == nil || leaf2 == nil {
		t.Fatal("missing expected switches")
	}

	if len(spine.Ports) != 2 {
		t.Errorf("spine1 ports = %d, want 2", len(spine.Ports))
	}
	if len(leaf1.Ports) != 1 {
		t.Errorf("leaf1 ports = %d, want 1", len(leaf1.Ports))
	}
	if len(leaf2.Ports) != 1 {
		t.Errorf("leaf2 ports = %d, want 1", len(leaf2.Ports))
	}

	// Verify ASNs
	if spine.ASN != 65000 {
		t.Errorf("spine1 ASN = %d, want 65000", spine.ASN)
	}
	if leaf1.ASN != 65100 {
		t.Errorf("leaf1 ASN = %d, want 65100", leaf1.ASN)
	}
	if leaf2.ASN != 65101 {
		t.Errorf("leaf2 ASN = %d, want 65101", leaf2.ASN)
	}
}

func TestSpineLeaf_DualSpine(t *testing.T) {
	tt := mustGenerate(t, Options{Spines: 2, Leaves: 3})

	if len(tt.Switches) != 5 {
		t.Fatalf("Switches = %d, want 5", len(tt.Switches))
	}
	if len(tt.Links) != 6 {
		t.Fatalf("Links = %d, want 6", len(tt.Links))
	}

	// Each leaf should have 2 ports (one per spine)
	for _, sw := range tt.Switches {
		if sw.Role == "leaf" && len(sw.Ports) != 2 {
			t.Errorf("%s ports = %d, want 2", sw.Name, len(sw.Ports))
		}
		if sw.Role == "spine" && len(sw.Ports) != 3 {
			t.Errorf("%s ports = %d, want 3", sw.Name, len(sw.Ports))
		}
	}
}

func TestSpineLeaf_PortNaming(t *testing.T) {
	tt := mustGenerate(t, Options{Spines: 1, Leaves: 3})

	var spine *Switch
	for i := range tt.Switches {
		if tt.Switches[i].Name == "spine1" {
			spine = &tt.Switches[i]
		}
	}

	wantPorts := []string{"Ethernet0", "Ethernet4", "Ethernet8"}
	if len(spine.Ports) != 3 {
		t.Fatalf("spine1 ports = %d, want 3", len(spine.Ports))
	}
	for i, want := range wantPorts {
		if spine.Ports[i] != want {
			t.Errorf("spine1.Ports[%d] = %q, want %q", i, spine.Ports[i], want)
		}
	}
}

func TestSpineLeaf_LinkEndpoints(t *testing.T) {
	tt := mustGenerate(t, Options{Spines: 1, Leaves: 2})

	// Link 0: spine1:Ethernet0 <-> leaf1:Ethernet0
	l := tt.Links[0]
	if l.A.Device != "spine1-linux" || l.A.Port != "Ethernet0" {
		t.Errorf("link[0].A = %s:%s, want spine1-linux:Ethernet0", l.A.Device, l.A.Port)
	}
	if l.B.Device != "leaf1-linux" || l.B.Port != "Ethernet0" {
		t.Errorf("link[0].B = %s:%s, want leaf1-linux:Ethernet0", l.B.Device, l.B.Port)
	}

	// Link 1: spine1:Ethernet4 <-> leaf2:Ethernet0
	l = tt.Links[1]
	if l.A.Device != "spine1-linux" || l.A.Port != "Ethernet4" {
		t.Errorf("link[1].A = %s:%s, want spine1-linux:Ethernet4", l.A.Device, l.A.Port)
	}
	if l.B.Device != "leaf2-linux" || l.B.Port != "Ethernet0" {
		t.Errorf("link[1].B = %s:%s, want leaf2-linux:Ethernet0", l.B.Device, l.B.Port)
	}
}

func TestMesh_Basic(t *testing.T) {
	tt := mustGenerate(t, Options{Mesh: true, Nodes: 3})

	if tt.Name != "mesh-3" {
		t.Errorf("Name = %q, want %q", tt.Name, "mesh-3")
	}
	if len(tt.Switches) != 3 {
		t.Fatalf("Switches = %d, want 3", len(tt.Switches))
	}
	if len(tt.Links) != 3 {
		t.Fatalf("Links = %d, want 3 (C(3,2))", len(tt.Links))
	}

	// Each node should have 2 ports
	for _, sw := range tt.Switches {
		if len(sw.Ports) != 2 {
			t.Errorf("%s ports = %d, want 2", sw.Name, len(sw.Ports))
		}
	}
}

func TestMesh_PortMapping(t *testing.T) {
	tt := mustGenerate(t, Options{Mesh: true, Nodes: 3})

	// Node1 (index 0) connects to:
	//   Node2 (index 1): port index (1-1)=0 → Ethernet0
	//   Node3 (index 2): port index (2-1)=1 → Ethernet4
	// Node2 (index 1) connects to:
	//   Node3 (index 2): port index (2-1)=1 → Ethernet4
	// Node3 (index 2) has no j>i

	// Link 0: node1:Ethernet0 <-> node2:Ethernet0
	l := tt.Links[0]
	if l.A.Device != "node1-linux" || l.A.Port != "Ethernet0" {
		t.Errorf("link[0].A = %s:%s, want node1-linux:Ethernet0", l.A.Device, l.A.Port)
	}
	if l.B.Device != "node2-linux" || l.B.Port != "Ethernet0" {
		t.Errorf("link[0].B = %s:%s, want node2-linux:Ethernet0", l.B.Device, l.B.Port)
	}

	// Link 1: node1:Ethernet4 <-> node3:Ethernet0
	l = tt.Links[1]
	if l.A.Device != "node1-linux" || l.A.Port != "Ethernet4" {
		t.Errorf("link[1].A = %s:%s, want node1-linux:Ethernet4", l.A.Device, l.A.Port)
	}
	if l.B.Device != "node3-linux" || l.B.Port != "Ethernet0" {
		t.Errorf("link[1].B = %s:%s, want node3-linux:Ethernet0", l.B.Device, l.B.Port)
	}

	// Link 2: node2:Ethernet4 <-> node3:Ethernet4
	l = tt.Links[2]
	if l.A.Device != "node2-linux" || l.A.Port != "Ethernet4" {
		t.Errorf("link[2].A = %s:%s, want node2-linux:Ethernet4", l.A.Device, l.A.Port)
	}
	if l.B.Device != "node3-linux" || l.B.Port != "Ethernet4" {
		t.Errorf("link[2].B = %s:%s, want node3-linux:Ethernet4", l.B.Device, l.B.Port)
	}
}

func TestMesh_LinksCount(t *testing.T) {
	// N nodes in full mesh → N*(N-1)/2 links
	for n := 2; n <= 5; n++ {
		tt := mustGenerate(t, Options{Mesh: true, Nodes: n})
		want := n * (n - 1) / 2
		if len(tt.Links) != want {
			t.Errorf("mesh(%d) links = %d, want %d", n, len(tt.Links), want)
		}
	}
}

func TestRing_Basic(t *testing.T) {
	tt := mustGenerate(t, Options{Ring: true, Nodes: 4})

	if tt.Name != "ring-4" {
		t.Errorf("Name = %q, want %q", tt.Name, "ring-4")
	}
	if len(tt.Switches) != 4 {
		t.Fatalf("Switches = %d, want 4", len(tt.Switches))
	}
	if len(tt.Links) != 4 {
		t.Fatalf("Links = %d, want 4", len(tt.Links))
	}

	// Each node has exactly 2 ports
	for _, sw := range tt.Switches {
		if len(sw.Ports) != 2 {
			t.Errorf("%s ports = %d, want 2", sw.Name, len(sw.Ports))
		}
		if sw.Ports[0] != "Ethernet0" || sw.Ports[1] != "Ethernet4" {
			t.Errorf("%s ports = %v, want [Ethernet0 Ethernet4]", sw.Name, sw.Ports)
		}
	}
}

func TestRing_Wraparound(t *testing.T) {
	tt := mustGenerate(t, Options{Ring: true, Nodes: 4})

	// Last link should be node4:Ethernet4 <-> node1:Ethernet0 (wraparound)
	last := tt.Links[len(tt.Links)-1]
	if last.A.Device != "node4-linux" || last.A.Port != "Ethernet4" {
		t.Errorf("last link A = %s:%s, want node4-linux:Ethernet4", last.A.Device, last.A.Port)
	}
	if last.B.Device != "node1-linux" || last.B.Port != "Ethernet0" {
		t.Errorf("last link B = %s:%s, want node1-linux:Ethernet0", last.B.Device, last.B.Port)
	}
}

func TestMesh_TooSmall(t *testing.T) {
	_, err := Generate(Options{Mesh: true, Nodes: 1})
	if err == nil {
		t.Error("expected error for mesh with 1 node")
	}
}

func TestRing_TooSmall(t *testing.T) {
	_, err := Generate(Options{Ring: true, Nodes: 2})
	if err == nil {
		t.Error("expected error for ring with 2 nodes")
	}
}

func TestSpineLeaf_NoSpines(t *testing.T) {
	_, err := Generate(Options{Spines: 0, Leaves: 3})
	if err == nil {
		t.Error("expected error for 0 spines")
	}
}

func TestSpineLeaf_NoLeaves(t *testing.T) {
	_, err := Generate(Options{Spines: 2, Leaves: 0})
	if err == nil {
		t.Error("expected error for 0 leaves")
	}
}

func TestSpineLeaf_ExceedsMaxPorts(t *testing.T) {
	_, err := Generate(Options{Spines: 31, Leaves: 1})
	if err == nil {
		t.Error("expected error for 31 spines (exceeds 30 port limit)")
	}
}

func TestMesh_ExceedsMaxPorts(t *testing.T) {
	_, err := Generate(Options{Mesh: true, Nodes: 32})
	if err == nil {
		t.Error("expected error for mesh with 32 nodes (needs 31 ports > 30 max)")
	}
}

func TestPortName(t *testing.T) {
	tests := []struct {
		index int
		want  string
	}{
		{0, "Ethernet0"},
		{1, "Ethernet4"},
		{2, "Ethernet8"},
		{7, "Ethernet28"},
		{29, "Ethernet116"},
	}
	for _, tc := range tests {
		if got := portName(tc.index); got != tc.want {
			t.Errorf("portName(%d) = %q, want %q", tc.index, got, tc.want)
		}
	}
}
func TestHosts_Basic(t *testing.T) {
	// 2 spines, 1 leaf, 1 host per switch (3 total hosts)
	tt := mustGenerate(t, Options{Spines: 2, Leaves: 1, HostsPerSwitch: 1})

	if len(tt.Hosts) != 3 {
		t.Fatalf("Hosts = %d, want 3", len(tt.Hosts))
	}
	// 2 switch-to-switch links (2 spines × 1 leaf) + 3 host links = 5 total
	if len(tt.Links) != 5 {
		t.Fatalf("Links = %d, want 5", len(tt.Links))
	}

	// Each host has 1 port
	for _, h := range tt.Hosts {
		if len(h.Ports) != 1 {
			t.Errorf("%s ports = %d, want 1", h.Name, len(h.Ports))
		}
		if h.Ports[0] != "Ethernet0" {
			t.Errorf("%s port = %q, want Ethernet0", h.Name, h.Ports[0])
		}
	}
}

func TestHosts_Naming(t *testing.T) {
	tt := mustGenerate(t, Options{Spines: 2, Leaves: 1, HostsPerSwitch: 2})

	// Total hosts = 2 per switch * 3 switches = 6
	if len(tt.Hosts) != 6 {
		t.Fatalf("Hosts = %d, want 6", len(tt.Hosts))
	}
	wantNames := []string{"host1", "host2", "host3", "host4", "host5", "host6"}
	for i, want := range wantNames {
		if tt.Hosts[i].Name != want {
			t.Errorf("Hosts[%d].Name = %q, want %q", i, tt.Hosts[i].Name, want)
		}
	}
}

func TestHosts_MultiPort(t *testing.T) {
	// 2 spines, 1 leaf, 2 hosts per switch, 2 ports per host
	tt := mustGenerate(t, Options{Spines: 2, Leaves: 1, HostsPerSwitch: 2, PortsPerHost: 2})

	// Total hosts = 2 * 3 = 6
	if len(tt.Hosts) != 6 {
		t.Fatalf("Hosts = %d, want 6", len(tt.Hosts))
	}
	// Each host has 2 ports
	for _, h := range tt.Hosts {
		if len(h.Ports) != 2 {
			t.Errorf("%s ports = %d, want 2", h.Name, len(h.Ports))
		}
		if h.Ports[0] != "Ethernet0" || h.Ports[1] != "Ethernet4" {
			t.Errorf("%s ports = %v, want [Ethernet0 Ethernet4]", h.Name, h.Ports)
		}
	}
	// Links: 2 switch-switch (2 spines × 1 leaf) + 6 hosts × 2 ports = 14
	if len(tt.Links) != 14 {
		t.Fatalf("Links = %d, want 14", len(tt.Links))
	}
}

func TestHosts_PortMapping(t *testing.T) {
	// 2 spines, 1 leaf, 1 host per switch
	// Switch order: spine1(0), spine2(1), leaf1(2)
	// host0: home=spine1(0), port0→spine1
	// host1: home=spine2(1), port0→spine2
	// host2: home=leaf1(2), port0→leaf1
	tt := mustGenerate(t, Options{Spines: 2, Leaves: 1, HostsPerSwitch: 1})

	// Find host links (links where one endpoint is a host)
	var hostLinks []Link
	for _, l := range tt.Links {
		if isHostEndpoint(l.A) || isHostEndpoint(l.B) {
			hostLinks = append(hostLinks, l)
		}
	}
	if len(hostLinks) != 3 {
		t.Fatalf("host links = %d, want 3", len(hostLinks))
	}

	// host0 → spine1-linux
	if hostLinks[0].B.Device != "host1" {
		t.Errorf("hostLinks[0].B.Device = %q, want host1", hostLinks[0].B.Device)
	}
	if hostLinks[0].A.Device != "spine1-linux" {
		t.Errorf("hostLinks[0].A.Device = %q, want spine1-linux", hostLinks[0].A.Device)
	}

	// host1 → spine2-linux
	if hostLinks[1].B.Device != "host2" {
		t.Errorf("hostLinks[1].B.Device = %q, want host2", hostLinks[1].B.Device)
	}
	if hostLinks[1].A.Device != "spine2-linux" {
		t.Errorf("hostLinks[1].A.Device = %q, want spine2-linux", hostLinks[1].A.Device)
	}

	// host2 → leaf1-linux
	if hostLinks[2].B.Device != "host3" {
		t.Errorf("hostLinks[2].B.Device = %q, want host3", hostLinks[2].B.Device)
	}
	if hostLinks[2].A.Device != "leaf1-linux" {
		t.Errorf("hostLinks[2].A.Device = %q, want leaf1-linux", hostLinks[2].A.Device)
	}
}

func TestHosts_Redundancy(t *testing.T) {
	// 2 spines, 2 leaves, 1 host per switch, 2 ports per host
	// host0: home=spine1(0), port0→spine1(0), port1→spine2(1)
	tt := mustGenerate(t, Options{Spines: 2, Leaves: 2, HostsPerSwitch: 1, PortsPerHost: 2})

	// 4 switches * 1 host * 2 ports = 8 host links
	var hostLinks []Link
	for _, l := range tt.Links {
		if isHostEndpoint(l.A) || isHostEndpoint(l.B) {
			hostLinks = append(hostLinks, l)
		}
	}
	if len(hostLinks) != 8 {
		t.Fatalf("host links = %d, want 8", len(hostLinks))
	}

	// Verify host1 (index 0, home=spine1) connects to spine1 and spine2
	var h1Links []Link
	for _, l := range hostLinks {
		if l.A.Device == "host1" || l.B.Device == "host1" {
			h1Links = append(h1Links, l)
		}
	}
	if len(h1Links) != 2 {
		t.Fatalf("host1 links = %d, want 2", len(h1Links))
	}
	seen := map[string]bool{}
	for _, l := range h1Links {
		other := l.A.Device
		if l.A.Device == "host1" {
			other = l.B.Device
		}
		seen[other] = true
	}
	if !seen["spine1-linux"] {
		t.Error("host1 should connect to spine1-linux")
	}
	if !seen["spine2-linux"] {
		t.Error("host1 should connect to spine2-linux")
	}
}

func TestHosts_NoHosts(t *testing.T) {
	tt := mustGenerate(t, Options{Spines: 2, Leaves: 2})
	if len(tt.Hosts) != 0 {
		t.Errorf("Hosts = %d, want 0", len(tt.Hosts))
	}
}

func TestHosts_SwitchPortAllocation(t *testing.T) {
	// 1 spine, 2 leaves, 1 host per switch
	// spine1 has 2 switch ports (for 2 leaves) + 1 host port = 3 total
	// leaf1 has 1 switch port (for 1 spine) + 1 host port = 2 total
	// leaf2 has 1 switch port (for 1 spine) + 1 host port = 2 total
	tt := mustGenerate(t, Options{Spines: 1, Leaves: 2, HostsPerSwitch: 1})

	for _, sw := range tt.Switches {
		switch sw.Name {
		case "spine1":
			if len(sw.Ports) != 3 {
				t.Errorf("spine1 ports = %d, want 3 (2 switch + 1 host)", len(sw.Ports))
			}
			if sw.Ports[2] != "Ethernet8" {
				t.Errorf("spine1.Ports[2] = %q, want Ethernet8", sw.Ports[2])
			}
		case "leaf1":
			if len(sw.Ports) != 2 {
				t.Errorf("leaf1 ports = %d, want 2 (1 switch + 1 host)", len(sw.Ports))
			}
			if sw.Ports[1] != "Ethernet4" {
				t.Errorf("leaf1.Ports[1] = %q, want Ethernet4", sw.Ports[1])
			}
		case "leaf2":
			if len(sw.Ports) != 2 {
				t.Errorf("leaf2 ports = %d, want 2 (1 switch + 1 host)", len(sw.Ports))
			}
			if sw.Ports[1] != "Ethernet4" {
				t.Errorf("leaf2.Ports[1] = %q, want Ethernet4", sw.Ports[1])
			}
		}
	}
}

// isHostEndpoint returns true if the endpoint's device name starts with "host".
func isHostEndpoint(e Endpoint) bool {
	return len(e.Device) > 4 && e.Device[:4] == "host"
}
