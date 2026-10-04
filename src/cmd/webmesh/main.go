/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

// webmesh is a daemon that runs inside a privileged container and manages
// inter-switch veth wiring for SONiC virtual switch labs.
//
// In Phase 2, it loads an eBPF patch panel program that forwards packets
// between paired veth endpoints via bpf_redirect_peer(), enabling dynamic
// rewiring at runtime.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"sonic-lab/internal/webmesh"
)

func main() {
	addr := flag.String("addr", ":9090", "listen address")
	topologyFile := flag.String("topology", "/etc/webmesh/topology.yml", "topology file (empty to skip)")
	stateFile := flag.String("state", "/var/run/webmesh/state.json", "state file path")
	interval := flag.Duration("interval", 5*time.Second, "reconcile interval")
	bpfObj := flag.String("bpf", "/etc/webmesh/patch_panel.o", "eBPF object file (empty to disable eBPF)")
	version := flag.String("version", "0.3.0", "version string")
	flag.Parse()

	// Ensure state directory exists
	if dir := filepath.Dir(*stateFile); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("creating state dir %s: %v", dir, err)
		}
	}

	// Load eBPF patch panel (optional)
	var panel *webmesh.PatchPanel
	if *bpfObj != "" {
		p, err := webmesh.LoadPatchPanel(*bpfObj)
		if err != nil {
			log.Printf("warning: eBPF not available (%v); running in Phase 1 mode", err)
		} else {
			panel = p
			defer panel.Close()
			log.Println("eBPF patch panel loaded")
		}
	}

	topFile := *topologyFile
	if topFile == "" {
		topFile = ""
	}

	rec := webmesh.NewReconciler(*stateFile, topFile, *interval, panel)
	go rec.Run()

	api := webmesh.NewAPI(rec, *version)
	server := &http.Server{Addr: *addr, Handler: api.Handler()}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("shutting down")
		_ = server.Close()
	}()

	log.Printf("webmesh %s listening on %s", *version, *addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}