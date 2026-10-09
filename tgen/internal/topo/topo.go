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
}

// Switch represents a single SONiC virtual switch.
type Switch struct {
	Name  string
	Role  string // "spine", "leaf", or "node"
	ASN   int
	Ports []string
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
	HostsPerSwitch  int  // -h: number of hosts attached to each switch
	PortsPerHost    int  // -p: number of ports per host (default 1)
}

// MaxPorts is the maximum number of ports available per switch
// (KVM platform maps 30 lanes: Ethernet0 through Ethernet116, step 4).
const MaxPorts = 30

// portName returns the Ethernet interface name for a 0-based port index.
// Ports use step-4 naming to match the KVM lane map.
func portName(index int) string {
	return fmt.Sprintf("Ethernet%d", index*4)
}

// Generate builds a Topology from the given Options.
func Generate(opts Options) (*Topology, error) {
	var t *Topology

	switch {
	case opts.Mesh:
		if opts.Nodes < 2 {
			return nil, fmt.Errorf("mesh requires at least 2 nodes (got %d)", opts.Nodes)
		}
		if opts.Nodes-1 > MaxPorts {
			return nil, fmt.Errorf("mesh of %d nodes requires %d ports per node, exceeds max %d",
				opts.Nodes, opts.Nodes-1, MaxPorts)
		}
		t = mesh(opts.Nodes)

	case opts.Ring:
		if opts.Nodes < 3 {
			return nil, fmt.Errorf("ring requires at least 3 nodes (got %d)", opts.Nodes)
		}
		t = ring(opts.Nodes)

	default:
		if opts.Spines < 1 {
			return nil, fmt.Errorf("spine-leaf requires at least 1 spine (got %d)", opts.Spines)
		}
		if opts.Leaves < 1 {
			return nil, fmt.Errorf("spine-leaf requires at least 1 leaf (got %d)", opts.Leaves)
		}
		if opts.Spines > MaxPorts {
			return nil, fmt.Errorf("each leaf would need %d ports, exceeds max %d", opts.Spines, MaxPorts)
		}
		if opts.Leaves > MaxPorts {
			return nil, fmt.Errorf("each spine would need %d ports, exceeds max %d", opts.Leaves, MaxPorts)
		}
		t = spineLeaf(opts.Spines, opts.Leaves)
	}

	// Add hosts if requested
	if opts.HostsPerSwitch > 0 {
		if err := addHosts(t, opts.HostsPerSwitch, opts.PortsPerHost); err != nil {
			return nil, err
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
// has one uplink to every spine.
func spineLeaf(spines, leaves int) *Topology {
	t := &Topology{Name: fmt.Sprintf("spine-leaf-%dx%d", spines, leaves)}

	// Create spines
	for s := 0; s < spines; s++ {
		sw := Switch{
			Name: fmt.Sprintf("spine%d", s+1),
			Role: "spine",
			ASN:  65000 + s,
		}
		// Each spine has one port per leaf
		for l := 0; l < leaves; l++ {
			sw.Ports = append(sw.Ports, portName(l))
		}
		t.Switches = append(t.Switches, sw)
	}

	// Create leaves
	for l := 0; l < leaves; l++ {
		sw := Switch{
			Name: fmt.Sprintf("leaf%d", l+1),
			Role: "leaf",
			ASN:  65100 + l,
		}
		// Each leaf has one port per spine
		for s := 0; s < spines; s++ {
			sw.Ports = append(sw.Ports, portName(s))
		}
		t.Switches = append(t.Switches, sw)
	}

	// Create links: leaf[l] ↔ spine[s]
	for s := 0; s < spines; s++ {
		for l := 0; l < leaves; l++ {
			t.Links = append(t.Links, Link{
				A: Endpoint{
					Device: fmt.Sprintf("spine%d-linux", s+1),
					Port:   portName(l),
				},
				B: Endpoint{
					Device: fmt.Sprintf("leaf%d-linux", l+1),
					Port:   portName(s),
				},
			})
		}
	}

	return t
}

// mesh generates a full mesh topology where every node is connected
// to every other node.
func mesh(n int) *Topology {
	t := &Topology{Name: fmt.Sprintf("mesh-%d", n)}

	// Create nodes
	for i := 0; i < n; i++ {
		sw := Switch{
			Name: fmt.Sprintf("node%d", i+1),
			Role: "node",
			ASN:  65000 + i,
		}
		// Each node has n-1 ports (one per peer)
		for p := 0; p < n-1; p++ {
			sw.Ports = append(sw.Ports, portName(p))
		}
		t.Switches = append(t.Switches, sw)
	}

	// Create links: for each pair (i, j) where i < j
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			// Node i: peer j is at port index (j-1) [skip self]
			// Node j: peer i is at port index i
			t.Links = append(t.Links, Link{
				A: Endpoint{
					Device: fmt.Sprintf("node%d-linux", i+1),
					Port:   portName(j - 1),
				},
				B: Endpoint{
					Device: fmt.Sprintf("node%d-linux", j+1),
					Port:   portName(i),
				},
			})
		}
	}

	return t
}

// ring generates a ring topology where each node connects to its
// two neighbors (previous and next in the ring).
func ring(n int) *Topology {
	t := &Topology{Name: fmt.Sprintf("ring-%d", n)}

	// Create nodes
	for i := 0; i < n; i++ {
		sw := Switch{
			Name: fmt.Sprintf("node%d", i+1),
			Role: "node",
			ASN:  65000 + i,
			// Each node has exactly 2 ports
			Ports: []string{portName(0), portName(1)},
		}
		t.Switches = append(t.Switches, sw)
	}

	// Create links: node[i] next-port → node[(i+1)%n] prev-port
	for i := 0; i < n; i++ {
		next := (i + 1) % n
		t.Links = append(t.Links, Link{
			A: Endpoint{
				Device: fmt.Sprintf("node%d-linux", i+1),
				Port:   portName(1), // "next" port
			},
			B: Endpoint{
				Device: fmt.Sprintf("node%d-linux", next+1),
				Port:   portName(0), // "prev" port
			},
		})
	}

	return t
}
