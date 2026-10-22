/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import "testing"

func TestWmIfname(t *testing.T) {
	if got := wmIfname(0); got != "wm-eth0" {
		t.Errorf("wmIfname(0) = %q, want wm-eth0", got)
	}
	if got := wmIfname(127); got != "wm-eth127" {
		t.Errorf("wmIfname(127) = %q, want wm-eth127", got)
	}
}

func TestSwIfname(t *testing.T) {
	if got := swIfname(5); got != "sw-eth5" {
		t.Errorf("swIfname(5) = %q, want sw-eth5", got)
	}
}
