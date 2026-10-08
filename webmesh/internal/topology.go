/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Endpoint identifies a switch port by container name and port name.
type Endpoint struct {
	Device string `yaml:"device" json:"device"`
	Port   string `yaml:"port"   json:"port"`
}

// Link describes a point-to-point connection between two endpoints.
type Link struct {
	ID        string     `yaml:"id,omitempty" json:"id,omitempty"`
	Endpoints []Endpoint `yaml:"endpoints"    json:"endpoints"`
}

// Key returns a stable identifier for the link.
func (l Link) Key() string {
	if l.ID != "" {
		return l.ID
	}
	if len(l.Endpoints) == 2 {
		a, b := l.Endpoints[0], l.Endpoints[1]
		return fmt.Sprintf("%s:%s<->%s:%s", a.Device, a.Port, b.Device, b.Port)
	}
	return ""
}

// Topology is a set of links describing the desired wiring.
type Topology struct {
	Links []Link `yaml:"links" json:"links"`
}

// topologyFile handles both the wrapped format (topology: links: ...)
// and the flat format (links: ...) used by the webmesh daemon.
type topologyFile struct {
	Topology *Topology `yaml:"topology"`
	Links    []Link    `yaml:"links"`
}

// LoadTopologyFile reads a topology YAML file and returns the Topology.
// It accepts both the wrapped format used by sample.yml and the flat
// format used by the webmesh API.
func LoadTopologyFile(path string) (Topology, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Topology{}, fmt.Errorf("reading %s: %w", path, err)
	}
	var tf topologyFile
	if err := yaml.Unmarshal(data, &tf); err != nil {
		return Topology{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	if tf.Topology != nil {
		return *tf.Topology, nil
	}
	return Topology{Links: tf.Links}, nil
}