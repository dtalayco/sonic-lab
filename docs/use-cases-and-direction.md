# Use Cases and Direction

**Status:** draft for discussion. These notes look at sonic-lab from the
point of view of the people using it: what they're trying to do, how they
want to drive the lab (by hand or from scripts), and what that implies for
the framework's structure and its testing strategy. They build on
[`design-document.md`](design-document.md) (the original generator plan)
and [`webmesh-design.md`](webmesh-design.md) (the patch panel).

## 1. What exists today

The framework already has several distinct layers, though they aren't
always separated cleanly:

| Layer | Today | NOS-specific? |
|---|---|---|
| Container runtime | Docker (Colima on macOS), Compose v2 | No |
| Node shells | A backing Debian container per switch owns the network namespace; the NOS container joins it (`network_mode: service:...`) | No |
| Wiring | webmesh: veth pairs through an eBPF patch panel; REST API, reconciler, link up/down, per-link stats, web UI, `webmeshctl` | Mostly no; see 4.2 |
| Topology generation | tgen: spine-leaf, mesh, ring; parallel links; hosts; optional SRv6 overlay | Mixed: graph is generic, output is SONiC + FRR |
| Node configuration | `sonic_entry.sh` + per-switch `frr.conf`: BGP unnumbered on every fabric port, Loopback0, optional SRv6/VPN | Yes |
| Tests | Go unit tests; CI matrix (spine-leaf, ring, mesh) that waits for webmesh and BGP | Partly |

The wiring layer is the most reusable piece. It knows nothing about
routing, and webmesh's runtime rewiring, link flapping and per-link stats
are useful for any containerized network.

## 2. Use cases

### UC1: Host-side protocol development

Developing or testing something that runs on end hosts: a transport or
congestion-control change, multipath (MPTCP), host bonding/LACP, a new
discovery or telemetry agent, PTP. The switches are infrastructure; the
interesting code is on the hosts.

Needs:

- Custom host images (today all hosts use `Dockerfile.host`), possibly
  different images for different hosts.
- Multi-homed hosts (exists: `-p`), with control over which switches each
  port lands on.
- A working network under the hosts without having to understand SONiC:
  either a known-good L2 config or a known-good L3 config.
- Packet capture at any point: on a host, a switch port or a webmesh link.
- Impairments (latency, loss, reordering, bandwidth limits) on chosen links.

### UC2: L2 feature work on the switches

VLANs (access and trunk), LACP port-channels, LLDP, STP/MSTP, MLAG, storm
control, L2 ACLs, or a new L2 protocol.

Needs:

- Switches with no routing configured, or routing that doesn't claim the
  switch-to-switch ports. Today every fabric port runs BGP unnumbered, and
  port bring-up in `sonic_entry.sh` is tied to the L3 path.
- Configurable L2 profiles on switch-to-switch ports (plain, trunk, LAG,
  LAG + trunk), not just on host-facing ports.
- Checks that don't depend on L3: oper status, LLDP adjacency, MAC tables,
  port-channel membership.

### UC3: L3 / routing work

BGP, OSPF/IS-IS, SRv6, EVPN/VXLAN, route policy, a new routing protocol or
a modified FRR.

Needs:

- Pluggable routing config: today's BGP-unnumbered fabric is one profile
  among several.
- The ability to run the routing layer over different L2 configs: plain
  ports, SVIs on trunks, port-channels.
- Swapping in custom daemon builds (a patched FRR, a new protocol daemon).
- Routing-state assertions: sessions, RIB/FIB contents, forwarding paths.

### UC4: The same test across many topologies and configs

A protocol or feature that works on a 2x2 spine-leaf should also work on a
ring, a mesh, a larger fabric, with ECMP, and over different L2 configs.
This is where most real bugs show up and where manual testing breaks down.

Needs:

- Parameterized lab generation, with the parameters recorded alongside the
  results.
- Deterministic naming, so a test can address "leaf1's first uplink" in
  every topology.
- Machine-readable results (JSON), so a matrix run can be summarized.
- Labs that can run in parallel (see 3.2).

### UC5: Failure and resilience testing

Link flaps, link loss under load, node restarts, partitions, slow links;
does the protocol converge, and how fast?

Needs:

- Link up/down (exists in webmesh).
- Impairments (the patch panel is the natural place; tc netem or an eBPF
  extension).
- Node restart that the rest of the lab survives. Today restarting the
  webmesh container replaces every veth, and SONiC VS doesn't recover the
  ports (seen while testing PR #30); switch restart behavior is untested.
- Timing: when did the event happen, when did the protocol reconverge.

### UC6: Manual exploration, demos and teaching

Bring up a lab, log in, poke at it, break things, explain it to someone.

Needs: a fast, reliable quick start; easy login to any node; the web UI;
readable names everywhere (`ip link`, the UI, logs); docs that match
reality.

### UC7: Regression testing of the framework and of NOS images

CI for this repo, and a way to qualify a new `docker-sonic-vs` build (or,
later, another NOS image) before using it.

Needs: the bring-up and forwarding test suites, a pinned matrix that runs
on every PR, and a larger one that runs nightly or on demand.

### UC8: Beyond SONiC

Other containerized network OSes (FRR-only nodes, other vendor NOS
containers, Linux bridge/OVS "switches", P4 software switches), or mixed
labs. This may mean forking the repo, with the deployment and wiring
layers as the shared core.

Needs: a clean boundary between the generic core (node shells, wiring,
topology graph, lab lifecycle, test harness) and the NOS-specific parts
(port naming, boot/readiness, config rendering). See section 4.

## 3. Manual and scripted use

Every use case above has a manual phase (figuring out what's going on) and
a scripted phase (making it repeatable). The framework should make moving
from one to the other cheap: whatever you did by hand should be easy to
capture in a script, and whatever a script sets up should be easy to step
into by hand.

### 3.1 Principles

- **One set of tools for both.** Tests and CI should call the same commands
  a person runs. Today the CI workflow implements bring-up and the BGP check
  inline in YAML, so they can't be run locally; `tests/smoke1.sh` (on the
  `smoke-tests` branch) does the same checks as a script. The CI should
  call the script.
- **Machine-readable everything.** Each command that reports state should
  have a JSON form: lab status, links, node readiness, test results.
  webmesh's API already is; tgen and the test scripts aren't yet.
- **Stable, predictable names.** Node names, port names and link keys
  should be derivable from the topology, so scripts don't have to discover
  them. (The `wm-ethN` change in PR #30 keeps the user-facing names
  `device:port` and pushes the opaque names to a verbose view.)
- **Idempotent, explicit lifecycle.** `up`, `wait-ready`, `down`, `clean`
  should each be safe to re-run, and `clean` should get you back to a known
  state after any failure.
- **Explicit readiness levels.** "Up" means different things for different
  use cases: containers running, links wired, ports oper-up, L2 adjacencies
  formed, routing converged. Each should be a separately waitable,
  reportable state (see 5.2).
- **Pause and inspect.** A test that fails should be able to leave the lab
  running in the failed state (`smoke1.sh -k`), and a person should be able
  to resume or re-run individual checks against a running lab.

### 3.2 Gaps today

| Gap | Effect | Direction |
|---|---|---|
| Fixed container names (`webmesh`, `sonic-*`, `host*`) and a fixed host port (8092) | Only one lab per machine; matrix runs are serial; a test can't run while you have a lab up | A per-lab name prefix and port, derived from the lab name |
| tgen takes only flags | Topologies are limited to the built-in shapes; a hand-edited topology can't be regenerated | Topology file as input (already item 3 in `design-document.md`), with flags as shorthand that produce one |
| Routing is always on and baked into the entrypoint | Pure L2 labs aren't possible (UC2) | Separate L2 and L3 config profiles (see 4.3) |
| No single lab CLI | Each task uses a different tool: tgen, `start.sh`, `docker compose`, `webmeshctl`, `docker exec` | A thin `lab` front end (generate, up, wait, status, exec, capture, flap, down, clean); could grow out of `webmeshctl` |
| Generated `start.sh` and the root `start.sh` differ; docs describe a 3-switch example and API endpoints that don't exist | Manual users hit surprises (UC6) | Treat docs and generated scripts as tested artifacts |
| No packet capture or impairment helpers | UC1 and UC5 require ad hoc `tcpdump`/`tc` | `lab capture <link>`, `lab impair <link> ...` via webmesh |

## 4. Structure: generic core and NOS-specific parts

### 4.1 Proposed layers

```
  tests / experiments      smoke1..3, user scripts, CI matrix
  ─────────────────────────────────────────────────────────────
  node configuration       L2 profile, L3 profile, host profile   ← NOS-specific
  ─────────────────────────────────────────────────────────────
  topology model           nodes, roles, ports, links (a graph)   ← generic
  ─────────────────────────────────────────────────────────────
  lab lifecycle            generate, up, wait, down, clean        ← mostly generic
  ─────────────────────────────────────────────────────────────
  wiring (webmesh)         veths, eBPF patch panel, flap, stats   ← generic
  ─────────────────────────────────────────────────────────────
  container runtime        Docker / Compose                       ← generic
```

The test matrix (section 5) is built from the same axes: topology model ×
node configuration profiles.

### 4.2 Where SONiC is baked into generic layers today

- **webmesh port naming.** `kvm_port_map.go` maps SONiC logical ports
  (`Ethernet0`, `Ethernet4`, ...) to the kernel interfaces SAI VS expects
  (`eth1`, `eth2`, ...). Other NOSes name interfaces differently. This
  mapping belongs to a node-type definition, not to webmesh; webmesh could
  take the in-namespace interface name directly in `topology.yml`.
- **Readiness.** The `vnet_ready` file handshake and the SONiC entrypoint
  are SONiC-specific; the "wait for wiring, then boot the NOS" pattern is
  generic.
- **tgen's output.** The graph construction in `tgen/internal/topo` is
  generic (aside from ASN and VTEP assignment), but the writer emits SONiC
  compose services, `sonic_entry.sh` and FRR configs directly.
- **Step-4 port numbering** (`Ethernet0`, `Ethernet4`, ...) is a SONiC
  convention that leaks into topology files and link keys.

### 4.3 Node types and profiles

One way to draw the boundary:

- A **node type** says how to run a kind of node: image, how its namespace
  is set up, how logical port names map to kernel interfaces, how to tell
  when it's ready, and how to apply config. Initial types: `sonic-vs`,
  `linux-host`. Later: `frr`, `linux-bridge`, other NOS images.
- A **profile** is a named, documented configuration convention for a node
  type, applied on top of the topology:
  - L2 profiles: `none`, `access-vlans`, `trunk`, `lag`, `lag-trunk`
  - L3 profiles: `none`, `bgp-unnumbered` (today's default),
    `bgp-unnumbered+srv6`, and later others
  - Host profiles: addressing and default routes that match the chosen
    L2/L3 profiles

Profiles are where the conventions (VLAN numbering, address plans, which
ports are host-facing, how BGP rides over a port-channel or SVI) get
written down once, and tests rely on them by name.

### 4.4 Forking

If the project grows past SONiC, the likely split is:

- a **core** (node shells, webmesh, topology model, lab lifecycle, test
  harness, node-type interface), and
- **NOS packs** (`sonic-vs` and others: images, port maps, entrypoints,
  config renderers, profile implementations).

Getting the boundaries in 4.2 right inside this repo first makes a later
fork or split mechanical rather than a rewrite. It's also worth comparing
against existing tools in this space, such as containerlab (multi-NOS
container labs from a topology file) and netlab (config generation for lab
topologies), to decide what to build versus reuse. What this repo has that
those don't center on is webmesh: an API-driven patch panel with runtime
rewiring, flaps and per-link stats.

## 5. Testing strategy

The use cases drive the tests: each suite answers a question a user has,
at a known layer, so a failure points at the layer that broke.

### 5.1 Test matrix axes

| Axis | Values (initially) |
|---|---|
| Topology | spine-leaf, ring, mesh; sizes; parallel links |
| L2 profile (switch-to-switch ports) | none, trunk, lag, lag-trunk |
| L3 profile | none, bgp-unnumbered, bgp-unnumbered+srv6 |
| Hosts | none, single-homed, multi-homed |
| Node type / image | sonic-vs (pinned image), later others |

Not every combination makes sense or is worth running, so each suite
declares which part of the matrix it covers, and results record the full
coordinates.

### 5.2 Readiness and check levels

Each level depends only on the levels below it, so L2 behavior can be
checked without configuring L3, and an L3 test can say which L2 profile it
ran over:

1. **Wired:** webmesh healthy, every link present.
2. **Ports up:** every configured port oper-up in the NOS.
3. **Adjacent:** LLDP on each port sees the expected neighbor and port, as
   given by the topology. Independent of L3, and catches miswiring.
4. **L2 forwarding:** hosts in the same VLAN can reach each other, within a
   switch and across trunks/LAGs; LAG member failover.
5. **L3 converged:** routing sessions up (today's BGP check).
6. **L3 forwarding:** hosts in different subnets can reach each other
   across the fabric; ECMP; failover after a link flap.

### 5.3 Suites

| Suite | Levels | Notes |
|---|---|---|
| `smoke1.sh` (bring-up) | 1-3 always; 5 when an L3 profile is on | Today checks 1 and 5 for the default L3 profile; adding 2-3 makes it valid for L2-only labs |
| `smoke2.sh` (L2) | 4 | Needs L2 profiles and host addressing conventions documented first |
| `smoke3.sh` (L3) | 5-6 | Run across several L2 profiles |
| Resilience (later) | 4-6 under flaps and restarts | Depends on node restart working (UC5) |

### 5.4 Where they run

- **Per PR:** Go unit tests plus a small bring-up matrix (what CI runs
  today), by calling the test scripts.
- **Nightly or on demand:** the larger matrix (sizes, profiles, ECMP,
  SRv6), with results collected as JSON.
- **Locally:** any suite against any topology, or against a lab you
  already have running.

## 6. Possible trajectory

**Near term** (mostly unblocks UC2 and the test strategy):

- Write down the L2 and L3 profile conventions (VLANs, address plans,
  port roles, how BGP rides over L2 constructs).
- Decouple port bring-up from routing in `sonic_entry.sh`; add an L3
  profile switch to tgen with today's behavior as the default.
- Layered checks in `smoke1.sh` (wired, ports up, LLDP adjacency); have CI
  call the script.
- Per-lab name prefix and port, so labs and tests can run side by side.
- Bring the README and design docs in line with the code.

**Medium term:**

- Topology file as tgen input; flags as shorthand for common shapes.
- Host profiles and custom host images (UC1).
- `smoke2.sh` (L2) and `smoke3.sh` (L3) across the matrix.
- A thin lab CLI with JSON output; capture and impairment through webmesh.
- Node restart that the lab survives.

**Longer term:**

- Node-type interface, with `sonic-vs` and `linux-host` as the first
  implementations; move the SONiC port map out of webmesh.
- A second node type to prove the boundary (an FRR-only router or a Linux
  bridge switch is the cheapest).
- Decide whether to split into a core plus NOS packs, or fork.

## 7. Open questions

- What's the primary interface for scripts: shell commands, the webmesh
  REST API, a Go or Python library, or all three in layers?
- Should the topology file also carry node config (profiles per node or per
  port), or should config stay a separate layer applied on top?
- How much of node configuration should tgen render at generation time,
  versus a runtime step (`lab apply-profile`) that can be changed on a
  running lab?
- Which NOS beyond SONiC matters first, and does that change what the core
  needs?
- Is building on or interoperating with containerlab worth it for the
  generic layers, keeping webmesh as the differentiator?
- What's the right home for results: JSON files, a summary in CI, or
  something longer-lived for nightly trends?
