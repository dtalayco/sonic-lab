#!/bin/bash
# Stage 3: the stage 2 tests across the SONiC lab, with node N as container
# host<N+1>. Run on the Mac with the lab up and the docker CLI pointing at
# the homa VM.
. "$(dirname "$0")/../lib.sh"

CP_NODE=/homa/cp_node
CP_OUT=$(mktemp -d)
node_exec() {
	docker exec -i host$(($1 + 1)) "${@:2}"
}

PAIR="--first-server 1 --server-nodes 1 --gbps 100 --client-max"
for size in 100 500000; do
	for p in homa tcp; do cp_run "pair size=$size" $p 5 "1" "0" "--workload $size $PAIR 1"; done
done

INCAST="--workload 500000 --gbps 100 --client-max 8 --first-server 3 --server-nodes 1"
A2A="--workload w4 --gbps 2 --client-max 200 --first-server 0 --server-nodes 4"
for p in homa tcp; do cp_run "incast 3->1 500KB" $p 6 "3" "0 1 2" "$INCAST"; done
for p in homa tcp; do cp_run "all-to-all w4" $p 8 "0 1 2 3" "0 1 2 3" "$A2A"; done
# Homa's metrics exist only in the VM's root namespace, not in the hosts
echo "resent_packets total $(colima ssh -p homa -- awk '$1 == "resent_packets" {s += $2} END {print s + 0}' /proc/net/homa_metrics)"
