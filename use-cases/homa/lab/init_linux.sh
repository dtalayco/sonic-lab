#!/usr/bin/env bash
# Backing container for the switch: holds its network namespace.
set -e
touch "/tmp/vnet_ready/$1"
sleep infinity
