/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinkKeyExplicitID(t *testing.T) {
	l := Link{
		ID: "spine1-leaf1",
		Endpoints: []Endpoint{
			{Device: "spine1-linux", Port: "Ethernet0"},
			{Device: "leaf1-linux", Port: "Ethernet0"},
		},
	}
	want := "spine1-leaf1"
	if got := l.Key(); got != want {
		t.Errorf("Key() = %q, want %q", got, want)
	}
}

func TestLinkKeyDerived(t *testing.T) {
	l := Link{
		Endpoints: []Endpoint{
			{Device: "spine1-linux", Port: "Ethernet0"},
			{Device: "leaf1-linux", Port: "Ethernet4"},
		},
	}
	want := "spine1-linux:Ethernet0<->leaf1-linux:Ethernet4"
	if got := l.Key(); got != want {
		t.Errorf("Key() = %q, want %q", got, want)
	}
}

func TestLinkKeyEmpty(t *testing.T) {
	l := Link{Endpoints: []Endpoint{{Device: "a", Port: "b"}}}
	if got := l.Key(); got != "" {
		t.Errorf("Key() with 1 endpoint = %q, want empty", got)
	}
}

func TestLoadTopologyFileWrapped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "topo.yml")
	content := `topology:
  links:
    - endpoints:
        - device: spine1-linux
          port: Ethernet0
        - device: leaf1-linux
          port: Ethernet0
    - endpoints:
        - device: spine1-linux
          port: Ethernet4
        - device: leaf2-linux
          port: Ethernet0
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	top, err := LoadTopologyFile(path)
	if err != nil {
		t.Fatalf("LoadTopologyFile: %v", err)
	}
	if len(top.Links) != 2 {
		t.Fatalf("len(links) = %d, want 2", len(top.Links))
	}
	if top.Links[0].Endpoints[0].Device != "spine1-linux" {
		t.Errorf("links[0].endpoints[0].device = %q, want spine1-linux", top.Links[0].Endpoints[0].Device)
	}
	if top.Links[1].Endpoints[1].Port != "Ethernet0" {
		t.Errorf("links[1].endpoints[1].port = %q, want Ethernet0", top.Links[1].Endpoints[1].Port)
	}
}

func TestLoadTopologyFileFlat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "topo.yml")
	content := `links:
  - endpoints:
      - device: spine1-linux
        port: Ethernet0
      - device: leaf1-linux
        port: Ethernet0
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	top, err := LoadTopologyFile(path)
	if err != nil {
		t.Fatalf("LoadTopologyFile: %v", err)
	}
	if len(top.Links) != 1 {
		t.Fatalf("len(links) = %d, want 1", len(top.Links))
	}
}

func TestLoadTopologyFileMissing(t *testing.T) {
	_, err := LoadTopologyFile("/nonexistent/path.yml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestLoadTopologyFileInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(path, []byte(":\n  - invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadTopologyFile(path)
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}
}
