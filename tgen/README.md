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
| `-links <int>` | Parallel links per switch pair (default 1; enables ECMP) |
| `-h <int>` | Number of hosts attached to each switch |
| `-p <int>` | Number of ports per host (default 1; each port maps to a different switch) |
| `-srv6` | Enable SRv6 overlay (locators, SIDs, seg6 kernel routes) |
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

# Spine-leaf with ECMP: 2 spines, 4 leaves, 4 parallel links per pair
go run . -s 2 -l 4 -links 4 -o ../ecmp/

# Spine-leaf with SRv6 overlay
go run . -s 2 -l 4 -srv6 -o ../srv6-lab/

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
   - `sonic_entry.sh` — SONiC container entrypoint (waits for vnet, starts supervisord,
     creates Loopback0, loads FRR config, optionally loads SRv6 routes)
   - `frr/<switch>.conf` — per-switch FRR configuration (BGP unnumbered + optional SRv6)
   - `srv6/<switch>.routes` — per-switch seg6 kernel routes (only when `-srv6`)
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

- **Loopback0** — the VTEP address, created by `sonic_entry.sh` as a
  dummy interface before BGP starts
- **BGP unnumbered** — one neighbor per switch-to-switch link, using the
  kernel interface name (`Ethernet0`, `Ethernet4`, …) with the `interface`
  keyword and a shared `TRANSIT` peer-group (sessions established over
  link-local; `ebgp-multihop` suppresses TTL check)
- **IPv6 address family** — each switch advertises its VTEP `/128` prefix;
  `next-hop-self` is set for multipath

Host connections are excluded from the BGP neighbor list. The FRR config
is mounted at `/tmp/frr.conf` (read-only) inside each SONiC container and
loaded explicitly via `vtysh -f` after `bgpd` is running.

### SRv6 Overlay (`-srv6`)

When `-srv6` is specified, tgen adds an SRv6 overlay on top of the BGP
unnumbered underlay. The address scheme is:

| Purpose | Prefix | Interface |
|---------|--------|-----------|
| VTEP (End SID) | `fd00:0:0:<asn>::1/128` | Loopback0 |
| Locator | `fd00:0:0:<asn>::/64` | (FRR only) |
| Overlay service | `2001:db8:<asn>::/64` | Loopback0 |

Per-switch artifacts:

- **FRR config** adds a `segment-routing srv6` block with a locator and a
  static End SID (`behavior uN`), plus `segment-routing srv6` inside the
  BGP router section (enables SID advertisement/distribution).
- **`srv6/<name>.routes`** — a shell script that:
  1. Adds the overlay `/128` to Loopback0
  2. Programs a seg6 kernel route for each remote switch's overlay prefix:
     ```
     ip -6 route add <remote-overlay>/64 encap seg6 mode encap segs <remote-vtep>
     ```
     The single-segment SRH (remote VTEP = End behavior) causes the kernel
     to encapsulate the packet with an outer IPv6 header destined to the
     remote VTEP. The BGP underlay routes the outer packet; the remote
     switch decapsulates and delivers the inner packet to its own Loopback0.

The routes file is mounted at `/srv6.routes` (read-only) and executed by
`sonic_entry.sh` after the FRR config loads.

**Verifying the control plane:**

```bash
# From any switch, confirm FRR loaded the SRv6 config:
docker exec sonic-leaf1 vtysh -c "show segment-routing srv6 locator"
docker exec sonic-leaf1 vtysh -c "show segment-routing srv6 sid"

# Confirm BGP is distributing VTEP prefixes (should show all switch VTEPs):
docker exec sonic-leaf1 vtysh -c "show ipv6 bgp" | grep "fd00:0:0"
```

**Verifying the data plane:**

```bash
# From any switch, ping the remote switch's overlay address:
docker exec sonic-leaf1 ping6 -c 3 2001:db8:fe4d::1
```

> **Known limitation (Colima 6.8 HWE kernel):** Cross-namespace seg6
> decapsulation is silently dropped in this environment. Packets arrive at
> the receiving interface (confirmed via `tcpdump -i Ethernet12`) but the
> kernel's seg6 input path drops them without incrementing any standard
> counter (`Ip6InDelivers`, `Ip6InDiscards`, `Ip6InHdrErrors` all remain
> zero). Setting `seg6_enabled=1` on all Ethernet interfaces does not fix
> it. The root cause is believed to be a restriction or bug in the Ubuntu
> 24.04 HWE kernel (6.8.0-64-generic) when processing seg6-encapsulated
> packets received on veth interfaces from a different network namespace.
>
> Local seg6 (same namespace, Loopback0→Loopback0) and in-container veth
> seg6 both work correctly. The sender side (encapsulation + ECMP
> distribution across parallel links) is confirmed working.
>
> If you run on a different kernel (e.g., mainline 6.8 with ftrace enabled,
> or a Linux VM with full tracing), the data plane may work. Test with:
> `ip seg6local add dev Loopback0 mode end` on the receiver, then ping.

**Kernel requirement:** The host kernel must have `CONFIG_IPV6_SEG6_LWTUNNEL=y`
(built-in or module). Colima's 6.8.0 kernel includes it. No special Docker
image is needed — stock `docker-sonic-vs:latest` (iproute2 6.1.0) supports
seg6 lwtunnel routes.

### Build

```bash
cd tgen
go build -o tgen .
```