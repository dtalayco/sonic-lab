#!/bin/bash
# Stage 1: Homa and TCP between two namespaces over one veth.
# Run as root inside the homa VM after build-homa.sh.
. "$(dirname "$0")/lib.sh"

for n in n0 n1 n2 n3; do ip netns del $n 2>/dev/null; done
ip link del brh 2>/dev/null
ip netns add n0
ip netns add n1
ip link add v0 netns n0 type veth peer name v1 netns n1
ip -n n0 addr add 10.70.0.1/24 dev v0
ip -n n1 addr add 10.70.0.2/24 dev v1
for x in "n0 v0" "n1 v1"; do
	set -- $x
	ip -n $1 link set lo up
	ip -n $1 link set $2 mtu 9000 up
	hosts_file $1 10.70.0.1 10.70.0.2
done
ip netns exec n0 ping -c 1 -q -W 2 10.70.0.2 | grep received

PAIR="--first-server 1 --server-nodes 1 --gbps 100 --client-max"
for size in 100 500000; do
	for p in homa tcp; do cp_run "size=$size" $p 5 "1" "0" "--workload $size $PAIR 1"; done
done
for p in homa tcp; do cp_run "size=500000 x8" $p 5 "1" "0" "--workload 500000 $PAIR 8"; done
echo "resent_packets total $(homa_resent)"
