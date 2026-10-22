# Tests

End-to-end tests that generate labs with `tgen`, bring them up, and check
that they work. These complement the Go unit tests in `tgen/` and `webmesh/`,
which don't start any containers.

| Script | What it checks | Status |
|---|---|---|
| `smoke1.sh` | Basic topologies come up: links wired, BGP Established | Available |
| `smoke2.sh` | L2 forwarding between hosts using VLANs | Planned |
| `smoke3.sh` | L3 routed forwarding between hosts | Planned |

## Requirements

**Where to run from:** anywhere. The script finds the repo root from its own
location, so `tests/smoke1.sh` from the repo root and
`~/work/sonic-lab/tests/smoke1.sh` from elsewhere behave the same.

**On the host:**

- Docker running (e.g. `colima start`), with Compose v2 (`docker compose`,
  not `docker-compose`). See the top-level README.
- Go, to build `tgen`.
- The `docker-sonic-vs:latest` image, built or pulled per
  `sonic-patches/README.md`. The script doesn't build it.

**No other lab running.** Lab container names are fixed (`webmesh`,
`sonic-*`), so the script refuses to start if a `webmesh` container exists.
Stop the other lab first with `docker compose down` in its directory.

## What the tests build and use

The tests always use the code in your current checkout:

| Tool | How it's used | Built by the test? |
|---|---|---|
| `tgen` | Generates each lab (compose file, `topology.yml`, FRR configs) | Yes. Built from `tgen/` into a temporary directory on every run, so it never uses a stale `tgen/tgen` binary |
| webmesh | Wires the links between switches; its health check gates the lab | Yes. `docker compose build` rebuilds the `sonic-lab/webmesh:latest` image from `webmesh/` when the source has changed |
| `sonic-lab/debian:latest` | Backing netns container for each switch | Yes, the same way (`Dockerfile.debian`) |
| `docker-sonic-vs:latest` | The SONiC switches | No. Must already exist |
| `webmeshctl` | Not used | — |

The `sonic-lab/*` images are shared with every other lab on the machine.
Running the tests rebuilds them from your checkout, so other labs started
afterwards pick up the same webmesh.

## smoke1.sh

Brings up each basic topology in turn and checks that it comes up:

| Topology | tgen args | Switches | BGP sessions per switch |
|---|---|---|---|
| `spine-leaf` | `-s 2 -l 2` | spine1-2, leaf1-2 | 2 |
| `ring` | `-r -n 4` | node1-4 | 2 |
| `mesh` | `-m -n 4` | node1-4 | 3 |

For each topology, the script:

1. Generates the lab with `tgen` into `smoke-<topology>/` at the repo root.
   tgen requires a direct child of the repo root; `smoke-*/` is ignored by
   git.
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
tests/smoke1.sh -c              # clean up everything (see below)
```

| Variable | Default | Meaning |
|---|---|---|
| `UP_TIMEOUT` | 300 | Seconds to wait for webmesh to report healthy |
| `BGP_TIMEOUT` | 600 | Seconds to wait for BGP sessions to be Established |

### What to expect

A passing run takes about 30-40 seconds per topology, under 3 minutes for
all three. Rebuilding the images after `-c` adds only seconds, because
Docker's build cache survives; building them on a fresh machine takes a few
minutes. Build output goes to a log and is shown only if the build fails.

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

- **Build failed:** the last lines of the build output.
- **Lab didn't come up:** the last lines of the webmesh log, which usually
  name the links it couldn't wire.
- **BGP not Established:** which switch is short (e.g.
  `node1 has 1/2 BGP sessions Established`), followed by its
  `show bgp summary`.

The failed lab is torn down unless you pass `-k`. With `-k` the script
stops at the first failure and leaves that lab running for inspection
(`docker exec -it sonic-<switch> bash`, the webmesh UI at
http://localhost:8092).

## Cleaning up

Normally there's nothing to clean: each lab is torn down when its test
finishes, including after a failure or Ctrl-C. To reset everything, for
example after `-k`, a killed run, or when you suspect a stale image, run:

```bash
tests/smoke1.sh -c
```

This:

- stops and removes any smoke lab's containers and volumes (compose
  projects named `smoke-*`), even if its directory is gone;
- deletes the `smoke-*/` directories at the repo root;
- removes the `sonic-lab/webmesh`, `sonic-lab/debian` and `sonic-lab/host`
  images, so the next run rebuilds them from source. An image still used by
  another lab's containers is kept, and the script says so.

Docker's build cache is kept, so the rebuild reuses unchanged layers such
as the `apt-get` installs. To force a rebuild from scratch, also run
`docker builder prune`.

It never removes `docker-sonic-vs:latest` or touches labs other than the
smoke labs. The temporary `tgen` binary needs no cleanup: it's rebuilt
on every run and deleted on exit.
