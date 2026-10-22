# Tests

End-to-end tests that generate labs with `tgen`, bring them up, and check
that they work. These complement the Go unit tests in `tgen/` and `webmesh/`,
which don't start any containers.

| Script | What it checks | Status |
|---|---|---|
| `smoke1.sh` | Basic topologies come up: links wired, BGP Established | Available |
| `smoke2.sh` | L2 forwarding between hosts using VLANs | Planned |
| `smoke3.sh` | L3 routed forwarding between hosts | Planned |

## Prerequisites

- Docker running, with Compose v2 (`docker compose`). See the top-level
  README.
- The `docker-sonic-vs:latest` image (see `sonic-patches/README.md`).
- Go, to build `tgen`.
- No other lab running. Lab container names are fixed (`webmesh`,
  `sonic-*`), so the script refuses to start if a `webmesh` container
  exists. Stop the other lab with `docker compose down` in its directory.

## smoke1.sh

Brings up each basic topology in turn and checks that it comes up:

| Topology | tgen args | Switches | BGP sessions per switch |
|---|---|---|---|
| `spine-leaf` | `-s 2 -l 2` | spine1-2, leaf1-2 | 2 |
| `ring` | `-r -n 4` | node1-4 | 2 |
| `mesh` | `-m -n 4` | node1-4 | 3 |

For each topology, the script:

1. Generates the lab with `tgen` into `smoke-<topology>/` at the repo root
   (tgen requires a direct child of the repo root; `smoke-*/` is ignored by
   git).
2. Builds the images and starts the lab, waiting for webmesh to report
   healthy, meaning every link is wired.
3. Waits until every BGP session in each switch's `frr/<switch>.conf` is
   Established.
4. Tears the lab down and removes its directory.

### Running it

```bash
tests/smoke1.sh                 # all three topologies
tests/smoke1.sh ring mesh       # just these
tests/smoke1.sh -k spine-leaf   # leave the lab running if it fails
```

| Variable | Default | Meaning |
|---|---|---|
| `UP_TIMEOUT` | 300 | Seconds to wait for webmesh to report healthy |
| `BGP_TIMEOUT` | 600 | Seconds to wait for BGP sessions to be Established |

### What to expect

A passing run takes about 30-40 seconds per topology once images are built
(the first run also builds the webmesh, debian and host images):

```
[09:39:14] building tgen
[09:39:15] === spine-leaf (-s 2 -l 2)
[09:39:19]   webmesh healthy: all links wired
[09:39:44]   leaf1: 2/2 BGP sessions Established
...
[09:39:44] PASS: spine-leaf in 29s
...
[09:41:50] all passed: spine-leaf ring mesh
```

The exit status is 0 if every topology passed, 1 if any failed, and 2 for
bad arguments.

On failure, the script prints the reason and moves on to the next
topology:

- **Lab didn't come up:** the last lines of the webmesh log, which usually
  name the links it couldn't wire.
- **BGP not Established:** which switch is short (e.g.
  `node1 has 1/2 BGP sessions Established`), followed by its
  `show bgp summary`.

The failed lab is torn down unless you pass `-k`. With `-k` the script
stops at the first failure and leaves that lab running for inspection
(`docker exec -it sonic-<switch> bash`, the webmesh UI at
http://localhost:8092). Stop it afterwards with the command the script
prints.
