#!/usr/bin/env bash
	set -e

	SWITCH="$1"
	VTEP="${2:-}"
	OVERLAY_V4="${3:-}"

	echo "Waiting for vnet file /tmp/vnet_ready/$SWITCH"

	while [ ! -f "/tmp/vnet_ready/$SWITCH" ]; do
	sleep 3
	done

	echo "vnet ready, starting SONiC..."

	# Start supervisord in the background so we can configure
	# the VTEP interface and FRR after SONiC's daemons are up.
	/usr/local/bin/supervisord -n &
	SUPERVISOR_PID=$!

	if [ -n "$VTEP" ]; then
	# Wait for zebra (started by SONiC's start.sh).
	echo "Waiting for zebra to start..."
	for i in $(seq 1 90); do
	if supervisorctl status zebra 2>/dev/null | grep -q "RUNNING"; then
	echo "zebra is running"
	break
	fi
	sleep 2
	done

	# Create Loopback0 (dummy type) with the VTEP address.
	# NOTE: SONiC's ip binary does not support "type loopback";
	# "type dummy" is the correct type for a VTEP interface.
	ip link add Loopback0 type dummy 2>/dev/null || true
	ip addr replace "$VTEP" dev Loopback0
	ip link set Loopback0 up
	echo "Loopback0 created with VTEP address $VTEP"

	# Set admin up in SONiC CONFIG_DB for all Ethernet interfaces.
	# This tells orchagent to program the port; required for the
	# interface to show as "up" in "show interfaces status".
	for iface in $(ip -o link show 2>/dev/null | awk -F': ' '{print $2}' | grep '^Ethernet'); do
	config interface startup "$iface" 2>/dev/null || true
	done
	echo "Set admin up for Ethernet interfaces"

	# Wait for orchagent to process the admin-up requests.
	sleep 5

	# Bring up all Ethernet* interfaces in the kernel BEFORE starting
	# bgpd. Zebra must see them as UP before bgpd connects, otherwise
	# bgpd crashes when reading the config with unnumbered neighbors.
	for iface in $(ip -o link show 2>/dev/null | awk -F': ' '{print $2}' | grep '^Ethernet'); do
	ip link set "$iface" up 2>/dev/null || true
	done
	echo "Brought up Ethernet interfaces"

	# Give zebra time to process the interface state changes via netlink
	# before bgpd connects and requests the interface list.
	sleep 3

	# SONiC's start.sh does not start bgpd — start it explicitly now
	# that all interfaces are up and registered with zebra.
	supervisorctl start bgpd 2>/dev/null || true
	echo "Waiting for bgpd to start..."
	for i in $(seq 1 30); do
	if supervisorctl status bgpd 2>/dev/null | grep -q "RUNNING"; then
	echo "bgpd is running"
	break
	fi
	sleep 2
	done

	# Load FRR config explicitly. Non-fatal: if it fails the container
	# should stay up so we can debug.
	echo "Loading FRR configuration..."
	vtysh -f /tmp/frr.conf || echo "WARNING: vtysh -f returned non-zero"
	echo "FRR configuration loaded"

	# If SRv6 routes file is present, enable seg6 on all interfaces and
	# execute the routes script to program kernel seg6 routes.
	if [ -f /srv6.routes ]; then
	# Enable SRv6 segment routing. The kernel default for seg6_enabled
	# is 0. Setting it on "all"/"default" covers future interfaces but
	# does NOT propagate to already-created ones, so we also set it
	# explicitly on each existing interface below.
	sysctl -w net.ipv6.conf.all.seg6_enabled=1 2>/dev/null || true
	sysctl -w net.ipv6.conf.default.seg6_enabled=1 2>/dev/null || true
	for iface in $(ip -o link show 2>/dev/null | awk -F': ' '{print $2}' | grep -E '^(Ethernet|Loopback)'); do
	echo 1 > "/proc/sys/net/ipv6/conf/$iface/seg6_enabled" 2>/dev/null || true
	done
	echo "Enabled seg6_enabled=1 on all interfaces"

	echo "Loading SRv6 overlay routes..."
	bash /srv6.routes || echo "WARNING: SRv6 routes script returned non-zero"
	echo "SRv6 overlay routes loaded"
	fi

	# Create overlay IPv4 VLAN interface in VRF (SRv6 leaves only).
	# Uses a VLAN interface (Vlan100) because SONiC does not instantiate
	# user-defined loopbacks in the kernel without a physical port behind
	# them. A VLAN interface forces immediate kernel provisioning.
	# The /24 is redistributed into BGP via the VRF's 'redistribute connected'.
	if [ -n "$OVERLAY_V4" ]; then
	config vrf add Vrf100 2>/dev/null || true

	# Clean up any stale loopback config from previous attempts
	config interface vrf unbind Loopback100 2>/dev/null || true
	config loopback del Loopback100 2>/dev/null || true

	# Provision a VLAN interface as the overlay loopback
	config vlan add 100 2>/dev/null || true
	config interface vrf bind Vlan100 Vrf100 2>/dev/null || true
	config interface ip add Vlan100 "$OVERLAY_V4" 2>/dev/null || true
	config interface startup Vlan100 2>/dev/null || true

	echo "Created Vlan100 in Vrf100 with $OVERLAY_V4"
	config save -y 2>/dev/null || true
	fi
	fi

	# Keep the container alive by waiting on supervisord.
	wait $SUPERVISOR_PID
	