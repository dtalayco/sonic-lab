/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

// webmeshctl is a small CLI for interacting with a running webmesh instance.
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
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "wait":
		cmdWait(os.Args[2:])
	case "push":
		cmdPush(os.Args[2:])
	case "links":
		cmdLinks(os.Args[2:])
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Usage: webmeshctl <command> [flags]

Commands:
  wait    Wait for webmesh to report healthy
  push    Push a topology YAML file to webmesh
  links   List wired links from webmesh

Run 'webmeshctl <command> -h' for command-specific flags.`)
}

const (
	defaultURL  = "http://localhost:9090"
	fallbackURL = "http://localhost:8092" // host port published by the example compose files
)

// candidateURLs returns the URLs to try: just -url if it was given,
// otherwise the default followed by the host-mapped fallback.
func candidateURLs(fs *flag.FlagSet, url string) []string {
	explicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "url" {
			explicit = true
		}
	})
	if explicit {
		return []string{url}
	}
	return []string{defaultURL, fallbackURL}
}

// resolveURL returns the first candidate that answers /v1/status, or the
// first candidate if none do so that errors report the expected URL.
func resolveURL(candidates []string) string {
	client := http.Client{Timeout: 2 * time.Second}
	for _, u := range candidates {
		resp, err := client.Get(u + "/v1/status")
		if err == nil {
			resp.Body.Close()
			return u
		}
	}
	return candidates[0]
}

func cmdWait(args []string) {
	fs := flag.NewFlagSet("wait", flag.ExitOnError)
	url := fs.String("url", defaultURL, "webmesh API URL; if not set, tries :9090 then :8092")
	timeout := fs.Duration("timeout", 120*time.Second, "max wait time")
	fs.Parse(args)
	candidates := candidateURLs(fs, *url)

	deadline := time.Now().Add(*timeout)
	for time.Now().Before(deadline) {
		for _, u := range candidates {
			resp, err := http.Get(u + "/v1/status")
			if err != nil {
				continue
			}
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
	log.Fatalf("timed out waiting for webmesh at %s", strings.Join(candidates, " or "))
}

func cmdPush(args []string) {
	fs := flag.NewFlagSet("push", flag.ExitOnError)
	config := fs.String("config", "", "topology YAML file (required)")
	url := fs.String("url", defaultURL, "webmesh API URL; if not set, tries :9090 then :8092")
	fs.Parse(args)

	if *config == "" {
		log.Fatal("push: -config is required")
	}

	data, err := os.ReadFile(*config)
	if err != nil {
		log.Fatalf("reading %s: %v", *config, err)
	}

	jsonBody, err := yamlToJSON(data)
	if err != nil {
		log.Fatalf("converting YAML to JSON: %v", err)
	}

	base := resolveURL(candidateURLs(fs, *url))
	req, err := http.NewRequest(http.MethodPut, base+"/v1/topology", strings.NewReader(string(jsonBody)))
	if err != nil {
		log.Fatalf("building request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("PUT %s/v1/topology: %v", base, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Println(string(body))
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("unexpected status: %d", resp.StatusCode)
	}
}

func cmdLinks(args []string) {
	fs := flag.NewFlagSet("links", flag.ExitOnError)
	url := fs.String("url", defaultURL, "webmesh API URL; if not set, tries :9090 then :8092")
	fs.Parse(args)

	base := resolveURL(candidateURLs(fs, *url))
	resp, err := http.Get(base + "/v1/links")
	if err != nil {
		log.Fatalf("GET %s/v1/links: %v", base, err)
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

func yamlToJSON(data []byte) ([]byte, error) {
	var v interface{}
	if err := yaml.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}