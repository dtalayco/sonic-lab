# tgen — Topology Generator

`tgen` generates a complete SONiC virtual lab from command-line flags. It
produces a `docker-compose.yml`, a `topology.yml` (consumed by webmesh),
and supporting shell scripts — ready to run with `./start.sh`.

## Usage

```
tgen [flags]
```

### Topology Modes

| Flag | Description |
|------|-------------|
| `-s <int>` | Number of spine switches (spine-leaf mode) |
| `-l <int>` | Number of leaf switches (spine-leaf mode) |
| `-m` | Generate full mesh topology |
| `-r` | Generate ring topology |
| `-n <int>` | Number of nodes (for `-m` or `-r`) |
| `-h <int>` | Number of hosts attached to each switch |
| `-p <int>` | Number of ports per host (default 1; each port maps to a different switch) |
| `-o <dir>` | Output directory (default `.`, must be a direct child of repo root) |

### Examples

```bash
# Classic spine-leaf: 1 spine, 4 leaves (each leaf has 1 uplink)
go run . -s 1 -l 4 -o ../mylab/

# Dual-spine: 2 spines, 6 leaves (each leaf has 2 uplinks)
go run . -s 2 -l 6 -o ../mylab/

# Full mesh: 5 nodes, every node connected to every other
go run . -m -n 5 -o ../mesh-lab/

# Ring: 8 nodes in a circular loop
go run . -r -n 8 -o ../ring-lab/

# Spine-leaf with hosts: 2 spines, 2 leaves, 2 hosts per switch, 2 ports per host
go run . -s 2 -l 2 -h 2 -p 2 -o ../lab-with-hosts/
```

### Hosts

The `-h` flag adds simulated host containers to the topology. Each switch
gets `-h` hosts, and each host has `-p` ports (default 1). Port `p` of a
host connects to switch `(homeSwitch + p) % totalSwitches`, giving
natural multi-homed redundancy.

Hosts are built from `Dockerfile.host` (a small Debian image with
`iproute2`, `tcpdump`, `bridge-utils`, `ifenslave`, `ethtool`, and
`iputils-ping`).

### How It Works

1. **Generate** — `tgen/internal/topo` builds the topology in memory
   (switch list + link list) from the flags.
2. **Write** — `tgen/internal/writer` emits:
   - `docker-compose.yml` — defines webmesh, backing Linux containers, and SONiC containers
   - `topology.yml` — the desired topology in the YAML format webmesh expects
   - `init_linux.sh` — backing container entrypoint (touches vnet-ready file)
   - `sonic_entry.sh` — SONiC container entrypoint (waits for vnet, starts supervisord)
   - `start.sh` — convenience script to build and launch everything
   - `tmp/vnet_ready/` — shared directory for readiness signaling

### Output Directory Constraint

The output directory **must be a direct child of the repo root** (e.g.,
`../mylab/` when running from `tgen/`). This is because the generated
`docker-compose.yml` uses `context: ..` to locate `Dockerfile.webmesh`
and `Dockerfile.debian` at the repo root.

### Port Allocation

Ports use step-4 Ethernet naming (`Ethernet0`, `Ethernet4`, `Ethernet8`,
…) to match the KVM platform's 30-lane map.

- **Spine-leaf**: Spine `j` allocates ports to leaves in order
  (`Ethernet0` → leaf1, `Ethernet4` → leaf2, …). Leaf `i` allocates
  ports to spines in order.
- **Mesh**: Node `i` connects to node `j` (j > i) on port index `j-1`.
  Node `j` connects back on port index `i`.
- **Ring**: Each node uses exactly 2 ports — `Ethernet0` (prev) and
  `Ethernet4` (next).

### Constraints

| Topology | Min nodes | Max nodes | Ports per node |
|----------|-----------|-----------|----------------|
| Spine-leaf | 1 spine + 1 leaf | 30 spines, 30 leaves | S per leaf, L per spine |
| Mesh | 2 | 31 | N-1 |
| Ring | 3 | unlimited | 2 |

(30 is the max due to the KVM 30-lane platform limitation.)

### BGP Unnumbered Underlay

Every switch gets an eBGP ASN and a deterministic VTEP IPv6 ULA loopback
address (`fd00:0:0:<asn>::1/128`). The generated `frr/<switch>.conf`
file configures FRR with:

- **Loopback0** — the VTEP address, created by `sonic_entry.sh` before
  supervisord starts
- **BGP unnumbered** — one neighbor per switch-to-switch link, using the
  kernel interface name (`eth0`, `eth1`, …) with the `unnumbered` keyword
  (sessions established over link-local)
- **IPv6 address family** — each switch advertises its VTEP `/128` prefix

Host connections are excluded from the BGP neighbor list. The FRR config
is mounted at `/etc/frr/frr.conf` inside each SONiC container; FRR picks
it up on startup via supervisord.

### Build

```bash
cd tgen
go build -o tgen .
```