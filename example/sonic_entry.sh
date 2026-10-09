#!/usr/bin/env bash
set -e

SWITCH="$1"
VTEP="${2:-}"

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

    # SONiC's start.sh does not start bgpd — start it explicitly.
    supervisorctl start bgpd 2>/dev/null || true
    echo "Waiting for bgpd to start..."
    for i in $(seq 1 30); do
        if supervisorctl status bgpd 2>/dev/null | grep -q "RUNNING"; then
            echo "bgpd is running"
            break
        fi
        sleep 2
    done

    # Create Loopback0 (dummy type) with the VTEP address.
    # NOTE: SONiC's ip binary does not support "type loopback";
    # "type dummy" is the correct type for a VTEP interface.
    ip link add Loopback0 type dummy
    ip addr replace "$VTEP" dev Loopback0
    ip link set Loopback0 up
    echo "Loopback0 created with VTEP address $VTEP"

    # Bring up all eth* interfaces to ensure they have link-local
    # IPv6 addresses. FRR's bgpd will crash if a BGP unnumbered
    # neighbor references an interface that doesn't exist or is down.
    for iface in $(ip -o link show 2>/dev/null | awk -F': ' '{print $2}' | grep '^eth'); do
        ip link set "$iface" up 2>/dev/null || true
    done
    echo "Brought up eth interfaces"

    # Load FRR config explicitly. Non-fatal: if it fails the container
    # should stay up so we can debug.
    echo "Loading FRR configuration..."
    vtysh -f /etc/frr/frr.conf || echo "WARNING: vtysh -f returned non-zero"
    echo "FRR configuration loaded"
fi

# Keep the container alive by waiting on supervisord.
wait $SUPERVISOR_PID
