//go:build !linux

/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import "fmt"

// AttachTC is a stub on non-Linux platforms. TC operations require Linux.
func (p *PatchPanel) AttachTC(linkIdx int) error {
	return fmt.Errorf("TC attach not supported on this platform")
}

// DetachTC is a stub on non-Linux platforms.
func (p *PatchPanel) DetachTC(linkIdx int) error {
	return fmt.Errorf("TC detach not supported on this platform")
}
