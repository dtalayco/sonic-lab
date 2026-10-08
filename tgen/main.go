/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

// tgen (topology generator) generates docker-compose.yml and supporting
// scripts for a SONiC virtual switch lab. Topologies are specified via
// command-line flags rather than input files:
//
//
//	# Spine-leaf: 2 spines, 4 leaves
//	tgen -s 2 -l 4 -o ../mylab/
//
//	# Full mesh: 4 nodes
//	tgen -m -n 4 -o ../mylab/
//
//	# Ring: 6 nodes
//	tgen -r -n 6 -o ../mylab/
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"sonic-lab/tgen/internal/topo"
	"sonic-lab/tgen/internal/writer"
)

func main() {
	fs := flag.NewFlagSet("tgen", flag.ExitOnError)

	outputDir := fs.String("o", ".", "output directory (must be a direct child of the repo root)")
	spines := fs.Int("s", 0, "number of spine switches (spine-leaf mode)")
	leaves := fs.Int("l", 0, "number of leaf switches (spine-leaf mode)")
	mesh := fs.Bool("m", false, "generate full mesh topology")
	ring := fs.Bool("r", false, "generate ring topology")
	nodes := fs.Int("n", 0, "number of nodes (for -m or -r)")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `tgen — SONiC virtual lab topology generator

Usage:
  tgen [flags]

Topology modes (pick one):
  -s <int>    Number of spine switches (spine-leaf mode, use with -l)
  -l <int>    Number of leaf switches (spine-leaf mode, use with -s)
  -m          Generate full mesh topology (use with -n)
  -r          Generate ring topology (use with -n)
  -n <int>    Number of nodes (for -m or -r)

Output:
  -o <dir>    Output directory (default ".", must be a direct child of repo root)

Examples:
  tgen -s 1 -l 4 -o ../mylab/     # 1 spine, 4 leaves
  tgen -s 2 -l 6 -o ../dc/        # 2 spines, 6 leaves
  tgen -m -n 5 -o ../mesh/        # Full mesh of 5 nodes
  tgen -r -n 8 -o ../ring/        # Ring of 8 nodes
`)
	}

	fs.Parse(os.Args[1:])

	// Validate mode selection
	opts := topo.Options{
		Spines: *spines,
		Leaves: *leaves,
		Mesh:   *mesh,
		Ring:   *ring,
		Nodes:  *nodes,
	}

	if opts.Mesh && opts.Ring {
		log.Fatal("cannot use -m and -r together")
	}
	if opts.Mesh && (*spines > 0 || *leaves > 0) {
		log.Fatal("cannot mix -m with -s/-l")
	}
	if opts.Ring && (*spines > 0 || *leaves > 0) {
		log.Fatal("cannot mix -r with -s/-l")
	}

	hasSpineLeaf := *spines > 0 || *leaves > 0
	if !opts.Mesh && !opts.Ring && !hasSpineLeaf {
		fs.Usage()
		log.Fatal("no topology mode specified (use -s/-l, -m, or -r)")
	}

	if opts.Mesh && *nodes < 2 {
		log.Fatal("-m requires -n >= 2")
	}
	if opts.Ring && *nodes < 3 {
		log.Fatal("-r requires -n >= 3")
	}

	// Resolve output directory to absolute path
	outDir, err := filepath.Abs(*outputDir)
	if err != nil {
		log.Fatalf("resolving output dir: %v", err)
	}

	// Verify output dir is a direct child of the repo root.
	// The generated compose file uses "context: .." to find Dockerfiles,
	// so the output dir must be one level below the repo root.
	repoRoot := filepath.Dir(outDir)
	if _, err := os.Stat(filepath.Join(repoRoot, "Dockerfile.webmesh")); err != nil {
		log.Fatalf("output dir %q does not appear to be a direct child of the repo root\n"+
			"(expected Dockerfile.webmesh in %q)\n"+
			"Use a path like '../mylab/' relative to this directory, or an absolute path.",
			*outputDir, repoRoot)
	}

	// Generate topology
	t, err := topo.Generate(opts)
	if err != nil {
		log.Fatalf("generating topology: %v", err)
	}

	// Write output files
	if err := writer.New(outDir, t).Run(); err != nil {
		log.Fatalf("writing output: %v", err)
	}

	fmt.Printf("Generated %s topology (%d switches, %d links) → %s\n",
		t.Name, len(t.Switches), len(t.Links), outDir)
	fmt.Println("Run ./start.sh in the output directory to launch the lab.")
}
