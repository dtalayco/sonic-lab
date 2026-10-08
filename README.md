# sonic-lab
[![Test Docker Compose](https://github.com/mestery/sonic-lab/actions/workflows/test-docker-compose.yml/badge.svg)](https://github.com/mestery/sonic-lab/actions/workflows/test-docker-compose.yml)

This repository contains a SONiC virtual lab built using `docker-sonic-vs`
container images. The lab builds a simple spine–leaf topology using an
eBPF-powered **webmesh** patch panel container for dynamic, API-driven
network wiring between switches.

## Repository Layout

```
├── webmesh/          # Go module: sonic-lab/webmesh
│   ├── cmd/webmesh/      # Server entrypoint
│   ├── cmd/webmeshctl/   # Operator CLI (wait, push, links)
│   ├── internal/         # Core packages (api, reconciler, veth, ebpf, ...)
│   ├── bpf/              # eBPF C program (patch_panel.c)
│   └── sample.yml        # Reference topology
├── tgen/             # Go module: sonic-lab/tgen
│   ├── main.go             # CLI entrypoint
│   └── internal/           # reader (YAML→compose), writer (scripts)
├── example/          # Working 3-switch spine-leaf topology
│   ├── docker-compose.yml
│   ├── sample.yml
│   ├── init_linux.sh
│   ├── sonic_entry.sh
│   └── tmp/vnet_ready/
├── Dockerfile.webmesh  # Multi-stage build for the webmesh container
├── Dockerfile.debian   # Backing Linux image (iproute2, util-linux, docker.io)
├── start.sh            # Convenience script to start the example lab
└── docs/webmesh-design.md
```

## Topology

```
        +--------+
        | Spine  |
        +--------+
        ||      ||
   link1||      ||link2
        ||      ||
+--------+      +--------+
| Leaf1  |      | Leaf2  |
+--------+      +--------+
```

All inter-switch links are managed by the **webmesh** container, which uses
eBPF TC programs (`bpf_redirect_peer`) to wire veth pairs between switch
network namespaces. Links can be dynamically created, destroyed, and
flapped at runtime via a REST API.

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                     webmesh container                            │
│                                                                  │
│  ┌────────────┐    ┌─────────────────────────────────────────┐   │
│  │  API Server │    │         eBPF Patch Panel               │   │
│  │  (Go/HTTP) │◄──►│  TC ingress on wm-* veth endpoints     │   │
│  └────────────┘    │  bpf_redirect_peer → remote switch      │   │
│                     └─────────────────────────────────────────┘   │
│                                                                  │
│  netns: webmesh (holds wm-* veth endpoints)                      │
└──────────────────────────────────────────────────────────────────┘
         │ veth                    │ veth
         ▼                         ▼
┌──────────────┐           ┌──────────────┐
│  spine1 netns │           │  leaf1 netns │
│  (SONiC VS)  │           │  (SONiC VS)  │
└──────────────┘           └──────────────┘
```

## Host Requirements

This lab is intended to run on macOS using **Colima** with amd64 emulation
(Rosetta), since SONiC images are amd64-only.

- Docker (via Colima)
- At least 8 GB RAM allocated to Colima
- `clang` is NOT required on the host (BPF is compiled inside the container)

## Quick Start

### 1. Start Colima

```bash
colima start --vm-type vz --vz-rosetta --cpu 4 --memory 8 --mount-type virtiofs
```

### 2. Build images

```bash
docker compose -f example/docker-compose.yml build
```

Or use the convenience script (also pulls the SONiC image on first run):

```bash
./start.sh -l
```

### 3. Start the lab

```bash
./start.sh
```

Or manually:

```bash
docker compose -f example/docker-compose.yml up -d
```

The webmesh container starts first and performs a healthcheck. SONiC
containers are gated on `webmesh` being healthy (all desired links
applied). Once healthy, SONiC boots and attaches to the pre-wired
interfaces.

### 4. Wait and verify

SONiC takes a few minutes to boot. Watch progress:

```bash
docker logs -f sonic-spine1
```

Once SONiC is up, verify connectivity:

```bash
# Check that interfaces are up
docker exec sonic-spine1 ip link show | grep -E "Ethernet|UP"

# Ping between switches (replace with actual loopback IPs from your topology)
docker exec sonic-spine1 ping -c 3 10.0.0.2
```

## Webmesh Web UI

The webmesh container serves a dashboard at:

```
http://localhost:8092
```

This shows all links, their state (up/down), and packet/byte stats from
the eBPF program. The page auto-refreshes every 2 seconds.

## Webmesh REST API

Base URL: `http://localhost:8092`

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/topology` | GET | Current desired topology |
| `/v1/topology` | PUT | Replace desired topology (YAML or JSON) |
| `/v1/links` | GET | List all links with state + stats |
| `/v1/links/{key}` | GET | Inspect a single link |
| `/v1/links/{key}/up` | POST | Bring a link up |
| `/v1/links/{key}/down` | POST | Take a link down (flap) |
| `/v1/stats` | GET | Aggregate eBPF stats |
| `/v1/health` | GET | Healthcheck (desired == applied) |

### Example: Flap a link

```bash
# Take the spine1-leaf1 link down
curl -X POST http://localhost:8092/v1/links/spine1-leaf1/down

# Verify SONiC detects the link-down
docker exec sonic-spine1 show interfaces status

# Bring it back up
curl -X POST http://localhost:8092/v1/links/spine1-leaf1/up
```

### Example: View link stats

```bash
curl -s http://localhost:8092/v1/stats | python3 -m json.tool
```

## Topology File

The desired topology is defined in a YAML file (e.g., `example/sample.yml`)
and mounted into the webmesh container at `/etc/webmesh/topology.yml`. The
file watcher polls for changes (mtime) and the reconciler applies
differences every 5 seconds.

Example topology (`example/sample.yml`):

```yaml
topology:
  name: spine-leaf-lab
  switches:
    spine1:
      role: spine
      asn: 65000
      ports:
        Ethernet0: {}
        Ethernet4: {}
        Ethernet8: {}
        Ethernet12: {}
    leaf1:
      role: leaf
      asn: 65101
      ports:
        Ethernet0: {}
        Ethernet4: {}
    leaf2:
      role: leaf
      asn: 65102
      ports:
        Ethernet0: {}
        Ethernet4: {}
  links:
    - endpoints:
        - device: spine1-linux
          port: Ethernet0
        - device: leaf1-linux
          port: Ethernet0
    - endpoints:
        - device: spine1-linux
          port: Ethernet4
        - device: leaf1-linux
          port: Ethernet4
    - endpoints:
        - device: spine1-linux
          port: Ethernet8
        - device: leaf2-linux
          port: Ethernet0
    - endpoints:
        - device: spine1-linux
          port: Ethernet12
        - device: leaf2-linux
          port: Ethernet4
```

To change the topology at runtime, edit the mounted file (or use the PUT
API) and the reconciler will apply changes within ~5 seconds.

## Generating New Topologies with tgen

The `tgen` tool reads a topology YAML file and generates a complete
`docker-compose.yml` + supporting scripts for a new lab:

```bash
cd tgen
go build -o tgen .
./tgen -o ../example/ sample.yml
```

> **Note:** The output directory must be a direct child of the repo root
> (e.g., `example/`, `mylab/`) because the generated compose file uses
> relative paths (`context: ..`) to reference `Dockerfile.webmesh` and
> `Dockerfile.debian`.

## webmeshctl CLI

The `webmeshctl` tool provides convenience commands for interacting with a
running webmesh instance:

```bash
cd webmesh
go build -o webmeshctl ./cmd/webmeshctl

# Wait for webmesh to become healthy
./webmeshctl wait

# Push a new topology at runtime
./webmeshctl push -config my-topology.yml

# List all wired links
./webmeshctl links
```

## Logging Into the Switches

```bash
docker exec -it sonic-spine1 bash
docker exec -it sonic-leaf1 bash
docker exec -it sonic-leaf2 bash
```

## Cleaning Up

```bash
docker compose -f example/docker-compose.yml down
```

To also remove the webmesh image:

```bash
docker compose -f example/docker-compose.yml down && docker rmi sonic-lab/webmesh:latest
```

## How the Networking Works

### eBPF Patch Panel

Each link uses **two veth pairs**:

```
Switch A netns          webmesh netns              Switch B netns
┌────────────┐      ┌─────────────────────┐      ┌────────────┐
│ EthernetX  │◄──┐  │  wm-a-X  ═══════  wm-b-Y │  ──┐EthernetY│
│  (TAP)     │   └──│  (tc ingress)  (tc ingress)│  ──┘ (TAP)  │
└────────────┘      └─────────────────────┘      └────────────┘
```

The eBPF program is attached to the TC ingress of both webmesh-side
endpoints. On packet arrival, it looks up the ingress ifindex in
`link_map` to find the peer ifindex, then calls
`bpf_redirect_peer(peer_ifidx)` which delivers the packet directly to
the veth peer (i.e., the switch's Ethernet interface) with minimal
overhead — no L2 switch, no routing, just a redirect.

### Why This Design

* Zero-copy packet forwarding via `bpf_redirect_peer`
* Dynamically rewirable at runtime (update map, no interface changes)
* Link flapping without tearing down veths
* Per-link packet/byte statistics for observability
* SONiC is unaware of the mechanism — it sees normal Ethernet link state

## Notes

* All veth interfaces are created and managed by webmesh — do not
  modify them manually
* The webmesh container requires `privileged: true` and access to
  `/var/run/docker.sock` (for namespace operations)
* The reconciler retries every 5 seconds until desired state is applied
* State is persisted to `/var/run/webmesh/state.json` inside the container

## References

- [SONiC Documentation](https://github.com/sonic-net/SONiC)
- [webmesh Design Document](docs/webmesh-design.md)
- [libbpf Documentation](https://github.com/libbpf/libbpf)
- [cilium/ebpf](https://github.com/cilium/ebpf)