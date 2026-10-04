/*
Copyright © 2026 Tyler Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"sonic-lab/internal/reader"
	"sonic-lab/internal/writer"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "generate":
		cmdGenerate(os.Args[2:])
	case "wait-webmesh":
		cmdWaitWebmesh(os.Args[2:])
	case "push-topology":
		cmdPushTopology(os.Args[2:])
	case "links":
		cmdLinks(os.Args[2:])
	default:
		// Backward compat: sonic-lab file.yml [output-dir]
		if strings.HasSuffix(os.Args[1], ".yml") || strings.HasSuffix(os.Args[1], ".yaml") {
			cmdGenerate(os.Args[1:])
		} else {
			printUsage()
			os.Exit(1)
		}
	}
}

func printUsage() {
	fmt.Println(`Usage: sonic-lab <command> [args]

Commands:
  generate <.yml> [output-dir]   Generate shell scripts from topology YAML
  wait-webmesh [-url URL]        Wait for webmesh API to report healthy
  push-topology -config FILE     Push topology to webmesh API
  links [-url URL]               List wired links from webmesh API

Legacy:
  sonic-lab <.yml> [output-dir]  (same as generate)`)
}

// cmdGenerate is the original script-generation mode.
func cmdGenerate(args []string) {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() < 1 {
		log.Fatal("generate: missing topology file argument")
	}
	ymlPath := fs.Arg(0)
	outputDir := "./scripts"
	if fs.NArg() >= 2 {
		outputDir = fs.Arg(1)
	}
	if err := reader.Run(ymlPath, outputDir); err != nil {
		log.Fatalf("Error reading config: %v", err)
	}
	if err := writer.New(outputDir).Run(); err != nil {
		log.Fatalf("Error writing scripts: %v", err)
	}
	fmt.Printf("Wrote topology files to %s\n", outputDir)
}

// cmdWaitWebmesh polls the webmesh /v1/status endpoint until it reports
// healthy or the timeout expires.
func cmdWaitWebmesh(args []string) {
	fs := flag.NewFlagSet("wait-webmesh", flag.ExitOnError)
	url := fs.String("url", "http://localhost:9090", "webmesh API URL")
	timeout := fs.Duration("timeout", 120*time.Second, "max wait time")
	fs.Parse(args)

	deadline := time.Now().Add(*timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(*url + "/v1/status")
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var status struct {
					Healthy bool `json:"healthy"`
				}
				if json.Unmarshal(body, &status) == nil && status.Healthy {
					fmt.Println("webmesh is ready")
					return
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	log.Fatalf("timed out waiting for webmesh at %s", *url)
}

// cmdPushTopology reads a topology YAML file and PUTs the links to the
// webmesh /v1/topology endpoint.
func cmdPushTopology(args []string) {
	fs := flag.NewFlagSet("push-topology", flag.ExitOnError)
	config := fs.String("config", "", "topology YAML file (required)")
	url := fs.String("url", "http://localhost:9090", "webmesh API URL")
	fs.Parse(args)

	if *config == "" {
		log.Fatal("push-topology: -config is required")
	}

	data, err := os.ReadFile(*config)
	if err != nil {
		log.Fatalf("reading %s: %v", *config, err)
	}

	// Convert YAML to JSON for the API call.
	// The webmesh API accepts the same format as the topology file.
	jsonBody, err := yamlToJSON(data)
	if err != nil {
		log.Fatalf("converting YAML to JSON: %v", err)
	}

	req, err := http.NewRequest(http.MethodPut, *url+"/v1/topology", strings.NewReader(string(jsonBody)))
	if err != nil {
		log.Fatalf("building request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("PUT %s/v1/topology: %v", *url, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Println(string(body))
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("unexpected status: %d", resp.StatusCode)
	}
}

// cmdLinks queries the webmesh /v1/links endpoint and prints the result.
func cmdLinks(args []string) {
	fs := flag.NewFlagSet("links", flag.ExitOnError)
	url := fs.String("url", "http://localhost:9090", "webmesh API URL")
	fs.Parse(args)

	resp, err := http.Get(*url + "/v1/links")
	if err != nil {
		log.Fatalf("GET %s/v1/links: %v", *url, err)
	}
	defer resp.Body.Close()

	var body struct {
		Links []struct {
			Endpoints []struct {
				Device string `json:"device"`
				Port   string `json:"port"`
			} `json:"endpoints"`
		} `json:"links"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		log.Fatalf("decoding response: %v", err)
	}
	for _, l := range body.Links {
		if len(l.Endpoints) == 2 {
			fmt.Printf("%s:%s <-> %s:%s\n", l.Endpoints[0].Device, l.Endpoints[0].Port, l.Endpoints[1].Device, l.Endpoints[1].Port)
		}
	}
}

// yamlToJSON converts a YAML document to JSON.
func yamlToJSON(data []byte) ([]byte, error) {
	var v interface{}
	if err := yaml.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
