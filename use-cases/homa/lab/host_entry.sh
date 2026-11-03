#!/bin/bash
# Wait for webmesh to wire eth1, then address it. Arg: address/prefix.
until ip link show eth1 > /dev/null 2>&1; do sleep 1; done
ip addr replace "$1" dev eth1
# SONiC VS forwards frames in userspace, so checksums left for a veth to
# offload never get filled in and TCP is dropped
ethtool -K eth1 tx off > /dev/null
ip link set eth1 up
echo "eth1 up with $1"
sleep infinity
