/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package writer

import (
	"fmt"
	"os"
	"path/filepath"
)

type Writer struct {
	OutputDir string
}

func New(outputDir string) *Writer {
	return &Writer{
		OutputDir: outputDir,
	}
}

func (w *Writer) Run() error {
	if err := os.MkdirAll(w.OutputDir, 0o755); err != nil {
		return fmt.Errorf("creating output dir %q: %w", err)
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

# Wait for vnet ready file in Debian container
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