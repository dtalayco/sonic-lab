//go:build linux

/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import (
	"fmt"
	"strings"

	"github.com/vishvananda/netlink"
)

// tcIngressParent is the TC classid for the ingress hook:
// TC_H_MAJ(TC_H_CLSACT) | TC_H_MIN_INGRESS = (0xFFF2 << 16) | 2
const tcIngressParent = uint32(0xFFF20002)

// AttachTC attaches the patch_panel program as a TC ingress filter on
// the interface with the given link index.
func (p *PatchPanel) AttachTC(linkIdx int) error {
	// Ensure the ingress qdisc exists (idempotent)
	qdisc := &netlink.Ingress{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: linkIdx,
			Parent:    netlink.HANDLE_ROOT,
		},
	}
	if err := netlink.QdiscAdd(qdisc); err != nil {
		if !isExistsErr(err) {
			return fmt.Errorf("add ingress qdisc on ifidx %d: %w", linkIdx, err)
		}
	}

	// Check if a filter with the same name already exists (idempotent)
	link, err := netlink.LinkByIndex(linkIdx)
	if err != nil {
		return fmt.Errorf("lookup link ifidx %d: %w", linkIdx, err)
	}
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
		return fmt.Errorf("add BPF filter on ifidx %d: %w", linkIdx, err)
	}
	return nil
}

// DetachTC removes the patch_panel BPF filter from the interface.
func (p *PatchPanel) DetachTC(linkIdx int) error {
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
	return netlink.FilterDel(filter)
}

func isExistsErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "exists")
}
