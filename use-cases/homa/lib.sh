# Shared helpers for the Homa experiments. Source from scripts run as root
# inside the homa VM.

HOMA_DIR=${HOMA_DIR:-/opt/HomaModule}
CP_NODE=$HOMA_DIR/util/cp_node

# Print RTT percentiles by message size from cp_node dump_times files.
rtt_summary() {
	python3 - "$@" <<'PY'
import sys
v = []
for f in sys.argv[1:]:
    for l in open(f):
        if l.startswith('#') or not l.strip():
            continue
        a = l.split()
        v.append((int(a[0]), float(a[1])))
def show(x, label):
    x = sorted(x)
    n = len(x)
    if n:
        print(f"  {label}: n={n} p50={x[n//2]:.0f}us p99={x[int(n*.99)]:.0f}us")
show([r for b, r in v if b <= 1000], "<=1KB  ")
show([r for b, r in v if 1000 < b <= 100000], "1-100KB")
show([r for b, r in v if b > 100000], ">100KB ")
PY
}

# Run a command on node <id>. Defaults to namespace n<id>; override to run
# nodes another way, such as in containers.
node_exec() {
	ip netns exec n$1 "${@:2}"
}

# Run cp_node servers and clients on nodes and report results.
# Usage: cp_run LABEL PROTO SECS "SERVER_IDS" "CLIENT_IDS" "CLIENT_OPTS"
# A positive --gbps is required in CLIENT_OPTS: --gbps 0 stalls on arm64.
cp_run() {
	local label=$1 proto=$2 secs=$3 servers=$4 clients=$5 opts=$6 i
	local out=${CP_OUT:-/tmp/cp-out}
	mkdir -p $out
	for i in $servers; do
		( echo "server --protocol $proto"; sleep $((secs + 3)); echo exit ) |
			node_exec $i timeout $((secs + 10)) stdbuf -oL $CP_NODE > $out/s$i.log 2>&1 &
	done
	sleep 1
	for i in $clients; do
		node_exec $i rm -f /tmp/cp-rtt.$i
		( echo "client --protocol $proto --id $i $opts"; sleep $secs
		  echo "dump_times /tmp/cp-rtt.$i"; sleep 0.5; echo exit ) |
			node_exec $i timeout $((secs + 10)) stdbuf -oL $CP_NODE > $out/c$i.log 2>&1 &
	done
	wait

	local gbps=0 rtts="" x
	for i in $clients; do
		node_exec $i cat /tmp/cp-rtt.$i > $out/rtt.$i 2> /dev/null
		x=$(grep ' clients:' $out/c$i.log | tail -1 | grep -oE '[0-9.]+ Gbps out' | cut -d' ' -f1)
		gbps=$(python3 -c "print(round(${gbps:-0} + ${x:-0}, 2))")
		rtts="$rtts $out/rtt.$i"
	done
	echo "=== $label $proto: $gbps Gbps out"
	rtt_summary $rtts
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
