/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import (
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
)

// wmIfname generates the webmesh-side veth endpoint name.
// Convention: "wm-" + base device name + "-" + port number.
// e.g. spine1-linux + Ethernet0 → "wm-spine1-0"
func wmIfname(dev, port string) string {
	base := strings.TrimSuffix(dev, "-linux")
	portNum := strings.TrimPrefix(port, "Ethernet")
	return "wm-" + base + "-" + portNum
}

// swIfname generates the temporary name for the switch-side veth endpoint
// before it is moved into the target namespace and renamed.
func swIfname(dev, port string) string {
	base := strings.TrimSuffix(dev, "-linux")
	portNum := strings.TrimPrefix(port, "Ethernet")
	return "sw-" + base + "-" + portNum
}

// containerPID returns the host PID of the init process of the named container.
func containerPID(name string) (int, error) {
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Pid}}", name).Output()
	if err != nil {
		return 0, fmt.Errorf("docker inspect %s: %w", name, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("parsing pid for %s: %w", name, err)
	}
	if pid == 0 {
		return 0, fmt.Errorf("container %s is not running", name)
	}
	return pid, nil
}

// linkInNS checks whether a network interface exists in the given PID's netns.
func linkInNS(pid int, ifname string) bool {
	return exec.Command("nsenter", "-t", strconv.Itoa(pid), "-n", "ip", "link", "show", ifname).Run() == nil
}

// linkIsUp checks whether a network interface is administratively UP
// in the given PID's netns.
func linkIsUp(pid int, ifname string) bool {
	out, err := exec.Command("nsenter", "-t", strconv.Itoa(pid), "-n", "ip", "link", "show", ifname).Output()
	if err != nil {
		return false
	}
	s := string(out)
	idx := strings.Index(s, "<")
	if idx < 0 {
		return false
	}
	end := strings.Index(s[idx:], ">")
	if end < 0 {
		return false
	}
	flags := s[idx : idx+end]
	for _, f := range strings.Split(flags, ",") {
		if strings.TrimSpace(f) == "UP" {
			return true
		}
	}
	return false
}

// ifindexByName returns the ifindex of a link in the current namespace.
func ifindexByName(name string) (int, error) {
	out, err := exec.Command("ip", "-j", "link", "show", name).Output()
	if err != nil {
		return 0, fmt.Errorf("ip link show %s: %w", name, err)
	}
	// The JSON output is a single object: {"ifindex":3,"ifname":"wm-spine1-0",...}
	// Parse minimally: find "ifindex":N
	s := string(out)
	key := `"ifindex":`
	idx := strings.Index(s, key)
	if idx < 0 {
		return 0, fmt.Errorf("ifindex not found in output for %s", name)
	}
	s = s[idx+len(key):]
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0, fmt.Errorf("parsing ifindex for %s: %w", name, err)
	}
	return n, nil
}

// createLink wires two veth pairs for a link through the webmesh namespace.
//
//
//   switch A netns       webmesh netns        switch B netns
//   ┌──────────┐  veth  ┌────────────┐  eBPF ┌────────────┐  veth ┌──────────┐
//   │  ethN    │◄──────►│ wm-a-x     │◄─────►│ wm-b-y     │◄──────►│  ethM    │
//   └──────────┘        └────────────┘       └────────────┘        └──────────┘
//
// The webmesh-side endpoints (wm-a-x, wm-b-y) remain in the webmesh
// namespace. The switch-side endpoints are moved into the respective
// switch namespaces and renamed to the SONiC kernel interface names
// (ethN), which SAI VS maps to logical ports (EthernetN) via the
// lane-based lanemap.
//
// The operation is idempotent: if either switch-side endpoint already
// exists in its target namespace, the function returns nil.
func createLink(devA, portA, devB, portB string) error {
	pidA, err := containerPID(devA)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", devA, err)
	}
	pidB, err := containerPID(devB)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", devB, err)
	}

	ka := kernelIfname(portA)
	kb := kernelIfname(portB)

	// Idempotency: if either switch-side endpoint already exists, skip
	if linkInNS(pidA, ka) || linkInNS(pidB, kb) {
		log.Printf("link %s:%s <-> %s:%s already exists, skipping", devA, portA, devB, portB)
		return nil
	}

	wmA := wmIfname(devA, portA)
	swA := swIfname(devA, portA)
	wmB := wmIfname(devB, portB)
	swB := swIfname(devB, portB)

	// --- Veth pair A: webmesh ↔ switch A ---
	if err := run("ip", "link", "add", wmA, "type", "veth", "peer", "name", swA); err != nil {
		return fmt.Errorf("create veth %s<->%s: %w", wmA, swA, err)
	}

	if err := run("ip", "link", "set", swA, "netns", strconv.Itoa(pidA)); err != nil {
		cleanupWmPair(wmA)
		return fmt.Errorf("move %s → %s: %w", swA, devA, err)
	}

	if err := run("nsenter", "-t", strconv.Itoa(pidA), "-n", "ip", "link", "set", swA, "name", ka); err != nil {
		cleanupWmPair(wmA)
		return fmt.Errorf("rename %s→%s in %s: %w", swA, ka, devA, err)
	}

	// --- Veth pair B: webmesh ↔ switch B ---
	if err := run("ip", "link", "add", wmB, "type", "veth", "peer", "name", swB); err != nil {
		cleanupWmPair(wmA)
		return fmt.Errorf("create veth %s<->%s: %w", wmB, swB, err)
	}

	if err := run("ip", "link", "set", swB, "netns", strconv.Itoa(pidB)); err != nil {
		cleanupWmPair(wmA)
		cleanupWmPair(wmB)
		return fmt.Errorf("move %s → %s: %w", swB, devB, err)
	}

	if err := run("nsenter", "-t", strconv.Itoa(pidB), "-n", "ip", "link", "set", swB, "name", kb); err != nil {
		cleanupWmPair(wmA)
		cleanupWmPair(wmB)
		return fmt.Errorf("rename %s→%s in %s: %w", swB, kb, devB, err)
	}

	// Bring up all four endpoints (best-effort)
	runQuiet("ip", "link", "set", wmA, "up")
	runQuiet("ip", "link", "set", wmB, "up")
	runQuiet("nsenter", "-t", strconv.Itoa(pidA), "-n", "ip", "link", "set", ka, "up")
	runQuiet("nsenter", "-t", strconv.Itoa(pidB), "-n", "ip", "link", "set", kb, "up")

	log.Printf("created link %s:%s <-> %s:%s (kernel: %s, %s; wm: %s, %s)",
		devA, portA, devB, portB, ka, kb, wmA, wmB)
	return nil
}

// deleteLink removes both veth pairs for a link.
// Deleting the webmesh-side endpoint also deletes the switch-side peer,
// which causes the SONiC interface to go down.
func deleteLink(devA, portA, devB, portB string) error {
	wmA := wmIfname(devA, portA)
	wmB := wmIfname(devB, portB)

	// Delete webmesh-side endpoints (peers in switch ns are removed too)
	runQuiet("ip", "link", "set", wmA, "down")
	runQuiet("ip", "link", "del", wmA)
	runQuiet("ip", "link", "set", wmB, "down")
	runQuiet("ip", "link", "del", wmB)

	log.Printf("deleted link %s:%s <-> %s:%s (wm: %s, %s)", devA, portA, devB, portB, wmA, wmB)
	return nil
}

// cleanupWmPair removes a webmesh-side veth endpoint (and its peer) if it
// exists in the current namespace.
func cleanupWmPair(wmName string) {
	runQuiet("ip", "link", "del", wmName)
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %v: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runQuiet(name string, args ...string) {
	_ = run(name, args...)
}