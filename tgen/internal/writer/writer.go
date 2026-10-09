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
		sb.WriteString(fmt.Sprintf("    entrypoint: [\"/bin/bash\", \"/sonic_entry.sh\", \"%s-linux\"]\n", name))
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

echo "Waiting for vnet file /tmp/vnet_ready/$1"

while [ ! -f "/tmp/vnet_ready/$1" ]; do
    sleep 3
done

echo "vnet ready, starting SONiC..."
exec /usr/local/bin/supervisord -n
`)
	return os.WriteFile(filepath.Join(w.OutputDir, "sonic_entry.sh"), data, 0o755)
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
