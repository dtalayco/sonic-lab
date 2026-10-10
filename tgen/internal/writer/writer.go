/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

// Package writer generates all output files for a topology:
// docker-compose.yml, topology.yml, and supporting shell scripts.
package writer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"sonic-lab/tgen/internal/topo"
)

// Writer generates output files into a target directory.
type Writer struct {
	OutputDir string
	Topology  *topo.Topology
}

// New creates a Writer for the given topology and output directory.
func New(outputDir string, t *topo.Topology) *Writer {
	return &Writer{
		OutputDir: outputDir,
		Topology:  t,
	}
}

// Run writes all output files.
func (w *Writer) Run() error {
	if err := os.MkdirAll(filepath.Join(w.OutputDir, "tmp", "vnet_ready"), 0o755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}

	if err := w.writeCompose(); err != nil {
		return fmt.Errorf("writing docker-compose.yml: %w", err)
	}
	if err := w.writeTopologyYAML(); err != nil {
		return fmt.Errorf("writing topology.yml: %w", err)
	}
	if err := w.writeInitScript(); err != nil {
		return fmt.Errorf("writing init_linux.sh: %w", err)
	}
	if err := w.writeEntryScript(); err != nil {
		return fmt.Errorf("writing sonic_entry.sh: %w", err)
	}
	if err := w.writeFRRConfigs(); err != nil {
		return fmt.Errorf("writing FRR configs: %w", err)
	}
	if w.Topology.SRv6 {
		if err := w.writeSRv6Routes(); err != nil {
			return fmt.Errorf("writing SRv6 routes: %w", err)
		}
	}
	if err := w.writeStartScript(); err != nil {
		return fmt.Errorf("writing start.sh: %w", err)
	}

	return nil
}

// --- docker-compose.yml ---

func (w *Writer) writeCompose() error {
	var sb strings.Builder

	sb.WriteString("version: \"3.9\"\n\n")

	// Anchors
	sb.WriteString("x-sonic-common: &sonic-common\n")
	sb.WriteString("  image: docker-sonic-vs:latest\n")
	sb.WriteString("  platform: linux/amd64\n")
	sb.WriteString("  privileged: true\n")
	sb.WriteString("  tty: true\n")
	sb.WriteString("  shm_size: \"2gb\"\n")
	sb.WriteString("  security_opt:\n")
	sb.WriteString("    - seccomp=unconfined\n\n")

	sb.WriteString("x-debian-common: &debian-common\n")
	sb.WriteString("  build:\n")
	sb.WriteString("    context: ..\n")
	sb.WriteString("    dockerfile: Dockerfile.debian\n")
	sb.WriteString("  image: sonic-lab/debian:latest\n")
	sb.WriteString("  platform: linux/amd64\n")
	sb.WriteString("  privileged: true\n")
	sb.WriteString("  tty: true\n")
	sb.WriteString("  volumes:\n")
	sb.WriteString("    - ./init_linux.sh:/init_linux.sh:ro\n")
	sb.WriteString("    - /var/run/docker.sock:/var/run/docker.sock\n")
	sb.WriteString("    - ./tmp/vnet_ready:/tmp/vnet_ready:rw\n")
	sb.WriteString("  shm_size: \"2gb\"\n\n")

	sb.WriteString("x-host-common: &host-common\n")
	sb.WriteString("  build:\n")
	sb.WriteString("    context: ..\n")
	sb.WriteString("    dockerfile: Dockerfile.host\n")
	sb.WriteString("  image: sonic-lab/host:latest\n")
	sb.WriteString("  platform: linux/amd64\n")
	sb.WriteString("  privileged: true\n")
	sb.WriteString("  tty: true\n\n")

	sb.WriteString("services:\n")

	// Webmesh
	sb.WriteString("  #################################\n")
	sb.WriteString("  # Webmesh patch panel container #\n")
	sb.WriteString("  #################################\n\n")
	sb.WriteString("  webmesh:\n")
	sb.WriteString("    build:\n")
	sb.WriteString("      context: ..\n")
	sb.WriteString("      dockerfile: Dockerfile.webmesh\n")
	sb.WriteString("    image: sonic-lab/webmesh:latest\n")
	sb.WriteString("    platform: linux/amd64\n")
	sb.WriteString("    privileged: true\n")
	sb.WriteString("    pid: \"host\"\n")
	sb.WriteString("    container_name: webmesh\n")
	sb.WriteString("    ports:\n")
	sb.WriteString("      - \"8092:9090\"\n")
	sb.WriteString("    volumes:\n")
	sb.WriteString("      - ./topology.yml:/etc/webmesh/topology.yml:ro\n")
	sb.WriteString("      - /var/run/docker.sock:/var/run/docker.sock:ro\n")
	sb.WriteString("      - ./tmp/vnet_ready:/tmp/vnet_ready:rw\n")
	sb.WriteString("    healthcheck:\n")
	sb.WriteString("      test: [\"CMD-SHELL\", \"curl -sf http://localhost:9090/v1/status | grep -q '\\\"healthy\\\":true'\"]\n")
	sb.WriteString("      interval: 2s\n")
	sb.WriteString("      timeout: 3s\n")
	sb.WriteString("      retries: 60\n\n")

	// Backing Linux containers
	sb.WriteString("  ###########################\n")
	sb.WriteString("  # Backing Linux containers #\n")
	sb.WriteString("  ###########################\n\n")

	for _, sw := range w.Topology.Switches {
		name := sw.Name
		sb.WriteString(fmt.Sprintf("  %s-linux:\n", name))
		sb.WriteString("    <<: *debian-common\n")
		sb.WriteString(fmt.Sprintf("    container_name: %s-linux\n", name))
		sb.WriteString(fmt.Sprintf("    command: [\"bash\", \"/init_linux.sh\", \"%s-linux\"]\n\n", name))
	}

	// SONiC containers
	sb.WriteString("  ###########################\n")
	sb.WriteString("  # SONiC VS containers      #\n")
	sb.WriteString("  ###########################\n\n")

	for i, sw := range w.Topology.Switches {
		name := sw.Name
		sb.WriteString(fmt.Sprintf("  %s:\n", name))
		sb.WriteString("    <<: *sonic-common\n")
		sb.WriteString(fmt.Sprintf("    container_name: sonic-%s\n", name))
		sb.WriteString("    depends_on:\n")
		sb.WriteString("      webmesh:\n")
		sb.WriteString("        condition: service_healthy\n")
		sb.WriteString(fmt.Sprintf("      %s-linux:\n", name))
		sb.WriteString("        condition: service_started\n")
		sb.WriteString(fmt.Sprintf("    network_mode: \"service:%s-linux\"\n", name))
		sb.WriteString("    volumes:\n")
		sb.WriteString("      - ./tmp/vnet_ready:/tmp/vnet_ready:ro\n")
		sb.WriteString(fmt.Sprintf("      - /var/run/redis-vs/%s:/var/run/redis\n", name))
		sb.WriteString("      - ./sonic_entry.sh:/sonic_entry.sh:ro\n")
		sb.WriteString(fmt.Sprintf("      - ./frr/%s.conf:/tmp/frr.conf:ro\n", name))
		if w.Topology.SRv6 {
			sb.WriteString(fmt.Sprintf("      - ./srv6/%s.routes:/srv6.routes:ro\n", name))
		}
		sb.WriteString(fmt.Sprintf("    entrypoint: [\"/bin/bash\", \"/sonic_entry.sh\", \"%s-linux\", \"%s\"]\n", name, sw.VtepAddr))
		if i < len(w.Topology.Switches)-1 {
			sb.WriteString("\n")
		}
	}

	// Host containers
	if len(w.Topology.Hosts) > 0 {
		sb.WriteString("\n  ########################\n")
		sb.WriteString("  # Simulated host containers #\n")
		sb.WriteString("  ########################\n\n")

		for i, h := range w.Topology.Hosts {
			sb.WriteString(fmt.Sprintf("  %s:\n", h.Name))
			sb.WriteString("    <<: *host-common\n")
			sb.WriteString(fmt.Sprintf("    container_name: %s\n", h.Name))
			if i < len(w.Topology.Hosts)-1 {
				sb.WriteString("\n")
			}
		}
	}

	return os.WriteFile(filepath.Join(w.OutputDir, "docker-compose.yml"), []byte(sb.String()), 0o644)
}

// --- topology.yml (consumed by webmesh) ---

// topologyYAML is the YAML structure that webmesh expects.
type topologyYAML struct {
	Topology struct {
		Name     string                `yaml:"name"`
		Switches map[string]switchYAML `yaml:"switches"`
		Hosts    map[string]hostYAML   `yaml:"hosts,omitempty"`
		Links    []linkYAML            `yaml:"links"`
	} `yaml:"topology"`
}

type switchYAML struct {
	Role  string                 `yaml:"role"`
	ASN   int                    `yaml:"asn"`
	Ports map[string]interface{} `yaml:"ports"`
}

type hostYAML struct {
	Ports map[string]interface{} `yaml:"ports"`
}

type linkYAML struct {
	Endpoints []endpointYAML `yaml:"endpoints"`
}

type endpointYAML struct {
	Device string `yaml:"device"`
	Port   string `yaml:"port"`
}

func (w *Writer) writeTopologyYAML() error {
	var ty topologyYAML
	ty.Topology.Name = w.Topology.Name
	ty.Topology.Switches = make(map[string]switchYAML, len(w.Topology.Switches))

	for _, sw := range w.Topology.Switches {
		sy := switchYAML{
			Role:  sw.Role,
			ASN:   sw.ASN,
			Ports: make(map[string]interface{}, len(sw.Ports)),
		}
		for _, p := range sw.Ports {
			sy.Ports[p] = map[string]interface{}{}
		}
		ty.Topology.Switches[sw.Name] = sy
	}

	if len(w.Topology.Hosts) > 0 {
		ty.Topology.Hosts = make(map[string]hostYAML, len(w.Topology.Hosts))
		for _, h := range w.Topology.Hosts {
			hyp := make(map[string]interface{}, len(h.Ports))
			for _, p := range h.Ports {
				hyp[p] = map[string]interface{}{}
			}
			ty.Topology.Hosts[h.Name] = hostYAML{Ports: hyp}
		}
	}

	for _, l := range w.Topology.Links {
		ty.Topology.Links = append(ty.Topology.Links, linkYAML{
			Endpoints: []endpointYAML{
				{Device: l.A.Device, Port: l.A.Port},
				{Device: l.B.Device, Port: l.B.Port},
			},
		})
	}

	data, err := yaml.Marshal(&ty)
	if err != nil {
		return fmt.Errorf("marshaling topology: %w", err)
	}

	return os.WriteFile(filepath.Join(w.OutputDir, "topology.yml"), data, 0o644)
}

// --- Shell scripts ---

func (w *Writer) writeInitScript() error {
	data := []byte(`#!/usr/bin/env bash
set -e
echo "Initializing with arguments $@"
touch "/tmp/vnet_ready/$1"
echo "vnet ready"
sleep infinity
`)
	return os.WriteFile(filepath.Join(w.OutputDir, "init_linux.sh"), data, 0o755)
}

func (w *Writer) writeEntryScript() error {
	data := []byte(`#!/usr/bin/env bash
set -e

SWITCH="$1"
VTEP="${2:-}"

echo "Waiting for vnet file /tmp/vnet_ready/$SWITCH"

while [ ! -f "/tmp/vnet_ready/$SWITCH" ]; do
    sleep 3
done

echo "vnet ready, starting SONiC..."

# Start supervisord in the background so we can configure
# the VTEP interface and FRR after SONiC's daemons are up.
/usr/local/bin/supervisord -n &
SUPERVISOR_PID=$!

if [ -n "$VTEP" ]; then
    # Wait for zebra (started by SONiC's start.sh).
    echo "Waiting for zebra to start..."
    for i in $(seq 1 90); do
        if supervisorctl status zebra 2>/dev/null | grep -q "RUNNING"; then
            echo "zebra is running"
            break
        fi
        sleep 2
    done

    # Create Loopback0 (dummy type) with the VTEP address.
    # NOTE: SONiC's ip binary does not support "type loopback";
    # "type dummy" is the correct type for a VTEP interface.
    ip link add Loopback0 type dummy 2>/dev/null || true
    ip addr replace "$VTEP" dev Loopback0
    ip link set Loopback0 up
    echo "Loopback0 created with VTEP address $VTEP"

    # Set admin up in SONiC CONFIG_DB for all Ethernet interfaces.
    # This tells orchagent to program the port; required for the
    # interface to show as "up" in "show interfaces status".
    for iface in $(ip -o link show 2>/dev/null | awk -F': ' '{print $2}' | grep '^Ethernet'); do
        config interface startup "$iface" 2>/dev/null || true
    done
    echo "Set admin up for Ethernet interfaces"

    # Wait for orchagent to process the admin-up requests.
    sleep 5

    # Bring up all Ethernet* interfaces in the kernel BEFORE starting
    # bgpd. Zebra must see them as UP before bgpd connects, otherwise
    # bgpd crashes when reading the config with unnumbered neighbors.
    for iface in $(ip -o link show 2>/dev/null | awk -F': ' '{print $2}' | grep '^Ethernet'); do
        ip link set "$iface" up 2>/dev/null || true
    done
    echo "Brought up Ethernet interfaces"

    # Give zebra time to process the interface state changes via netlink
    # before bgpd connects and requests the interface list.
    sleep 3

    # SONiC's start.sh does not start bgpd — start it explicitly now
    # that all interfaces are up and registered with zebra.
    supervisorctl start bgpd 2>/dev/null || true
    echo "Waiting for bgpd to start..."
    for i in $(seq 1 30); do
        if supervisorctl status bgpd 2>/dev/null | grep -q "RUNNING"; then
            echo "bgpd is running"
            break
        fi
        sleep 2
    done

    # Load FRR config explicitly. Non-fatal: if it fails the container
    # should stay up so we can debug.
    echo "Loading FRR configuration..."
    vtysh -f /tmp/frr.conf || echo "WARNING: vtysh -f returned non-zero"
    echo "FRR configuration loaded"

    # If SRv6 routes file is present, execute it to program kernel seg6 routes.
    if [ -f /srv6.routes ]; then
        echo "Loading SRv6 overlay routes..."
        bash /srv6.routes || echo "WARNING: SRv6 routes script returned non-zero"
        echo "SRv6 overlay routes loaded"
    fi
fi

# Keep the container alive by waiting on supervisord.
wait $SUPERVISOR_PID
`)
	return os.WriteFile(filepath.Join(w.OutputDir, "sonic_entry.sh"), data, 0o755)
}

// --- FRR configuration ---

// kernelPortName returns the kernel interface name for a SONiC port.
// In SONiC VS, the kernel interface name is the same as the port name
// (Ethernet0, Ethernet4, Ethernet8, etc.).
func kernelPortName(portName string) string {
	return portName
}

// neighborInfo describes a BGP neighbor relationship for one local port.
type neighborInfo struct {
	localPort string
	peerASN   int
}

// switchNeighbors returns the list of switch-to-switch BGP neighbors
// for the given switch, excluding host connections.
func (w *Writer) switchNeighbors(sw topo.Switch) []neighborInfo {
	var result []neighborInfo
	swLinuxName := sw.Name + "-linux"

	for _, link := range w.Topology.Links {
		var local, peer *topo.Endpoint
		if link.A.Device == swLinuxName {
			local, peer = &link.A, &link.B
		} else if link.B.Device == swLinuxName {
			local, peer = &link.B, &link.A
		} else {
			continue
		}

		// Only include links to other switches, not hosts.
		peerName := strings.TrimSuffix(peer.Device, "-linux")
		peerSwitch, ok := w.findSwitch(peerName)
		if !ok {
			continue
		}

		result = append(result, neighborInfo{
			localPort: local.Port,
			peerASN:   peerSwitch.ASN,
		})
	}

	return result
}

// findSwitch looks up a switch by name in the topology.
func (w *Writer) findSwitch(name string) (*topo.Switch, bool) {
	for i := range w.Topology.Switches {
		if w.Topology.Switches[i].Name == name {
			return &w.Topology.Switches[i], true
		}
	}
	return nil, false
}

// generateFRRConfig produces a complete FRR configuration for one switch,
// including BGP unnumbered neighbors and the VTEP loopback prefix.
func (w *Writer) generateFRRConfig(sw topo.Switch) string {
	neighbors := w.switchNeighbors(sw)

	var sb strings.Builder

	sb.WriteString("! FRR configuration generated by tgen\n")
	sb.WriteString(fmt.Sprintf("! Switch: %s  ASN: %d  VTEP: %s\n", sw.Name, sw.ASN, sw.VtepAddr))
	sb.WriteString("!\n")
	sb.WriteString("frr defaults traditional\n")
	sb.WriteString("!\n")

	// Loopback0 with VTEP address
	sb.WriteString("interface Loopback0\n")
	sb.WriteString(fmt.Sprintf(" ipv6 address %s\n", sw.VtepAddr))
	sb.WriteString("!\n")

	// SRv6 locator + static SID (End behavior)
	// Must appear BEFORE router bgp — FRR 10.4.1 parser cannot transition
	// from one top-level node (router bgp) to another (segment-routing)
	// within the same config file.
	if w.Topology.SRv6 {
		locatorName := sw.Name
		locatorPrefix := topo.LocatorPrefixForASN(sw.ASN)
		vtepBare := topo.VtepBareAddr(sw.VtepAddr)

		sb.WriteString("segment-routing\n")
		sb.WriteString(" srv6\n")
		sb.WriteString("  locators\n")
		sb.WriteString(fmt.Sprintf("   locator %s\n", locatorName))
		sb.WriteString(fmt.Sprintf("    prefix %s\n", locatorPrefix))
		sb.WriteString("   exit\n")
		sb.WriteString("   !\n")
		sb.WriteString("  exit\n")
		sb.WriteString("  !\n")
		sb.WriteString("  static-sids\n")
		sb.WriteString(fmt.Sprintf("   sid %s/128 locator %s behavior uN\n", vtepBare, locatorName))
		sb.WriteString("  exit\n")
		sb.WriteString("  !\n")
		sb.WriteString(" exit\n")
		sb.WriteString(" !\n")
		sb.WriteString("!\n")
	}

	// BGP router
	sb.WriteString(fmt.Sprintf("router bgp %d\n", sw.ASN))
	sb.WriteString(" !\n")
	sb.WriteString(" no bgp ebgp-requires-policy\n")
	sb.WriteString(" no bgp default ipv4-unicast\n")
	sb.WriteString(" bgp disable-ebgp-connected-route-check\n")
	sb.WriteString(" bgp bestpath as-path multipath-relax\n")

	// Peer-group for unnumbered eBGP transit
	sb.WriteString(" neighbor TRANSIT peer-group\n")
	sb.WriteString(" neighbor TRANSIT remote-as external\n")
	sb.WriteString(" neighbor TRANSIT ebgp-multihop\n")

	// Unnumbered neighbors via interface keyword + peer-group
	for _, nb := range neighbors {
		kernel := kernelPortName(nb.localPort)
		sb.WriteString(fmt.Sprintf(" neighbor %s interface peer-group TRANSIT\n", kernel))
	}
	sb.WriteString(" !\n")

	// IPv6 address family
	sb.WriteString(" address-family ipv6\n")
	sb.WriteString(fmt.Sprintf("  network %s\n", sw.VtepAddr))
	sb.WriteString("  neighbor TRANSIT activate\n")
	sb.WriteString("  neighbor TRANSIT next-hop-self\n")
	sb.WriteString(" exit-address-family\n")

	// SRv6 in BGP (SID distribution via BGP)
	if w.Topology.SRv6 {
		sb.WriteString(" segment-routing srv6\n")
	}

	sb.WriteString("!\n")

	sb.WriteString("line vty\n")
	sb.WriteString("!\n")

	return sb.String()
}

// writeFRRConfigs writes one FRR config file per switch into the frr/ subdirectory.
func (w *Writer) writeFRRConfigs() error {
	frrDir := filepath.Join(w.OutputDir, "frr")
	if err := os.MkdirAll(frrDir, 0o755); err != nil {
		return fmt.Errorf("creating frr dir: %w", err)
	}

	for _, sw := range w.Topology.Switches {
		config := w.generateFRRConfig(sw)
		path := filepath.Join(frrDir, sw.Name+".conf")
		if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
			return fmt.Errorf("writing frr config for %s: %w", sw.Name, err)
		}
	}

	return nil
}

// writeSRv6Routes writes per-switch seg6 route files into the srv6/ subdirectory.
// Each file adds the switch's overlay prefix to Loopback0 and programs seg6
// kernel routes to every remote VTEP (single-segment SRH = End behavior).
func (w *Writer) writeSRv6Routes() error {
	srv6Dir := filepath.Join(w.OutputDir, "srv6")
	if err := os.MkdirAll(srv6Dir, 0o755); err != nil {
		return fmt.Errorf("creating srv6 dir: %w", err)
	}

	for _, sw := range w.Topology.Switches {
		var sb strings.Builder
		sb.WriteString("#!/bin/bash\n")
		sb.WriteString(fmt.Sprintf("# SRv6 overlay routes for %s (ASN %d)\n", sw.Name, sw.ASN))
		sb.WriteString("# Generated by tgen — do not edit manually.\n")
		sb.WriteString("#\n")
		sb.WriteString(fmt.Sprintf("# VTEP (End SID): %s\n", topo.VtepBareAddr(sw.VtepAddr)))
		sb.WriteString(fmt.Sprintf("# Overlay prefix:   %s\n\n", sw.OverlayPrefix))

		// Add the overlay /128 to Loopback0 so the kernel can deliver
		// decapsulated packets to this switch.
		overlayHost := sw.OverlayPrefix[:len(sw.OverlayPrefix)-3] + "1/128"
		sb.WriteString(fmt.Sprintf("ip addr replace %s dev Loopback0 2>/dev/null || true\n", overlayHost))
		sb.WriteString("\n")

		// Seg6 routes to every remote switch's overlay prefix.
		// Each route: dest = remote overlay /64, SRH = [remote VTEP]
		// dev Loopback0 marks the route as local; the kernel still performs
		// a FIB lookup for the outer dst (remote VTEP) and uses BGP ECMP
		// nexthops to forward the encapsulated packet.
		for _, other := range w.Topology.Switches {
			if other.Name == sw.Name {
				continue
			}
			remoteVTEP := topo.VtepBareAddr(other.VtepAddr)
			sb.WriteString(fmt.Sprintf(
				"ip -6 route add %s encap seg6 mode encap segs %s dev Loopback0 2>/dev/null || true\n",
				other.OverlayPrefix, remoteVTEP,
			))
		}

		path := filepath.Join(srv6Dir, sw.Name+".routes")
		if err := os.WriteFile(path, []byte(sb.String()), 0o755); err != nil {
			return fmt.Errorf("writing srv6 routes for %s: %w", sw.Name, err)
		}
	}

	return nil
}

func (w *Writer) writeStartScript() error {
	data := []byte(`#!/usr/bin/env bash
set -e

echo "Building images..."
docker compose build

echo "Starting topology..."
docker compose up -d

echo
echo "Running containers:"
docker ps --filter "name=sonic-"
echo
echo "Webmesh UI: http://localhost:8092"
`)
	return os.WriteFile(filepath.Join(w.OutputDir, "start.sh"), data, 0o755)
}
