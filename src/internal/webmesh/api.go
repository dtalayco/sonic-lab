/*
Copyright © 2026 Kyle Mestery All rights reserved.
Use of this source code is governed by a BSD-style
license that can be found in the LICENSE file.
*/

package webmesh

import (
	"encoding/json"
	"net/http"
	"strings"
)

// API is the HTTP server exposing the webmesh control plane.
type API struct {
	rec     *Reconciler
	version string
}

// NewAPI creates an API server backed by the given reconciler.
func NewAPI(rec *Reconciler, version string) *API {
	return &API{rec: rec, version: version}
}

// Handler returns the HTTP handler for the API.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", a.handleStatus)
	mux.HandleFunc("/v1/links", a.handleLinks)
	mux.HandleFunc("/v1/links/", a.handleLinkKey)
	mux.HandleFunc("/v1/topology", a.handleTopology)
	return mux
}

// --- /v1/status ---

type StatusResponse struct {
	Healthy bool   `json:"healthy"`
	Desired int    `json:"desired"`
	Applied int    `json:"applied"`
	Version string `json:"version"`
}

func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	desired := a.rec.DesiredCount()
	applied := a.rec.AppliedCount()
	healthy := desired > 0 && desired == applied
	writeJSON(w, http.StatusOK, StatusResponse{
		Healthy: healthy,
		Desired: desired,
		Applied: applied,
		Version: a.version,
	})
}

// --- /v1/links ---

type LinksResponse struct {
	Links []Link `json:"links"`
}

func (a *API) handleLinks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, LinksResponse{Links: a.rec.AppliedLinks()})
	case http.MethodPost:
		var link Link
		if err := json.NewDecoder(r.Body).Decode(&link); err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := a.rec.AddLink(link); err != nil {
			httpError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, link)
	default:
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// --- /v1/links/{key} ---

func (a *API) handleLinkKey(w http.ResponseWriter, r *http.Request) {
	// Extract key from URL path: /v1/links/{key} or /v1/links/{key}/stats
	rest := r.URL.Path[len("/v1/links/"):]
	if rest == "" {
		httpError(w, http.StatusBadRequest, "missing link key")
		return
	}

	// Check for /v1/links/{key}/stats
	if strings.HasSuffix(rest, "/stats") {
		key := strings.TrimSuffix(rest, "/stats")
		a.handleLinkStats(w, r, key)
		return
	}

	key := rest
	switch r.Method {
	case http.MethodDelete:
		if err := a.rec.RemoveLink(key); err != nil {
			httpError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// --- /v1/links/{key}/stats ---

func (a *API) handleLinkStats(w http.ResponseWriter, r *http.Request, key string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	stats, err := a.rec.GetLinkStats(key)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// --- /v1/topology ---

type TopologyRequest struct {
	Links []Link `json:"links"`
}

type TopologyResponse struct {
	Desired   int      `json:"desired"`
	Applied   int      `json:"applied"`
	Created   int      `json:"created"`
	Deleted   int      `json:"deleted"`
	Unchanged int      `json:"unchanged"`
	Errors    []string `json:"errors,omitempty"`
}

func (a *API) handleTopology(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req TopologyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}

	created, deleted, errs := a.rec.SetDesired(Topology{Links: req.Links})
	unchanged := len(req.Links) - created
	if unchanged < 0 {
		unchanged = 0
	}

	writeJSON(w, http.StatusOK, TopologyResponse{
		Desired:   a.rec.DesiredCount(),
		Applied:   a.rec.AppliedCount(),
		Created:   created,
		Deleted:   deleted,
		Unchanged: unchanged,
		Errors:    errs,
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}