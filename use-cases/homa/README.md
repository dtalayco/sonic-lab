# Use case: Homa

Run [Homa](https://homa-transport.atlassian.net/wiki/spaces/HOMA/overview)
on simulated Linux hosts connected by a switch, with a workload that
exercises it, and compare it against TCP.

The goal is to find out what sonic-lab needs, and where the simulation
approach falls short, rather than to measure Homa accurately. Published
results show what a real network should give, so differences from them
point at limits of the simulation.

Realistic network behavior (link rates, queues, buffers) is a possible
future *simulation mode*. Many use cases don't need it, and it would get
in the way of some, so it shouldn't be the default.

## Requirements

Homa is a kernel module, and every container shares the Docker VM's
kernel. So the module is loaded once in the VM, every simulated host uses
that one instance, and the VM needs a kernel Homa builds against. Homa
`main` does not build on the 6.8 kernel Colima ships; it does on Ubuntu's
6.17. To keep that away from other labs, these experiments use their own
Colima profile, `homa`.

The Homa tools (`cp_node`, `homa_prio`) assume x86, so `build-homa.sh`
patches two spots for arm64.

## Running

From the Mac, once:

```sh
use-cases/homa/setup-vm.sh
```

This leaves the docker CLI pointing at the `homa` VM. Use
`docker context use colima` to switch back to the default profile.

Then, inside the VM (it sees the repo at the same path):

```sh
D=$PWD/use-cases/homa
colima ssh -p homa -- sudo $D/build-homa.sh   # Also after every VM restart
colima ssh -p homa -- sudo $D/veth.sh
colima ssh -p homa -- sudo $D/bridge.sh
colima ssh -p homa -- sudo $D/bottleneck.sh
```

The SONiC lab runs from the Mac, with the docker CLI pointing at the
`homa` VM and Homa loaded:

```sh
cd use-cases/homa/lab
docker compose up -d --wait   # SONiC configures its VLAN a minute or two later
./run.sh
docker compose down
```

Restart the whole lab rather than single containers: webmesh doesn't
rewire a link when only one end restarts.

## Files

| File | What it does |
|---|---|
| `setup-vm.sh` | Creates the `homa` Colima profile and installs the 6.17 kernel |
| `build-homa.sh` | Builds Homa at a pinned commit, patches the tools for arm64, loads the module |
| `homa-arm64.patch` | The arm64 fixes for the tools, applied by `build-homa.sh` |
| `lib.sh` | Shared helpers: runs `cp_node` in namespaces and summarizes RTTs |
| `veth.sh` | Stage 1: two namespaces over one veth |
| `bridge.sh` | Stage 2: four namespaces on a Linux bridge, no link model |
| `bottleneck.sh` | Stage 2 with 2 Gbps links, comparing a FIFO switch queue with 8 DSCP priority bands |
| `lab/` | Stage 3: hand-written lab with one SONiC switch and four hosts on VLAN 10; `run.sh` runs the stage 2 tests across it |

The workload is Homa's own `cp_node`, which runs the same traffic over
Homa or TCP (`--protocol`). `w4` is a message size distribution from the
Homa papers.

## Results so far

Single runs of 5-10 s; differences under about 2x are noise.

- **No link model:** Homa's short-message latency is about 4x better than
  TCP's (6 vs 25 us p50), but under load the run is limited by CPU, not
  the network, and TCP matches or beats Homa. Incast shows no congestion
  at all.
- **2 Gbps links:** under 90% incast, short Homa messages are 20-40x
  faster than TCP at p50 and about 100x at p99, and 8 priority bands
  roughly halve Homa's short-message latency compared with a FIFO. This
  is the behavior published results lead us to expect.
- **SONiC switch, no link model:** Homa and TCP both work across SONiC
  VS once host checksum offload is off, but the switch is the bottleneck:
  under 1 Gbps in total, with the switch container at up to 350% CPU.
  Round trips for small messages take 105-130 us, against 6-25 us over a
  bridge. Homa does worse than TCP here (0.28 vs 0.93 Gbps for 500 KB
  messages), and RPCs time out in the all-to-all run.

## Limitations found

- **Shared kernel:** all hosts share one Homa module, and `net.homa.*`
  settings exist only in the VM's root namespace, so they are machine-wide.
- **No link model:** without rate limits, results reflect CPU scheduling.
  The bottleneck here is Linux `tc` on both ends of each link, which works
  the same with any switch.
- **Segmentation offload:** Homa sends about 7 KB packets over veth, while
  TCP gets 64 KB segments. Raising `net.homa.max_gso_size` to 100000 gave
  zero throughput.
- **Topology generation:** tgen can't produce a single switch, and hosts
  have no type, image or addresses. Switch config is routing only; L2
  needed a custom `sonic_entry.sh`.
- **SONiC VS data path:** frames pass through userspace, which limits
  throughput to about 1 Gbps on this machine and adds about 100 us per
  round trip. A modeled link rate would have to stay well below that, or
  use a kernel-forwarding switch (bridge, OVS) for performance work.
- **Checksum offload:** hosts must turn off TX checksum offload
  (`ethtool -K eth1 tx off`), or TCP through SONiC VS is dropped. Ping
  and Homa work without it, so it's easy to miss. Likely affects every
  sonic-lab topology with hosts.
- **webmesh recovery:** restarting a host container leaves its link
  unwired; webmesh finds the switch-side half and never recreates the
  pair.
- **Second path out of hosts:** hosts are also on the compose network,
  with their default route there, so traffic that misses the lab link
  leaves through Docker instead of failing. A ping to a host address
  was answered this way while the lab link was down.
- **Metrics location:** `/proc/net/homa_metrics` exists only in the VM's
  root namespace, not inside the host containers.
- **Unexplained:** resent packets grow with no queue drops, and Homa
  delivers less than offered load in the all-to-all runs.
- **`cp_node --gbps 0` stalls** after one RPC on arm64; always pass a
  positive rate.
