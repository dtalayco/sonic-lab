#!/bin/bash
# Stage 2: four namespaces n0-n3 on a Linux bridge, with no link model.
# Run as root inside the homa VM after build-homa.sh. Leaves the
# namespaces up for bottleneck.sh.
. "$(dirname "$0")/lib.sh"

for n in n0 n1 n2 n3; do ip netns del $n 2>/dev/null; done
ip link del brh 2>/dev/null
ip link add brh type bridge
ip link set brh mtu 9000 up
for i in 0 1 2 3; do
	ip netns add n$i
	hosts_file n$i 10.70.1.1 10.70.1.2 10.70.1.3 10.70.1.4
	ip link add h$i netns n$i type veth peer name p$i
	ip link set p$i master brh mtu 9000 up
	ip -n n$i addr add 10.70.1.$((i + 1))/24 dev h$i
	ip -n n$i link set lo up
	ip -n n$i link set h$i mtu 9000 up
done
ip netns exec n0 ping -c 1 -q -W 2 10.70.1.4 | grep received

[ "$1" = setup ] && exit 0

INCAST="--workload 500000 --gbps 100 --client-max 8 --first-server 3 --server-nodes 1"
A2A="--workload w4 --gbps 2 --client-max 200 --first-server 0 --server-nodes 4"
for p in homa tcp; do cp_run "incast 3->1 500KB" $p 6 "3" "0 1 2" "$INCAST"; done
for p in homa tcp; do cp_run "all-to-all w4" $p 8 "0 1 2 3" "0 1 2 3" "$A2A"; done
echo "resent_packets total $(homa_resent)"
