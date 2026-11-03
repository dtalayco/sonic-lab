#!/usr/bin/env bash
# L2-only switch: the host ports are untagged members of VLAN 10, with no
# routing. A hack until the topology can describe switch config.
SWITCH="$1"
VLAN=10
PORTS="Ethernet0 Ethernet4 Ethernet8 Ethernet12"

touch /usr/bin/docker
chmod +x /usr/bin/docker

while [ ! -f "/tmp/vnet_ready/$SWITCH" ]; do sleep 3; done

/usr/local/bin/supervisord -n &
SUPERVISOR_PID=$!

echo "Waiting for orchagent..."
for i in $(seq 1 90); do
	supervisorctl status orchagent 2> /dev/null | grep -q RUNNING && break
	sleep 2
done
sleep 10

config vlan add $VLAN
for p in $PORTS; do
	config vlan member add -u $VLAN $p
	config interface startup $p
done
echo "VLAN $VLAN configured on $PORTS"

wait $SUPERVISOR_PID
