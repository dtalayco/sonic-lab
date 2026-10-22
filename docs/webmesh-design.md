# Webmesh: eBPF Patch Panel for SONiC VS Lab

## 1. Executive Summary

This document proposes a new intermediary container, **webmesh**, that acts as a
centralized patch panel for all inter-switch veth wiring in the SONiC virtual
lab. The webmesh container runs two components:

1. **An eBPF data plane** — a TC (traffic-control) program that forwards
   packets between paired veth endpoints living in the webmesh network
   namespace. Forwarding rules are stored in a BPF hash map and can be
   updated at runtime without touching any network devices.

2. **A REST API server** — a small HTTP service that accepts topology
   descriptions (the same YAML the outer Go tool already parses) and
   reconciles the actual veth/eBPF state to match.

The outer `sonic-lab` Go program becomes a thin orchestrator: it starts
containers, waits for the webmesh API to be healthy, pushes the desired
topology, and starts SONiC. The manual `colima ssh sudo ./create_vnet.sh`
step is eliminated entirely.

---

## 2. Current Architecture

```
Host (Colima VM)
│
├── docker daemon
│   ├── spine1-linux   (debian, owns netns, sleep infinity)
│   ├── leaf1-linux    (debian, owns netns, sleep infinity)
│   ├── leaf2-linux    (debian, owns netns, sleep infinity)
│   ├── sonic-spine1   (shares spine1-linux netns)
│   ├── sonic-leaf1    (shares leaf1-linux netns)
│   └── sonic-leaf2    (shares leaf2-linux netns)
│
└── create_vnet.sh  ←  run manually via `colima ssh sudo …`
        for each link:
          ip link add  vXXX  type veth peer name vYYY
          ip link set  vXXX  netns <spine-pid>   → rename EthernetN
          ip link set  vYYY  netns <leaf-pid>    → rename EthernetM
          ip link set … up
```

**Startup is a three-phase manual sequence:**

| Phase | Action | How |
|-------|--------|-----|
| 1 | Start backing Linux containers | `docker compose up -d *-linux` |
| 2 | Create veth topology | `colima ssh sudo ./create_vnet.sh` |
| 3 | Start SONiC containers | `docker compose up -d` |

The phases are strictly ordered and the readiness signal between phase 1 and
phase 3 is a file touched inside the Linux containers
(`/tmp/vnet_ready/<name>`), polled by `sonic_entry.sh`.

### Problems

| # | Problem | Impact |
|---|---------|--------|
| 1 | `create_vnet.sh` must be run manually via `colima ssh` | Fragile, easy to forget, not automatable from the Go tool |
| 2 | Three-phase startup with manual ordering | Error-prone; no single entry point |
| 3 | Readiness file is a hack | Race conditions; no structured health signal |
| 4 | No dynamic rewiring | Topology is frozen after `create_vnet.sh` runs; adding a link requires re-running the script and restarting containers |
| 5 | No observability hook | All inter-switch traffic is invisible; no tap, mirror, or QoS point |
| 6 | Wiring logic lives in a shell script | Hard to test, no type safety, no error recovery |

---

## 3. Proposed Architecture

### 3.1 High-Level Diagram

```
                        ┌───────────────────────────────┐
                        │       webmesh container       │
                        │   ┌───────────────────────┐   │
                        │   │   API Server (:9090)  │   │
                        │   │   (Go, net/http)      │   │
                        │   └────────┬──────────────┘   │
                        │            │ manages          │
                        │   ┌────────▼──────────────┐   │
                        │   │  eBPF TC Program      │   │
                        │   │  (patch_panel.o)      │   │
                        │   │  ┌─────────────────┐  │   │
                        │   │  │   BPF map       │  │   │
                        │   │  │  ifidx→peer     │  │   │
                        │   │  └─────────────────┘  │   │
                        │   └───────────────────────┘   │
                        │                               │
                        │ netns:                        │
                        │ wm-sp1-e0 ──┐    ┌── wm-l1-e0 │
                        │ wm-sp1-e4 ──┤eBPF├── wm-l1-e4 │
                        │ wm-sp1-e8 ──┤    └── wm-l2-e0 │
                        │ wm-sp1-e12 ─┘        wm-l2-e4 │
                        └───┬───────────┬──────────┬────┘
                            │ veth      │ veth     │ veth
                       ┌────▼───┐  ┌────▼───┐  ┌───▼────┐
                       │spine1  │  │ leaf1  │  │ leaf2  │
                       │(sonic) │  │(sonic) │  │(sonic) │
                       │Eth0 Eth│  │Eth0Eth4│  │Eth0Eth4│
                       │4 Eth8  │  └────────┘  └────────┘
                       │Eth12   │
                       └────────┘
```

Each link is a **two-veth, one-eBPF-redirect** path:

```
  switch A netns          webmesh netns             switch B netns
  ┌──────────┐  veth  ┌──────────────┐  eBPF  ┌──────────────┐  veth  ┌──────────┐
  │ EthernetX│◄──────►│ wm-a-x       │◄──────►│ wm-b-y       │◄──────►│ EthernetY│
  └──────────┘        └──────────────┘        └──────────────┘        └──────────┘
                              ingress TC: bpf_redirect_peer(wm-b-y)
```

The eBPF program sits on the **ingress** of each webmesh-side veth endpoint.
When a packet arrives on `wm-a-x`, it looks up `wm-a-x`'s ifindex in the BPF
map, gets `wm-b-y`'s ifindex, and calls `bpf_redirect_peer()` which delivers
the packet to the peer of `wm-b-y` — i.e. `EthernetY` in switch B's netns.

### 3.2 Container Layout (docker-compose)

```yaml
services:
  # ── Webmesh patch panel ──────────────────────────────────
  webmesh:
    image: sonic-lab/webmesh:latest   # custom build
    platform: linux/amd64
    privileged: true                  # CAP_BPF, CAP_NET_ADMIN, CAP_SYS_ADMIN
    cap_add: [BPF, NET_ADMIN, SYS_ADMIN]
    volumes:
      - ./topology.yml:/etc/webmesh/topology.yml:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
    ports:
      - "9090:9090"                   # API server
    # No network_mode — uses default bridge so the host can reach the API
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9090/v1/status"]
      interval: 2s
      timeout: 1s
      retries: 30

  # ── Backing Linux containers (unchanged) ─────────────────
  spine1-linux:
    <<: *debian-common
    container_name: spine1-linux
    command: ["bash", "/init_linux.sh", "spine1-linux", "32"]

  leaf1-linux:
    <<: *debian-common
    container_name: leaf1-linux
    command: ["bash", "/init_linux.sh", "leaf1-linux", "32"]

  leaf2-linux:
    <<: *debian-common
    container_name: leaf2-linux
    command: ["bash", "/init_linux.sh", "leaf2-linux", "32"]

  # ── SONiC containers (unchanged) ─────────────────────────
  spine1:
    <<: *sonic-common
    container_name: sonic-spine1
    depends_on:
      webmesh:
        condition: service_healthy   # ← new: wait for webmesh API
      spine1-linux:
        condition: service_started
    network_mode: "service:spine1-linux"
    volumes:
      - ./tmp/vnet_ready:/tmp/vnet_ready:ro
      - /var/run/redis-vs/spine1:/var/run/redis
      - ./sonic_entry.sh:/sonic_entry.sh:ro
    entrypoint: ["/bin/bash", "/sonic_entry.sh", "spine1-linux"]

  # leaf1, leaf2 analogous …
```

Key changes from the current compose file:

| Change | Rationale |
|--------|-----------|
| New `webmesh` service | Houses the API + eBPF data plane |
| `depends_on: webmesh: condition: service_healthy` | Replaces the readiness-file hack with a structured health check |
| `privileged: true` on webmesh | Required for `CAP_BPF`, `CAP_NET_ADMIN`, and cross-namespace netlink |
| API port `9090` exposed | Allows the outer Go program (or `curl`) to drive wiring |

### 3.3 Component Breakdown

```
webmesh container
├── /usr/local/bin/webmesh          # Go binary (API server + reconciler)
├── /etc/webmesh/topology.yml       # mounted from host (read-only)
└── /var/run/webmesh/
    ├── patch_panel.o               # pre-compiled eBPF bytecode
    └── state.json                  # persistent state (optional)
```

The Go binary has two main goroutines:

```
main()
├── apiServer(:9090)        — handles HTTP requests
└── reconciler(ticker)      — periodically reconciles desired vs actual state
       ├── loadTopology()   — reads topology.yml or last API-push
       ├── diffTopology()   — computes create/delete sets
       └── applyDiff()      — creates/deletes veths, updates BPF map
```

---

## 4. Technical Design

### 4.1 eBPF Program

The eBPF program is a single TC ingress handler compiled to a BPF object
file with `clang`. It is intentionally minimal.

```c
/* patch_panel.c — compiled with:
 *   clang -O2 -target bpf -c patch_panel.c -o patch_panel.o
 */

#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

/*
 * link_map: ingress ifindex (in webmesh netns) → peer ifindex (in webmesh netns)
 *
 * bpf_redirect_peer(peer_ifindex) delivers the packet to the *peer* of the
 * interface with that ifindex.  Since both endpoints of a veth pair that live
 * in the webmesh netns have their peers in switch netnses, redirecting to the
 * webmesh-side ifindex of the *remote* endpoint causes delivery to the switch.
 */
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key,   __u32);   /* ingress ifindex  */
    __type(value, __u32);   /* peer ifindex     */
    __uint(max_entries, 1024);
} link_map SEC(".maps");

/* Optional: per-link stats (packets, bytes) for observability */
struct link_stats {
    __u64 packets;
    __u64 bytes;
};

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __type(key,   __u32);
    __type(value, struct link_stats);
    __uint(max_entries, 1024);
} stats_map SEC(".maps");

SEC("tc/ingress")
int patch_panel(struct __sk_buff *skb)
{
    __u32 in_if  = skb->ingress_ifindex;
    __u32 *peer  = bpf_map_lookup_elem(&link_map, &in_if);

    if (peer == NULL)
        return TC_ACT_SHOT;   /* unmapped — drop */

    /* Update stats */
    struct link_stats *s = bpf_map_lookup_elem(&stats_map, &in_if);
    if (s) {
        s->packets++;
        s->bytes   += skb->len;
    }

    /* Redirect to the peer of the remote webmesh endpoint.
     * bpf_redirect_peer(X) sends the skb to the veth-peer of ifindex X.
     * If X is wm-b-y (in webmesh), its peer is EthernetY (in switch B).
     */
    return bpf_redirect_peer(*peer, 0);
}

char _license[] SEC("license") = "GPL";
```

**Build:**

```dockerfile
# In the webmesh Dockerfile
FROM debian:bullseye AS bpf-builder
RUN apt-get update && apt-get install -y clang llvm libbpf-dev linux-headers-generic
COPY patch_panel.c /
RUN clang -O2 -target bpf -c /patch_panel.c -o /patch_panel.o

FROM debian:bullseye
COPY --from=bpf-builder /patch_panel.o /var/run/webmesh/patch_panel.o
# … install Go binary …
```

**Kernel requirement:** `bpf_redirect_peer()` requires Linux ≥ 4.18. The
Colima VM ships a modern kernel; this is not a concern in practice.

### 4.2 BPF Map Semantics

For a link `A:EthernetX ↔ B:EthernetY`, the webmesh namespace contains
two veth endpoints:

| veth pair | webmesh end (ifindex) | switch end (ifindex) |
|-----------|----------------------|---------------------|
| pair 1 | `wm-a-x` = **I₁** | `EthernetX` in A = I₁' |
| pair 2 | `wm-b-y` = **I₂** | `EthernetY` in B = I₂' |

Two entries are inserted into `link_map`:

```
I₁ → I₂     (packet on wm-a-x redirects to peer of wm-b-y = EthernetY in B)
I₂ → I₁     (packet on wm-b-y redirects to peer of wm-a-x = EthernetX in A)
```

The TC program is attached to **both** `wm-a-x` and `wm-b-y` (ingress).

### 4.3 Veth Lifecycle (Go, using `vishvananda/netlink`)

The reconciler manages veth pairs using the `vishvananda/netlink` package,
which provides typed wrappers around netlink syscalls.

```go
package webmesh

import (
    "github.com/vishvananda/netlink"
    "github.com/cilium/ebpf"
    "github.com/cilium/ebpf/asm"
)

type Link struct {
    ID        string   // "spine1:Ethernet0↔leaf1:Ethernet0"
    DevA      string   // "spine1-linux"  (container name)
    PortA     string   // "Ethernet0"
    DevB      string   // "leaf1-linux"
    PortB     string   // "Ethernet0"

    // Populated at wiring time
    WebmeshIfA  int  // ifindex of wm-a-x in webmesh netns
    WebmeshIfB  int  // ifindex of wm-b-y in webmesh netns
}

// wireLink creates the two veth pairs and updates the BPF map.
func (r *Reconciler) wireLink(l *Link) error {
    pidA, err := r.pidForContainer(l.DevA)  // via docker.sock
    // …
    pidB, err := r.pidForContainer(l.DevB)

    // 1. Create veth pair A: one end in webmesh, one end in switch A
    wmA := "wm-" + short(l.DevA) + "-" + short(l.PortA)
    swA := l.PortA  // "Ethernet0"
    if err := netlink.VethAdd(&netlink.Veth{
        LinkAttrs: netlink.LinkAttrs{Name: wmA},
        PeerName:  swA,
    }); err != nil { return err }

    // Move swA end into switch A's netns
    la, _ := netlink.LinkByName(wmA)
    if err := netlink.LinkSetNsPid(la, pidA); err != nil { return err }
    // Rename inside the target netns (requires nsenter or netlink with ns)
    // …

    // 2. Create veth pair B (analogous)
    // …

    // 3. Bring both webmesh-side endpoints up
    // …

    // 4. Record ifindices and update BPF map
    la, _ = netlink.LinkByName(wmA)
    lb, _ = netlink.LinkByName(wmB)
    r.bpfMap.Update(uint32(la.Attrs().Index), uint32(lb.Attrs().Index), ebpf.UpdateAny)
    r.bpfMap.Update(uint32(lb.Attrs().Index), uint32(la.Attrs().Index), ebpf.UpdateAny)

    // 5. Attach TC program to both webmesh-side endpoints
    // …
}
```

> **Note on renaming across namespaces:** `netlink.LinkSetNsPid()` moves the
> interface but does not rename it in the destination namespace. The rename
> must be done via `nsenter -t <pid> -n ip link set <old> name <new>`, or by
> opening the target namespace with `netlink.HandleAt(ns, …)`. The
> `vishvananda/netlink` package supports operating on a specific namespace
> via `netlink.NewHandleAt(ns)`.

### 4.4 TC Attachment from Go

```go
import (
    "github.com/cilium/ebpf"
    "github.com/vishvananda/netlink"
)

func attachTC(linkIdx int, prog *ebpf.Program) error {
    filter := &netlink.BpfFilter{
        Filter: netlink.Filter{
            LinkIndex: linkIdx,
            Parent:    netlink.HANDLE_INGRESS & 0xffff0000, // HANDLE_INGRESS
            Priority:  1,
            Protocol:  0x0003, // ETH_P_ALL
        },
        Fd:           prog.FD(),
        Name:         "patch_panel",
        DirectAction: true,   // skip classifier, execute directly
    }
    return netlink.FilterAdd(filter)
}
```

### 4.5 Reconciler Loop

The reconciler runs on a ticker (default: 5 s) and also triggers immediately
after an API call.

```
┌─────────────────────────────────────────────────────────┐
│  reconciler.tick()                                      │
│                                                         │
│  desired := loadTopology()                              │
│  actual  := scanWebmeshNetns()  // enumerate veths      │
│                                                         │
│  toCreate := desired - actual                           │
│  toDelete := actual  - desired                          │
│                                                         │
│  for each l in toCreate: wireLink(l)                    │
│  for each l in toDelete: unwireLink(l)                  │
│                                                         │
│  update BPF map (add new entries, delete old)           │
│  persist state.json (optional)                          │
└─────────────────────────────────────────────────────────┘
```

`unwireLink` is the inverse of `wireLink`:
1. Delete BPF map entries for both ifindices
2. Detach TC filter from both webmesh-side endpoints
3. Delete both veth pairs (`netlink.LinkDel`)

Because the veth endpoints in the switch namespaces are deleted along with
their webmesh-side peers, the SONiC containers will see their `EthernetX`
interfaces go administratively down (link-down). This is the expected and
correct behavior for a removed link.

---

## 5. API Design

The API is a small REST service listening on `:9090` inside the container.
All endpoints are JSON.

### 5.1 Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/status` | Health + eBPF program status |
| `GET` | `/v1/links` | List currently wired links |
| `PUT` | `/v1/topology` | **Declarative sync** — push full desired topology |
| `POST` | `/v1/links` | Add a single link |
| `DELETE` | `/v1/links/{id}` | Remove a single link |
| `GET` | `/v1/links/{id}/stats` | Per-link packet/byte counters (from BPF stats map) |

### 5.2 `GET /v1/status`

```json
{
  "healthy": true,
  "ebpf_loaded": true,
  "tc_attached": 8,
  "links_wired": 4,
  "version": "0.1.0"
}
```

Used by the Docker healthcheck and by the outer Go program to gate startup.

### 5.3 `PUT /v1/topology`

Request body is the same shape as the existing `sample.yml`, serialized as JSON:

```json
{
  "links": [
    {
      "id": "spine1:Ethernet0↔leaf1:Ethernet0",
      "endpoints": [
        { "device": "spine1-linux", "port": "Ethernet0" },
        { "device": "leaf1-linux",  "port": "Ethernet0" }
      ]
    },
    {
      "id": "spine1:Ethernet4↔leaf1:Ethernet4",
      "endpoints": [
        { "device": "spine1-linux", "port": "Ethernet4" },
        { "device": "leaf1-linux",  "port": "Ethernet4" }
      ]
    }
    // …
  ]
}
```

Response (200):

```json
{
  "applied": 4,
  "created": 2,
  "deleted": 0,
  "unchanged": 2,
  "errors": []
}
```

The operation is **idempotent**: calling it twice with the same body results
in zero changes on the second call.

### 5.4 `POST /v1/links` / `DELETE /v1/links/{id}`

For incremental changes without a full sync. Useful for dynamic rewiring
during a running lab session.

`POST` body:

```json
{
  "id": "spine1:Ethernet8↔leaf2:Ethernet0",
  "endpoints": [
    { "device": "spine1-linux", "port": "Ethernet8" },
    { "device": "leaf2-linux",  "port": "Ethernet0" }
  ]
}
```

### 5.5 `GET /v1/links/{id}/stats`

Reads the BPF `stats_map` for the given link's ifindices.

```json
{
  "link_id": "spine1:Ethernet0↔leaf1:Ethernet0",
  "a_to_b": { "packets": 15234, "bytes": 1828080 },
  "b_to_a": { "packets": 14901, "bytes": 1788120 }
}
```

This is the first observability hook — it gives per-link traffic counters
without any additional tooling.

---

## 6. Startup Sequence (New)

The three-phase manual sequence collapses into a single automated flow:

```
sonic-lab (Go, on host)
│
│  1. docker compose up -d webmesh
│     └── webmesh starts, loads eBPF, begins listening on :9090
│
│  2. docker compose up -d spine1-linux leaf1-linux leaf2-linux
│     └── backing containers start, touch readiness files
│
│  3. poll GET http://localhost:9090/v1/status until healthy
│
│  4. PUT http://localhost:9090/v1/topology  ← desired topology from YAML
│     └── webmesh creates all veth pairs, attaches TC, populates BPF map
│         webmesh touches /tmp/vnet_ready/<name> for each switch
│
│  5. docker compose up -d spine1 leaf1 leaf2
│     └── SONiC containers start, see Ethernet interfaces, run supervisord
│
└── done
```

The `sonic_entry.sh` readiness-file wait is **retained** as a safety net
(webmesh touches the same files), but the primary gating signal is now the
webmesh health check + topology sync, not a manual script.

The new `start.sh` becomes:

```bash
#!/usr/bin/env bash
set -e

# Load image if requested
# … (existing -l flag logic) …

echo "Starting webmesh…"
docker compose up -d webmesh

echo "Starting backing Linux containers…"
docker compose up -d spine1-linux leaf1-linux leaf2-linux

echo "Waiting for webmesh API…"
sonic-lab wait-webmesh --url http://localhost:9090

echo "Pushing topology…"
sonic-lab push-topology --config topology.yml --url http://localhost:9090

echo "Starting SONiC containers…"
docker compose up -d

echo
docker ps --filter "name=sonic-"
```

Or, the entire sequence can be driven by the Go tool directly via the
Docker SDK, eliminating `start.sh` altogether.

---

## 7. Implementation Plan

The work is split into three phases. Each phase is independently shippable.

### Phase 1 — Webmesh Container with API + Direct Veth (no eBPF)

**Goal:** Eliminate the manual `create_vnet.sh` step. Get a working
`docker compose up -d` one-liner.

This phase uses **direct veth pairs between switch namespaces** (the same
mechanism `create_vnet.sh` uses today) but driven by the webmesh API and
reconciler. No eBPF is involved yet.

| Task | Detail |
|------|--------|
| Build `webmesh` Docker image | Debian base + Go binary + `iproute2` + `docker.io` (client) |
| Implement API server | `GET /v1/status`, `PUT /v1/topology`, `GET /v1/links` |
| Implement reconciler | Read topology, diff, create/delete veths via `nsenter` + `ip link` |
| Implement readiness signalling | Touch `/tmp/vnet_ready/<name>` after wiring each switch |
| Update `docker-compose.yml` | Add `webmesh` service, wire `depends_on` |
| Update Go tool (`src/`) | Add `wait-webmesh` and `push-topology` subcommands |
| Remove `create_vnet.sh` | No longer needed |
| Update `start.sh` | New automated sequence |

**Exit criteria:**
- `./start.sh` brings up the full lab with no manual intervention
- `PUT /v1/topology` is idempotent
- Removing a link from the YAML and re-pushing deletes the veth

**Estimated effort:** 2–3 days

### Phase 2 — eBPF Patch Panel

**Goal:** Route all inter-switch traffic through the webmesh namespace
using eBPF TC redirection.

| Task | Detail |
|------|--------|
| Write `patch_panel.c` | TC ingress program with `link_map` + `stats_map` |
| Add BPF build stage to Dockerfile | `clang -target bpf` in a multi-stage build |
| Load eBPF from Go | Use `github.com/cilium/ebpf` to load `patch_panel.o` |
| Modify `wireLink` | Create two veths per link (webmesh-side + switch-side), attach TC |
| Modify `unwireLink` | Detach TC, delete map entries, delete veths |
| Implement `GET /v1/links/{id}/stats` | Read `stats_map` via cilium/ebpf |
| Verify kernel support | Add startup check for `bpf_redirect_peer` availability |

**Exit criteria:**
- All inter-switch traffic transits the webmesh namespace
- `tcpdump -i wm-sp1-e0` in the webmesh container captures switch-to-switch frames
- Per-link stats endpoint returns non-zero counters under traffic
- Latency increase vs. direct veth is < 2 µs per hop (measurable with `sockperf`)

**Estimated effort:** 3–5 days

### Phase 3 — Dynamic Rewiring & Observability

**Goal:** Make the lab truly dynamic. Add observability features.

| Task | Detail |
|------|--------|
| File watcher on `topology.yml` | Reconcile on file change without API call |
| `SIGHUP` handler | Reload topology on signal |
| Traffic mirroring | Add a second eBPF program (or extend the map) to mirror traffic to a tap interface for `tcpdump` / `wireshark` |
| Link flapping | Add `POST /v1/links/{id}/down` and `/up` to simulate link failures |
| Web UI (stretch) | Serve a simple HTML page on `:9090` showing topology + live stats |
| Remove backing Linux containers (stretch) | Let SONiC containers own their netnses directly; webmesh wires veths into SONiC PIDs |

**Estimated effort:** 5–8 days (excluding stretch items)

---

## 8. Webmesh Go Binary: Package Layout

```
src/
├── main.go                      # CLI entry point (existing, extended)
├── internal/
│   ├── reader/                  # existing YAML parser
│   ├── writer/                  # existing script generator
│   └── webmesh/                 # NEW: webmesh daemon (runs in container)
│       ├── main.go              #   daemon entry point
│       ├── api.go               #   HTTP handlers
│       ├── reconciler.go        #   diff + apply logic
│       ├── veth.go              #   veth create/delete (netlink + nsenter)
│       ├── ebpf.go              #   eBPF load, map ops, TC attach
│       └── docker.go            #   container PID lookup via Docker SDK
└── topologies/
    └── sample.yml               # moved from src/sample.yml
```

The `webmesh` package is compiled into a separate binary (`webmesh`) that is
`CMD` of the webmesh container. The existing `sonic-lab` binary gains new
subcommands:

```
sonic-lab generate <topology.yml> [output-dir]   # existing
sonic-lab wait-webmesh --url <url>               # poll /v1/status
sonic-lab push-topology --config <yml> --url <url>  # PUT /v1/topology
sonic-lab links --url <url>                      # GET /v1/links
```

---

## 9. Key Dependencies (Go)

| Package | Purpose |
|---------|---------|
| `github.com/cilium/ebpf` | Load eBPF objects, manage BPF maps, get program FDs |
| `github.com/vishvananda/netlink` | Create/delete veths, set namespace, add TC filters |
| `github.com/docker/docker/client` | Look up container PIDs (replaces `docker inspect` shell-out) |
| `gopkg.in/yaml.v3` | Parse topology YAML (existing) |
| `net/http` | API server (stdlib, no framework needed) |

The eBPF C program is the only non-Go artifact. It is compiled at Docker
image build time and loaded at runtime by the Go binary.

---

## 10. Alternatives Considered

### 10.1 Linux Bridge per Link

Instead of eBPF, place both webmesh-side veth endpoints on a per-link Linux
bridge.

```
wm-a-x ──┐
         ├── bridge0 ──► (floods to the other port)
wm-b-y ──┘
```

**Pros:** No eBPF required. Simpler to implement. Works on any kernel.
**Cons:** Bridge does MAC learning and (with 2 ports) a single flood per
unknown-dest. Adds ~1 µs latency vs. eBPF redirect. No per-packet hook for
QoS, mirroring, or policy. Harder to extend.

**Verdict:** Viable fallback if eBPF proves problematic in the Colima VM.
The API and reconciler design is identical; only the data-plane mechanism
changes.

### 10.2 Direct Veth Between Switch Namespaces (No Webmesh Netns)

The webmesh daemon creates veth pairs directly between the two switch
namespaces, with no intermediary namespace. This is the current
`create_vnet.sh` approach, but as a daemon.

**Pros:** Simplest. Lowest latency. No eBPF.
**Cons:** No central observability point. No patch-panel semantics.
Rewiring a link requires deleting and recreating the veth (brief link-down).

**Verdict:** This is Phase 1. It solves the automation problem but not the
observability or dynamic-rewiring problems.

### 10.3 OVN / Network Namespace Agent

Use a full CNI or overlay network (OVN, Calico, etc.) to manage the
namespaces.

**Pros:** Production-grade.
**Cons:** Massive overkill for a lab. SONiC VS expects specific Ethernet
interface names and TAP-backed ports; overlay networks abstract this away
and break the model.

**Verdict:** Not applicable.

---

## 11. Risks and Mitigations

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| eBPF `bpf_redirect_peer` not available in Colima VM kernel | Low | Phase 1 works without eBPF. Add kernel version check at webmesh startup; fall back to bridge mode |
| `CAP_BPF` not granted in Colima's Docker runtime | Medium | Use `privileged: true` (already the pattern for SONiC containers). Test on first Colima VM |
| Cross-namespace rename via netlink is unreliable | Medium | Fall back to `nsenter -t <pid> -n ip link set` via `exec.Command`. This is what `create_vnet.sh` does today and is proven |
| SONiC TAP devices conflict with veth names | Low | veth endpoints are named `EthernetN` (matching SONiC's expected names) — this is the same as today and works |
| BPF map full (exceeds `max_entries`) | Very low | `max_entries=1024` supports 512 links. Log a warning and reject the link if the map is full |
| Webmesh container restart loses wiring | Medium | State is derived from the topology file, not stored in BPF maps. On restart, the reconciler re-wires everything from the topology. Veths are deleted when the container's netns is destroyed, so a clean re-wire is safe |

---

## 12. Future Extensions

Once the patch panel is in place, several extensions become straightforward:

- **Traffic mirroring to a tap interface** — add a second eBPF program that
  duplicates frames to a `tap0` in the webmesh namespace; attach a
  `tcpdump` container to it.
- **Per-link QoS / rate limiting** — extend the eBPF program with `bpf_skb_adjust_len`
  or a token-bucket map keyed by link ID.
- **Packet injection** — expose a `POST /v1/links/{id}/inject` endpoint that
  sends crafted frames (e.g. BPDUs, LACP) into a link for testing.
- **Topology visualization** — the API already has all the data; serve a
  simple D3.js graph on `:9090`.
- **Multi-topology** — run multiple webmesh containers, each managing a
  subset of the lab.
- **Remove backing Linux containers** — have SONiC containers own their
  netnses directly. The webmesh daemon creates veths into SONiC container
  PIDs. This eliminates an entire layer of containers.

---

## 13. Summary of Changes to Existing Files

| File | Change |
|------|--------|
| `docker-compose.yml` | Add `webmesh` service; update `depends_on` for SONiC services |
| `create_vnet.sh` | **Deleted** (Phase 1) |
| `init_linux.sh` | Unchanged (still touches readiness file as safety net) |
| `sonic_entry.sh` | Unchanged (still waits for readiness file) |
| `start.sh` | Rewritten to call webmesh API instead of `colima ssh` |
| `src/main.go` | Add `wait-webmesh`, `push-topology`, `links` subcommands |
| `src/internal/reader/` | Unchanged |
| `src/internal/writer/` | Remove `writeStartScript` (or update it); remove `create_vnet.sh` generation |
| `src/internal/webmesh/` | **New** package — the webmesh daemon binary |
| `Dockerfile.webmesh` | **New** — multi-stage build (BPF compile + Go build) |
| `docs/design-document.md` | Update to reference this document |
