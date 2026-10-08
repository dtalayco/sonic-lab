/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

// tgen (topology generator) reads a topology YAML file and generates
// the docker-compose.yml and supporting scripts needed to run a SONiC
// virtual switch lab with webmesh.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"sonic-lab/tgen/internal/reader"
	"sonic-lab/tgen/internal/writer"
)

func main() {
	fs := flag.NewFlagSet("tgen", flag.ExitOnError)
	outputDir := fs.String("o", ".", "output directory (must be a direct child of the repo root)")
	fs.Parse(flag.Args())

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: tgen [-o output-dir] <topology.yml>\n")
		log.Fatal("missing topology file argument")
	}

	ymlPath := fs.Arg(0)

	if err := reader.Run(ymlPath, *outputDir); err != nil {
		log.Fatalf("Error reading config: %v", err)
	}
	if err := writer.New(*outputDir).Run(); err != nil {
		log.Fatalf("Error writing scripts: %v", err)
	}
	fmt.Printf("Wrote topology files to %s\n", *outputDir)
}