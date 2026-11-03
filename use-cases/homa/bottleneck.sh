#!/bin/bash
# Bottleneck model on the stage 2 bridge: every link rate-limited in both
# directions, with the switch egress either one FIFO or eight DSCP bands.
# Run as root inside the homa VM after build-homa.sh.
. "$(dirname "$0")/lib.sh"

RATE=${RATE:-2gbit}
QUEUE=${QUEUE:-2mb}

ip link show brh > /dev/null 2>&1 || "$(dirname "$0")/bridge.sh" setup
sysctl -q -w net.homa.link_mbps=$((${RATE%gbit} * 1000))
trap 'clear_queues; sysctl -q -w net.homa.link_mbps=25000' EXIT

clear_queues() {
	for i in 0 1 2 3; do
		tc qdisc del dev p$i root 2> /dev/null
		ip netns exec n$i tc qdisc del dev h$i root 2> /dev/null
	done
}

# Rate-limited egress with a bounded queue. Args: optional "ip netns exec
# nX" prefix, then the device.
shaped() {
	local dev=${@: -1}
	"${@:1:$#-1}" tc qdisc add dev $dev root handle 1: htb default 1
	"${@:1:$#-1}" tc class add dev $dev parent 1: classid 1:1 htb rate $RATE ceil $RATE quantum 9100
}

# Host uplinks stand in for the NIC; they are the same in both configs.
uplinks() {
	for i in 0 1 2 3; do
		shaped ip netns exec n$i h$i
		ip netns exec n$i tc qdisc add dev h$i parent 1:1 handle 10: bfifo limit $QUEUE
	done
}

fifo() {
	clear_queues
	uplinks
	for i in 0 1 2 3; do
		shaped p$i
		tc qdisc add dev p$i parent 1:1 handle 10: bfifo limit $QUEUE
	done
}

# Eight bands keyed on the top 3 bits of the TOS byte, where Homa puts its
# priority; priority 7 goes to band 1. Other traffic lands in band 8.
prio8() {
	clear_queues
	uplinks
	for i in 0 1 2 3; do
		shaped p$i
		tc qdisc add dev p$i parent 1:1 handle 10: prio bands 8 priomap 7 7 7 7 7 7 7 7 7 7 7 7 7 7 7 7
		for b in 1 2 3 4 5 6 7 8; do
			tc qdisc add dev p$i parent 10:$b handle $((20 + b)): bfifo limit $QUEUE
		done
		for pr in 0 1 2 3 4 5 6 7; do
			tc filter add dev p$i parent 10: protocol ip prio 1 u32 \
				match ip tos $((pr << 5)) 0xe0 flowid 10:$((8 - pr))
		done
	done
}

drops() {
	local up=0 i d
	for i in 0 1 2 3; do
		d=$(ip netns exec n$i tc -s qdisc show dev h$i | awk '/bfifo/ {getline; for (i = 1; i <= NF; i++) if ($i == "dropped") s += $(i + 1)} END {print s + 0}')
		up=$((up + d))
	done
	echo "  drops: switch(p3)=$(tc -s qdisc show dev p3 | awk '/bfifo/ {getline; for (i = 1; i <= NF; i++) if ($i == "dropped") s += $(i + 1)} END {print s + 0}') uplinks=$up"
}

bands() {
	echo "  bands at p3, bytes sent 1..8: $(tc -s qdisc show dev p3 | awk '/qdisc bfifo/ {getline; printf "%s ", $2}')"
}

INCAST="--workload w4 --gbps 0.6 --client-max 200 --first-server 3 --server-nodes 1"
A2A="--workload w4 --gbps 1.4 --client-max 200 --first-server 0 --server-nodes 4"
for cfg in fifo prio8; do
	echo "##### $cfg, $RATE links, $QUEUE queues"
	for p in homa tcp; do
		$cfg 2> /dev/null
		cp_run "incast 90%" $p 10 "3" "0 1 2" "$INCAST"
		drops
		[ $cfg = prio8 ] && bands
		$cfg 2> /dev/null
		cp_run "all-to-all 70%" $p 10 "0 1 2 3" "0 1 2 3" "$A2A"
		drops
	done
done
echo "resent_packets total $(homa_resent)"
