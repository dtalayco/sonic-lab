# Shared helpers for the Homa experiments. Source from scripts run as root
# inside the homa VM.

HOMA_DIR=${HOMA_DIR:-/opt/HomaModule}
CP_NODE=$HOMA_DIR/util/cp_node

now_ms() {
	python3 -c 'import time; print(int(time.time() * 1000))'
}

# Print RTT percentiles by message size from cp_node dump_times files. With
# EVENTS set, also append them as an "end" event for the phase.
# Usage: rtt_summary LABEL PROTO GBPS FILE...
rtt_summary() {
	python3 - "$@" <<'PY'
import json, os, sys, time
label, proto, gbps = sys.argv[1:4]
v = []
for f in sys.argv[4:]:
    try:
        for l in open(f):
            if l.startswith('#') or not l.strip():
                continue
            a = l.split()
            v.append((int(a[0]), float(a[1])))
    except FileNotFoundError:
        pass
buckets = {}
for name, lo, hi in (("<=1KB", -1, 1000), ("1-100KB", 1000, 100000), (">100KB", 100000, 1 << 62)):
    x = sorted(r for b, r in v if lo < b <= hi)
    n = len(x)
    if n:
        buckets[name] = {"n": n, "p50_us": x[n // 2], "p99_us": x[int(n * .99)]}
        print(f"  {name:7}: n={n} p50={x[n // 2]:.0f}us p99={x[int(n * .99)]:.0f}us")
if os.environ.get("EVENTS"):
    with open(os.environ["EVENTS"], "a") as f:
        f.write(json.dumps({"t": int(time.time() * 1000), "event": "end", "label": label,
                            "proto": proto, "gbps": float(gbps), "rtt": buckets}) + "\n")
PY
}

# Run a command on node <id>. Defaults to namespace n<id>; override to run
# nodes another way, such as in containers.
node_exec() {
	ip netns exec n$1 "${@:2}"
}

# Run cp_node servers and clients on nodes and report results. With EVENTS
# set, start and end events for the phase are appended to that file.
# Usage: cp_run LABEL PROTO SECS "SERVER_IDS" "CLIENT_IDS" "CLIENT_OPTS"
# A positive --gbps is required in CLIENT_OPTS: --gbps 0 stalls on arm64.
cp_run() {
	local label=$1 proto=$2 secs=$3 servers=$4 clients=$5 opts=$6 i pids=""
	local out=${CP_OUT:-/tmp/cp-out}
	mkdir -p $out
	for i in $servers; do
		( echo "server --protocol $proto"; sleep $((secs + 3)); echo exit ) |
			node_exec $i timeout $((secs + 10)) stdbuf -oL $CP_NODE > $out/s$i.log 2>&1 &
		pids="$pids $!"
	done
	sleep 1
	if [ -n "$EVENTS" ]; then
		printf '{"t":%s,"event":"start","label":"%s","proto":"%s","opts":"%s"}\n' \
			"$(now_ms)" "$label" "$proto" "$opts" >> "$EVENTS"
	fi
	for i in $clients; do
		node_exec $i rm -f /tmp/cp-rtt.$i
		( echo "client --protocol $proto --id $i $opts"; sleep $secs
		  echo "dump_times /tmp/cp-rtt.$i"; sleep 0.5; echo exit ) |
			node_exec $i timeout $((secs + 10)) stdbuf -oL $CP_NODE > $out/c$i.log 2>&1 &
		pids="$pids $!"
	done
	# Only this phase's processes: callers may have their own in the background
	wait $pids

	local gbps=0 rtts="" x
	for i in $clients; do
		node_exec $i cat /tmp/cp-rtt.$i > $out/rtt.$i 2> /dev/null
		x=$(grep ' clients:' $out/c$i.log | tail -1 | grep -oE '[0-9.]+ Gbps out' | cut -d' ' -f1)
		gbps=$(python3 -c "print(round(${gbps:-0} + ${x:-0}, 2))")
		rtts="$rtts $out/rtt.$i"
	done
	echo "=== $label $proto: $gbps Gbps out"
	rtt_summary "$label" $proto $gbps $rtts
	grep -h -i -E 'error|fatal' $out/c*.log $out/s*.log | head -2 | cut -c1-130
}

# Write /etc/netns/<ns>/hosts so cp_node can resolve nodeN. Args: ns, then
# one address per node in node order.
hosts_file() {
	local ns=$1 j=0 a
	shift
	mkdir -p /etc/netns/$ns
	{
		echo "127.0.0.1 localhost"
		for a in "$@"; do echo "$a node$j"; j=$((j + 1)); done
	} > /etc/netns/$ns/hosts
}

homa_resent() {
	awk '$1 == "resent_packets" {s += $2} END {print s + 0}' /proc/net/homa_metrics
}
