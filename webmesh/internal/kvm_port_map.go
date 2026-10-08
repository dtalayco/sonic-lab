/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

// Package webmesh — KVM platform port-to-kernel-interface mapping.
//
// SONiC VS (x86_64-kvm_x86_64-r0) uses SAI_VS_HOSTIF_USE_TAP_DEVICE=true.
// The SAI layer creates TAP devices named after the logical port names
// (Ethernet0, Ethernet4, ...) and maps them to kernel interfaces (eth1,
// eth2, ...) via a lane-based lookup using lanemap.ini and port_config.ini.
//
// Webmesh must create veth endpoints named after the kernel interfaces
// (ethN), not the logical port names, so that SAI VS can correctly
// associate them with its TAP devices.
//
// The mapping below is derived from the canonical files:
//   /usr/share/sonic/hwsku/lanemap.ini.orig
//   /usr/share/sonic/hwsku/port_config.ini.orig
//
// Each entry matches by lane number:
//
//	logical port   lanes           kernel if
//	Ethernet0    25,26,27,28     eth1
//	Ethernet4    29,30,31,32     eth2
//	...etc.
package webmesh

import (
	"log"
	"regexp"
)

// kvmPortMap maps SONiC logical port names to kernel interface names
// for the x86_64-kvm_x86_64-r0 platform.
var kvmPortMap = map[string]string{
	"Ethernet0":   "eth1",
	"Ethernet4":   "eth2",
	"Ethernet8":   "eth3",
	"Ethernet12":  "eth4",
	"Ethernet16":  "eth5",
	"Ethernet20":  "eth6",
	"Ethernet24":  "eth7",
	"Ethernet28":  "eth8",
	"Ethernet32":  "eth9",
	"Ethernet36":  "eth10",
	"Ethernet40":  "eth11",
	"Ethernet44":  "eth12",
	"Ethernet48":  "eth13",
	"Ethernet52":  "eth14",
	"Ethernet56":  "eth15",
	"Ethernet60":  "eth16",
	"Ethernet64":  "eth17",
	"Ethernet68":  "eth18",
	"Ethernet72":  "eth19",
	"Ethernet76":  "eth20",
	"Ethernet80":  "eth21",
	"Ethernet84":  "eth22",
	"Ethernet88":  "eth23",
	"Ethernet92":  "eth24",
	"Ethernet96":  "eth25",
	"Ethernet100": "eth26",
	"Ethernet104": "eth27",
	"Ethernet108": "eth28",
	"Ethernet112": "eth29",
	"Ethernet116": "eth30",
}

// ethRe matches kernel interface names already in "ethN" form.
var ethRe = regexp.MustCompile(`^eth\d+$`)

// kernelIfname returns the kernel interface name corresponding to a
// SONiC logical port name. If the port name is already in "ethN" form
// it is returned as-is. If the port name is not in the KVM mapping,
// it is returned unchanged (with a warning logged).
func kernelIfname(port string) string {
	if ethRe.MatchString(port) {
		return port
	}
	if k, ok := kvmPortMap[port]; ok {
		return k
	}
	log.Printf("warning: no KVM port mapping for %q, using as-is", port)
	return port
}