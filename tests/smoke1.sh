#!/usr/bin/env bash
# smoke1.sh — bring up each basic topology and check that it comes up.
#
# For each topology: generate it with tgen, build and start it, wait for
# webmesh to report healthy (every link wired), then wait for every BGP
# session in each switch's FRR config to reach Established.
#
# Labs use fixed container names (webmesh, sonic-*), so topologies run one
# at a time and no other lab may be running.
#
# Usage: tests/smoke1.sh [-k] [topology ...]
#        tests/smoke1.sh -c
#   -k          on failure, leave the failed lab running for debugging
#   -c          clean up: stop any smoke labs, remove their directories and
#               the sonic-lab/* images, then exit
#   topology    spine-leaf, ring, mesh (default: all three)
#
# Can be run from any directory.
#
# Environment:
#   UP_TIMEOUT   seconds to wait for webmesh healthy (default 300)
#   BGP_TIMEOUT  seconds to wait for BGP Established (default 600)
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
UP_TIMEOUT=${UP_TIMEOUT:-300}
BGP_TIMEOUT=${BGP_TIMEOUT:-600}
ALL_TOPOLOGIES="spine-leaf ring mesh"

topo_args() {
	case "$1" in
	spine-leaf) echo "-s 2 -l 2" ;;
	ring) echo "-r -n 4" ;;
	mesh) echo "-m -n 4" ;;
	*) return 1 ;;
	esac
}

log() { echo "[$(date +%H:%M:%S)] $*"; }

# clean_all removes everything a run (or an interrupted run) can leave
# behind. Images still used by another lab's containers are kept.
clean_all() {
	local p img
	for p in $(docker ps -a --format '{{.Label "com.docker.compose.project"}}' | grep '^smoke-' | sort -u); do
		log "stopping lab $p"
		docker compose -p "$p" down -v --remove-orphans >/dev/null 2>&1 || true
	done
	rm -rf "$ROOT"/smoke-*/
	for img in sonic-lab/webmesh:latest sonic-lab/debian:latest sonic-lab/host:latest; do
		docker image inspect "$img" >/dev/null 2>&1 || continue
		if docker image rm "$img" >/dev/null 2>&1; then
			log "removed image $img"
		else
			log "kept image $img (in use by another lab's containers)"
		fi
	done
	log "clean"
}

KEEP=0
CLEAN=0
while getopts kc opt; do
	case "$opt" in
	k) KEEP=1 ;;
	c) CLEAN=1 ;;
	*) sed -n '2,22p' "$0"; exit 2 ;;
	esac
done
shift $((OPTIND - 1))
TOPOLOGIES=${*:-$ALL_TOPOLOGIES}
for t in $TOPOLOGIES; do
	topo_args "$t" >/dev/null || { echo "unknown topology: $t (want: $ALL_TOPOLOGIES)"; exit 2; }
done

# --- Preflight ---
docker info >/dev/null 2>&1 || { echo "docker is not running (try: colima start)"; exit 1; }
if [ "$CLEAN" = 1 ]; then
	clean_all
	exit 0
fi
docker compose version >/dev/null 2>&1 || { echo "'docker compose' (v2) is required"; exit 1; }
docker image inspect docker-sonic-vs:latest >/dev/null 2>&1 ||
	{ echo "image docker-sonic-vs:latest not found; see sonic-patches/README.md"; exit 1; }
if docker ps -a --format '{{.Names}}' | grep -qx webmesh; then
	echo "another lab is running (container 'webmesh' exists); stop it first with"
	echo "'docker compose down' in its directory"
	exit 1
fi

WORK=$(mktemp -d)
LAB=""
cleanup() {
	if [ -n "$LAB" ]; then
		if [ "$KEEP" = 1 ]; then
			log "leaving $LAB running (-k); clean up with: $0 -c"
		else
			docker compose -f "$LAB/docker-compose.yml" down -v --remove-orphans >/dev/null 2>&1 || true
			rm -rf "$LAB"
		fi
	fi
	rm -rf "$WORK"
}
trap cleanup EXIT

log "building tgen"
(cd "$ROOT/tgen" && go build -o "$WORK/tgen" .)

# wait_bgp <lab>: wait until each switch has all its configured BGP
# neighbors Established.
wait_bgp() {
	local lab=$1 conf sw want got deadline=$((SECONDS + BGP_TIMEOUT))
	for conf in "$lab"/frr/*.conf; do
		sw=$(basename "$conf" .conf)
		want=$(grep -c '^ neighbor .* interface' "$conf" || true)
		while :; do
			got=$(docker exec "sonic-$sw" vtysh -c 'show bgp neighbors json' 2>/dev/null |
				grep -o '"bgpState":"Established"' | wc -l | tr -d ' ')
			[ "$got" -ge "$want" ] && break
			if [ $SECONDS -ge $deadline ]; then
				log "FAIL: $sw has $got/$want BGP sessions Established"
				docker exec "sonic-$sw" vtysh -c 'show bgp summary' 2>&1 | sed 's/^/    /' || true
				return 1
			fi
			sleep 5
		done
		log "  $sw: $got/$want BGP sessions Established"
	done
}

# run_topology <name>: bring up one topology, check it, and tear it down.
run_topology() {
	local name=$1 compose start=$SECONDS
	LAB="$ROOT/smoke-$name"
	compose="docker compose -f $LAB/docker-compose.yml"

	log "=== $name ($(topo_args "$name"))"
	rm -rf "$LAB"
	# shellcheck disable=SC2046
	"$WORK/tgen" $(topo_args "$name") -o "$LAB" >/dev/null || return 1
	if ! $compose build >"$WORK/build.log" 2>&1; then
		log "FAIL: build failed:"
		tail -20 "$WORK/build.log" | sed 's/^/    /'
		return 1
	fi
	if ! $compose up -d --wait --wait-timeout "$UP_TIMEOUT" >/dev/null 2>&1; then
		log "FAIL: lab did not come up; webmesh says:"
		docker logs --tail 20 webmesh 2>&1 | sed 's/^/    /'
		return 1
	fi
	log "  webmesh healthy: all links wired"
	wait_bgp "$LAB" || return 1
	log "PASS: $name in $((SECONDS - start))s"

	$compose down -v --remove-orphans >/dev/null 2>&1
	rm -rf "$LAB"
	LAB=""
}

failed=""
for t in $TOPOLOGIES; do
	if ! run_topology "$t"; then
		failed="$failed $t"
		# The next topology can't start alongside a kept lab.
		[ "$KEEP" = 1 ] && break
		docker compose -f "$LAB/docker-compose.yml" down -v --remove-orphans >/dev/null 2>&1 || true
		rm -rf "$LAB"
		LAB=""
	fi
done

if [ -n "$failed" ]; then
	log "FAILED:$failed"
	exit 1
fi
log "all passed: $TOPOLOGIES"
