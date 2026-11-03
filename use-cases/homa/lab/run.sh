#!/bin/bash
# Stage 3: the stage 2 tests across the SONiC lab, with node N as container
# host<N+1>. Run on the Mac with the lab up and the docker CLI pointing at
# the homa VM.
#
# -r NAME  record samples and phases to ../runs/<time>-NAME and build
#          report.html there
HERE=$(cd "$(dirname "$0")" && pwd)
. "$HERE/../lib.sh"

while getopts r: opt; do
	case $opt in
	r) NAME=$OPTARG ;;
	*) echo "usage: $0 [-r NAME]" >&2; exit 2 ;;
	esac
done

CP_NODE=/homa/cp_node
CP_OUT=$(mktemp -d)
node_exec() {
	docker exec -i host$(($1 + 1)) "${@:2}"
}

if [ -n "$NAME" ]; then
	RUN=$HERE/../runs/$(date +%Y%m%d-%H%M%S)-$NAME
	mkdir -p "$RUN"
	export EVENTS=$RUN/events.jsonl
	colima ssh -p homa -- sudo python3 "$HERE/../collect.py" -o "$RUN/samples.jsonl" --homa \
		--label name="$NAME" --label switch=sonic-vs --label lab=homa-1x4 &
	trap 'colima ssh -p homa -- sudo pkill -f "collect.py -o $RUN"' EXIT
	sleep 3
fi

PAIR="--first-server 1 --server-nodes 1 --gbps 100 --client-max"
for size in 100 500000; do
	for p in homa tcp; do cp_run "pair size=$size" $p 5 "1" "0" "--workload $size $PAIR 1"; done
done

INCAST="--workload 500000 --gbps 100 --client-max 8 --first-server 3 --server-nodes 1"
A2A="--workload w4 --gbps 2 --client-max 200 --first-server 0 --server-nodes 4"
for p in homa tcp; do cp_run "incast 3->1 500KB" $p 6 "3" "0 1 2" "$INCAST"; done
for p in homa tcp; do cp_run "all-to-all w4" $p 8 "0 1 2 3" "0 1 2 3" "$A2A"; done
# Homa's metrics exist only in the VM's root namespace, not in the hosts
echo "resent_packets total $(colima ssh -p homa -- sudo awk '$1 == "resent_packets" {s += $2} END {print s + 0}' /proc/net/homa_metrics)"

if [ -n "$NAME" ]; then
	sleep 3
	colima ssh -p homa -- sudo pkill -f "collect.py -o $RUN"
	trap - EXIT
	wait
	python3 "$HERE/../report.py" "$RUN"
fi
