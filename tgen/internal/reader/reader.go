/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package reader

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Topology Topology `yaml:"topology"`
}

type Topology struct {
	Name     string            `yaml:"name"`
	Switches map[string]Switch `yaml:"switches"`
	Links    []Link            `yaml:"links"`
}

type Switch struct {
	Role  string                 `yaml:"role"`
	ASN   int                    `yaml:"asn"`
	Ports map[string]interface{} `yaml:"ports"`
}

type Link struct {
	Endpoints []Endpoint `yaml:"endpoints"`
}

type Endpoint struct {
	Device string `yaml:"device"`
	Port   string `yaml:"port"`
}

// Run reads the topology config at configPath and writes the generated
// docker-compose.yml into outputDir.
func Run(configPath, outputDir string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("creating output dir %q: %w", err)
	}

	if err := writeFile(filepath.Join(outputDir, "docker-compose.yml"), []byte(generateDockerCompose(cfg))); err != nil {
		return fmt.Errorf("writing docker-compose.yml: %w", err)
	}

	// Copy the topology file into the output dir so the compose file can reference it.
	if err := writeFile(filepath.Join(outputDir, "sample.yml"), data); err != nil {
		return fmt.Errorf("writing sample.yml: %w", err)
	}

	return nil
}

func generateDockerCompose(cfg Config) string {
	var sb strings.Builder

	sb.WriteString("version: \"3.9\"\n\n")

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

	sb.WriteString("services:\n")

	// Webmesh service
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
	sb.WriteString("      - ./sample.yml:/etc/webmesh/topology.yml:ro\n")
	sb.WriteString("      - /var/run/docker.sock:/var/run/docker.sock:ro\n")
	sb.WriteString("      - ./tmp/vnet_ready:/tmp/vnet_ready:rw\n")
	sb.WriteString("    healthcheck:\n")
	sb.WriteString("      test: [\"CMD-SHELL\", \"curl -sf http://localhost:9090/v1/status | grep -q '\\\"healthy\\\":true'\"]\n")
	sb.WriteString("      interval: 2s\n")
	sb.WriteString("      timeout: 3s\n")
	sb.WriteString("      retries: 60\n\n")

	// Backing linux containers
	sb.WriteString("  ###########################\n")
	sb.WriteString("  # Backing Linux containers #\n")
	sb.WriteString("  ###########################\n\n")

	keys := make([]string, 0, len(cfg.Topology.Switches))
	for k := range cfg.Topology.Switches {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, name := range keys {
		sb.WriteString(fmt.Sprintf("  %s-linux:\n", name))
		sb.WriteString("    <<: *debian-common\n")
		sb.WriteString(fmt.Sprintf("    container_name: %s-linux\n", name))
		sb.WriteString(fmt.Sprintf("    command: [\"bash\", \"/init_linux.sh\", \"%s-linux\"]\n\n", name))
	}

	// SONiC containers
	sb.WriteString("  ###########################\n")
	sb.WriteString("  # SONiC VS containers      #\n")
	sb.WriteString("  ###########################\n\n")

	for i, name := range keys {
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
		if i < len(keys)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// writeFile writes data to path, making shell scripts executable.
func writeFile(path string, data []byte) error {
	var perm os.FileMode = 0o644
	if strings.HasSuffix(path, ".sh") {
		perm = 0o755
	}
	return os.WriteFile(path, data, perm)
}