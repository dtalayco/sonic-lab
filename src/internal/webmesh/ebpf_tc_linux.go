//go:build linux

/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/vishvananda/netlink"
)

// tcIngressParent is the TC handle for the ingress hook.
// netlink.HANDLE_INGRESS = 0xFFFFFFF1
const tcIngressParent = netlink.HANDLE_INGRESS

// AttachTC attaches the patch_panel program as a TC ingress filter on
// the interface with the given link index.
func (p *PatchPanel) AttachTC(linkIdx int) error {
	// Resolve the interface name for tc CLI use
	link, err := netlink.LinkByIndex(linkIdx)
	if err != nil {
		return fmt.Errorf("lookup link ifidx %d: %w", linkIdx, err)
	}
	ifname := link.Attrs().Name

	// Ensure the ingress qdisc exists (idempotent). We use the tc CLI
	// because the netlink library's QdiscAdd is rejected by some kernel
	// versions with "Ingress filters must set Parent to HANDLE_INGRESS".
	cmd := exec.Command("tc", "qdisc", "add", "dev", ifname, "ingress")
	if out, err := cmd.CombinedOutput(); err != nil {
		if !strings.Contains(string(out), "exists") {
			return fmt.Errorf("tc qdisc add dev %s ingress: %s: %w", ifname, string(out), err)
		}
	}

	// Check if a filter with the same name already exists (idempotent)
	existing, _ := netlink.FilterList(link, tcIngressParent)
	for _, f := range existing {
		if bf, ok := f.(*netlink.BpfFilter); ok && bf.Name == "patch_panel" {
			return nil // already attached
		}
	}

	filter := &netlink.BpfFilter{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: linkIdx,
			Parent:    tcIngressParent,
			Priority:  1,
			Protocol:  0x0003, // ETH_P_ALL
		},
		Fd:           p.prog.FD(),
		Name:         "patch_panel",
		DirectAction: true,
	}
	if err := netlink.FilterAdd(filter); err != nil {
		return fmt.Errorf("add BPF filter on %s: %w", ifname, err)
	}
	return nil
}

// DetachTC removes the patch_panel BPF filter from the interface.
func (p *PatchPanel) DetachTC(linkIdx int) error {
	link, err := netlink.LinkByIndex(linkIdx)
	if err != nil {
		return fmt.Errorf("lookup link ifidx %d: %w", linkIdx, err)
	}
	filter := &netlink.BpfFilter{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: linkIdx,
			Parent:    tcIngressParent,
			Priority:  1,
			Protocol:  0x0003,
		},
		Fd:           p.prog.FD(),
		Name:         "patch_panel",
		DirectAction: true,
	}
	if err := netlink.FilterDel(filter); err != nil {
		return fmt.Errorf("remove BPF filter on %s: %w", link.Attrs().Name, err)
	}
	return nil
}


