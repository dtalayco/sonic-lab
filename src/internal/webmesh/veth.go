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

// vethName generates the veth name used in the staging namespace before the
// endpoint is moved into a container namespace.
// Convention: "v" + first 3 chars of base device name + port number.
// e.g. spine1-linux + Ethernet0 → "vsp0"
func vethName(dev, port string) string {
	base := strings.TrimSuffix(dev, "-linux")
	prefix := base
	if len(prefix) > 3 {
		prefix = prefix[:3]
	}
	portNum := strings.TrimPrefix(port, "Ethernet")
	return "v" + prefix + portNum
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

// createLink wires a veth pair between two containers.
// It creates the pair in the current (staging) namespace, moves each end into
// the target container's namespace, renames it to the SONiC port name, and
// brings it up. The operation is idempotent: if either endpoint already exists
// in its target namespace, the function returns nil.
func createLink(devA, portA, devB, portB string) error {
	pidA, err := containerPID(devA)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", devA, err)
	}
	pidB, err := containerPID(devB)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", devB, err)
	}

	// Idempotency check
	if linkInNS(pidA, portA) || linkInNS(pidB, portB) {
		log.Printf("link %s:%s <-> %s:%s already exists, skipping", devA, portA, devB, portB)
		return nil
	}

	ha := vethName(devA, portA)
	hb := vethName(devB, portB)

	// Create veth pair in staging namespace
	if err := run("ip", "link", "add", ha, "type", "veth", "peer", "name", hb); err != nil {
		return fmt.Errorf("create veth %s<->%s: %w", ha, hb, err)
	}

	// Move endpoint A into container A
	if err := run("ip", "link", "set", ha, "netns", strconv.Itoa(pidA)); err != nil {
		cleanupVeth(ha, hb)
		return fmt.Errorf("move %s → %s: %w", ha, devA, err)
	}

	// Move endpoint B into container B
	if err := run("ip", "link", "set", hb, "netns", strconv.Itoa(pidB)); err != nil {
		cleanupVeth(ha, hb)
		return fmt.Errorf("move %s → %s: %w", hb, devB, err)
	}

	// Rename in target namespaces
	if err := run("nsenter", "-t", strconv.Itoa(pidA), "-n", "ip", "link", "set", ha, "name", portA); err != nil {
		cleanupVeth(ha, hb)
		return fmt.Errorf("rename %s→%s in %s: %w", ha, portA, devA, err)
	}
	if err := run("nsenter", "-t", strconv.Itoa(pidB), "-n", "ip", "link", "set", hb, "name", portB); err != nil {
		cleanupVeth(ha, hb)
		return fmt.Errorf("rename %s→%s in %s: %w", hb, portB, devB, err)
	}

	// Bring up (best-effort; failures here are non-fatal)
	runQuiet("nsenter", "-t", strconv.Itoa(pidA), "-n", "ip", "link", "set", portA, "up")
	runQuiet("nsenter", "-t", strconv.Itoa(pidB), "-n", "ip", "link", "set", portB, "up")

	log.Printf("created link %s:%s <-> %s:%s", devA, portA, devB, portB)
	return nil
}

// deleteLink removes a veth pair between two containers.
// It is safe to call even if the links or containers no longer exist.
func deleteLink(devA, portA, devB, portB string) error {
	if pid, err := containerPID(devA); err == nil && linkInNS(pid, portA) {
		runQuiet("nsenter", "-t", strconv.Itoa(pid), "-n", "ip", "link", "set", portA, "down")
		runQuiet("nsenter", "-t", strconv.Itoa(pid), "-n", "ip", "link", "del", portA)
	}
	if pid, err := containerPID(devB); err == nil && linkInNS(pid, portB) {
		runQuiet("nsenter", "-t", strconv.Itoa(pid), "-n", "ip", "link", "set", portB, "down")
		runQuiet("nsenter", "-t", strconv.Itoa(pid), "-n", "ip", "link", "del", portB)
	}
	log.Printf("deleted link %s:%s <-> %s:%s", devA, portA, devB, portB)
	return nil
}

// cleanupVeth removes veth endpoints that may be left in the staging namespace
// after a failed createLink. Deleting either end of a veth pair removes both.
func cleanupVeth(names ...string) {
	for _, n := range names {
		runQuiet("ip", "link", "del", n)
	}
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