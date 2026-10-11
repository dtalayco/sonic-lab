/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

// Package topo generates network topologies from simple parameters.
// It supports three topology types:
//
//   - Spine-leaf: S spines, L leaves, every leaf uplinks to every spine
//   - Full mesh: N nodes, every node connected to every other node
//   - Ring: N nodes connected in a circular loop
//
// Hosts can be attached to any topology via the HostsPerSwitch and
// PortsPerHost options.
package topo

import (
	"fmt"
)

// Topology is the generated network topology.
type Topology struct {
	Name     string
	Switches []Switch
	Hosts    []Host
	Links    []Link
	SRv6     bool // enable SRv6 overlay generation
}

// Switch represents a single SONiC virtual switch.
type Switch struct {
	Name          string
	Role          string // "spine", "leaf", or "node"
	ASN           int
	VtepAddr      string // VTEP IPv6 ULA loopback address (e.g. fd00:0:0:fd98::1/128)
	OverlayPrefix string // SRv6 overlay service prefix (e.g. 2001:db8:fd98::/64)
	Ports         []string
}

// Host represents a simulated host container attached to one or more switches.
type Host struct {
	Name  string
	Ports []string
}

// Link represents a connection between two switch ports.
type Link struct {
	A Endpoint
	B Endpoint
}

// Endpoint identifies a specific port on a specific device.
type Endpoint struct {
	Device string
	Port   string
}

// Options controls topology generation.
type Options struct {
	Spines          int  // -s: number of spine switches
	Leaves          int  // -l: number of leaf switches
	Mesh            bool // -m: generate full mesh topology
	Ring            bool // -r: generate ring topology
	Nodes           int  // -n: number of nodes (for mesh/ring)
	Links           int  // -links: parallel links per switch pair (default 1)
	HostsPerSwitch  int  // -h: number of hosts attached to each switch
	PortsPerHost    int  // -p: number of ports per host (default 1)
	SRv6            bool // -srv6: enable SRv6 overlay generation
}

// MaxPorts is the maximum number of ports available per switch
// (KVM platform maps 30 lanes: Ethernet0 through Ethernet116, step 4).
const MaxPorts = 30

// portName returns the Ethernet interface name for a 0-based port index.
// Ports use step-4 naming to match the KVM lane map.
func portName(index int) string {
	return fmt.Sprintf("Ethernet%d", index*4)
}

// VtepAddrForASN returns a deterministic VTEP IPv6 ULA loopback address
// derived from the switch's ASN. Scheme: fd00:0:0:<asn>::1/128.
// Only 2-byte ASNs (0-65535) are supported, which covers all lab use cases.
func VtepAddrForASN(asn int) string {
	return fmt.Sprintf("fd00:0:0:%x::1/128", asn)
}

// LocatorPrefixForASN returns the SRv6 locator /64 prefix derived from the
// switch's ASN. Scheme: fd00:0:0:<asn>::/64.
func LocatorPrefixForASN(asn int) string {
	return fmt.Sprintf("fd00:0:0:%x::/64", asn)
}

// OverlayPrefixForASN returns the SRv6 overlay service /64 prefix derived
// from the switch's ASN. Uses IPv6 documentation space (2001:db8::/32).
// Scheme: 2001:db8:<asn>::/64.
func OverlayPrefixForASN(asn int) string {
	return fmt.Sprintf("2001:db8:%x::/64", asn)
}

// VtepBareAddr returns the VTEP address without the /128 prefix length.
// E.g. "fd00:0:0:fde8::1/128" → "fd00:0:0:fde8::1"
func VtepBareAddr(vtepAddr string) string {
	return vtepAddr[:len(vtepAddr)-4] // strip "/128"
}

// SidAddrForASN returns the SRv6 End SID (uN behavior) as a bare IPv6
// address derived from the switch's ASN. Scheme: fd00:0:0:<asn>::100.
// This is deliberately distinct from the VTEP address (::1) to avoid a
// collision with the loopback interface address. The SID must be a
// locally-assigned address for the kernel to perform seg6 decapsulation.
func SidAddrForASN(asn int) string {
	return fmt.Sprintf("fd00:0:0:%x::100", asn)
}

// Generate builds a Topology from the given Options.
func Generate(opts Options) (*Topology, error) {
	// Default to 1 link per pair if not specified
	if opts.Links < 1 {
		opts.Links = 1
	}

	var t *Topology

	switch {
	case opts.Mesh:
		if opts.Nodes < 2 {
			return nil, fmt.Errorf("mesh requires at least 2 nodes (got %d)", opts.Nodes)
		}
		if (opts.Nodes-1)*opts.Links > MaxPorts {
			return nil, fmt.Errorf("mesh of %d nodes with %d links requires %d ports per node, exceeds max %d",
				opts.Nodes, opts.Links, (opts.Nodes-1)*opts.Links, MaxPorts)
		}
		t = mesh(opts.Nodes, opts.Links)

	case opts.Ring:
		if opts.Nodes < 3 {
			return nil, fmt.Errorf("ring requires at least 3 nodes (got %d)", opts.Nodes)
		}
		if 2*opts.Links > MaxPorts {
			return nil, fmt.Errorf("ring with %d links requires %d ports per node, exceeds max %d",
				opts.Links, 2*opts.Links, MaxPorts)
		}
		t = ring(opts.Nodes, opts.Links)

	default:
		if opts.Spines < 1 {
			return nil, fmt.Errorf("spine-leaf requires at least 1 spine (got %d)", opts.Spines)
		}
		if opts.Leaves < 1 {
			return nil, fmt.Errorf("spine-leaf requires at least 1 leaf (got %d)", opts.Leaves)
		}
		if opts.Leaves*opts.Links > MaxPorts {
			return nil, fmt.Errorf("each spine would need %d ports (%d leaves x %d links), exceeds max %d",
				opts.Leaves*opts.Links, opts.Leaves, opts.Links, MaxPorts)
		}
		if opts.Spines*opts.Links > MaxPorts {
			return nil, fmt.Errorf("each leaf would need %d ports (%d spines x %d links), exceeds max %d",
				opts.Spines*opts.Links, opts.Spines, opts.Links, MaxPorts)
		}
		t = spineLeaf(opts.Spines, opts.Leaves, opts.Links)
	}

	// Add hosts if requested
	if opts.HostsPerSwitch > 0 {
		if err := addHosts(t, opts.HostsPerSwitch, opts.PortsPerHost); err != nil {
			return nil, err
		}
	}

	// Set SRv6 flag and overlay prefixes
	t.SRv6 = opts.SRv6
	if opts.SRv6 {
		for i := range t.Switches {
			t.Switches[i].OverlayPrefix = OverlayPrefixForASN(t.Switches[i].ASN)
		}
	}

	return t, nil
}

// addHosts appends host containers and host-to-switch links to the topology.
// Each switch gets hostsPerSwitch hosts. Each host has portsPerHost ports,
// with port p connecting to switch (homeSwitch + p) % totalSwitches.
func addHosts(t *Topology, hostsPerSwitch, portsPerHost int) error {
	if portsPerHost < 1 {
		portsPerHost = 1
	}

	totalSwitches := len(t.Switches)
	totalHosts := hostsPerSwitch * totalSwitches

	// Create host entries
	for i := 0; i < totalHosts; i++ {
		h := Host{Name: fmt.Sprintf("host%d", i+1)}
		for p := 0; p < portsPerHost; p++ {
			h.Ports = append(h.Ports, portName(p))
		}
		t.Hosts = append(t.Hosts, h)
	}

	// Track next available port index per switch (starts after switch-to-switch ports)
	nextPort := make([]int, totalSwitches)
	for i, sw := range t.Switches {
		nextPort[i] = len(sw.Ports)
	}

	// Create host-to-switch links
	for i := 0; i < totalHosts; i++ {
		homeSwitch := i / hostsPerSwitch
		for p := 0; p < portsPerHost; p++ {
			targetSwitch := (homeSwitch + p) % totalSwitches

			idx := nextPort[targetSwitch]
			if idx >= MaxPorts {
				return fmt.Errorf("%s would need %d ports (host connections), exceeds max %d",
					t.Switches[targetSwitch].Name, idx+1, MaxPorts)
			}
			nextPort[targetSwitch]++

			t.Switches[targetSwitch].Ports = append(t.Switches[targetSwitch].Ports, portName(idx))

			t.Links = append(t.Links, Link{
				A: Endpoint{
					Device: fmt.Sprintf("%s-linux", t.Switches[targetSwitch].Name),
					Port:   portName(idx),
				},
				B: Endpoint{
					Device: fmt.Sprintf("host%d", i+1),
					Port:   portName(p),
				},
			})
		}
	}

	return nil
}

// spineLeaf generates a classic spine-leaf topology where every leaf
// has `links` parallel uplinks to every spine (for ECMP).
func spineLeaf(spines, leaves, links int) *Topology {
	t := &Topology{Name: fmt.Sprintf("spine-leaf-%dx%d", spines, leaves)}

	// Create spines: each has leaves*links ports
	for s := 0; s < spines; s++ {
		asn := 65000 + s
		sw := Switch{
			Name:     fmt.Sprintf("spine%d", s+1),
			Role:     "spine",
			ASN:      asn,
			VtepAddr: VtepAddrForASN(asn),
		}
		// Ports grouped by leaf: leaf l uses port indices [l*links, (l+1)*links)
		for l := 0; l < leaves; l++ {
			for k := 0; k < links; k++ {
				sw.Ports = append(sw.Ports, portName(l*links+k))
			}
		}
		t.Switches = append(t.Switches, sw)
	}

	// Create leaves: each has spines*links ports
	for l := 0; l < leaves; l++ {
		asn := 65100 + l
		sw := Switch{
			Name:     fmt.Sprintf("leaf%d", l+1),
			Role:     "leaf",
			ASN:      asn,
			VtepAddr: VtepAddrForASN(asn),
		}
		// Ports grouped by spine: spine s uses port indices [s*links, (s+1)*links)
		for s := 0; s < spines; s++ {
			for k := 0; k < links; k++ {
				sw.Ports = append(sw.Ports, portName(s*links+k))
			}
		}
		t.Switches = append(t.Switches, sw)
	}

	// Create links: leaf[l] ↔ spine[s], `links` parallel links per pair
	for s := 0; s < spines; s++ {
		for l := 0; l < leaves; l++ {
			for k := 0; k < links; k++ {
				t.Links = append(t.Links, Link{
					A: Endpoint{
						Device: fmt.Sprintf("spine%d-linux", s+1),
						Port:   portName(l*links + k),
					},
					B: Endpoint{
						Device: fmt.Sprintf("leaf%d-linux", l+1),
						Port:   portName(s*links + k),
					},
				})
			}
		}
	}

	return t
}

// mesh generates a full mesh topology where every node is connected
// to every other node with `links` parallel links per pair.
func mesh(n, links int) *Topology {
	t := &Topology{Name: fmt.Sprintf("mesh-%d", n)}

	// Create nodes: each has (n-1)*links ports
	for i := 0; i < n; i++ {
		asn := 65000 + i
		sw := Switch{
			Name:     fmt.Sprintf("node%d", i+1),
			Role:     "node",
			ASN:      asn,
			VtepAddr: VtepAddrForASN(asn),
		}
		// Ports grouped by peer position (self skipped)
		for p := 0; p < n-1; p++ {
			for k := 0; k < links; k++ {
				sw.Ports = append(sw.Ports, portName(p*links+k))
			}
		}
		t.Switches = append(t.Switches, sw)
	}

	// Create links: for each pair (i, j) where i < j
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			// Node i: peer j is at position (j-1) in the peer list [skip self at i]
			// Node j: peer i is at position i in the peer list [i < j, so i keeps its index]
			for k := 0; k < links; k++ {
				t.Links = append(t.Links, Link{
					A: Endpoint{
						Device: fmt.Sprintf("node%d-linux", i+1),
						Port:   portName((j-1)*links + k),
					},
					B: Endpoint{
						Device: fmt.Sprintf("node%d-linux", j+1),
						Port:   portName(i*links + k),
					},
				})
			}
		}
	}

	return t
}

// ring generates a ring topology where each node connects to its
// two neighbors with `links` parallel links per neighbor (for ECMP).
func ring(n, links int) *Topology {
	t := &Topology{Name: fmt.Sprintf("ring-%d", n)}

	// Create nodes: each has 2*links ports
	for i := 0; i < n; i++ {
		asn := 65000 + i
		sw := Switch{
			Name:     fmt.Sprintf("node%d", i+1),
			Role:     "node",
			ASN:      asn,
			VtepAddr: VtepAddrForASN(asn),
		}
		// Ports 0..links-1: "prev" neighbor; ports links..2*links-1: "next" neighbor
		for p := 0; p < 2*links; p++ {
			sw.Ports = append(sw.Ports, portName(p))
		}
		t.Switches = append(t.Switches, sw)
	}

	// Create links: for each pair of neighbors in the ring, `links` parallel links
	for i := 0; i < n; i++ {
		next := (i + 1) % n
		for k := 0; k < links; k++ {
			t.Links = append(t.Links, Link{
				A: Endpoint{
					Device: fmt.Sprintf("node%d-linux", i+1),
					Port:   portName(links + k), // "next" side
				},
				B: Endpoint{
					Device: fmt.Sprintf("node%d-linux", next+1),
					Port:   portName(k), // "prev" side
				},
			})
		}
	}

	return t
}
