#!/usr/bin/env bash
	set -e
	echo "Initializing with arguments $@"
	touch "/tmp/vnet_ready/$1"
	echo "vnet ready"
	sleep infinity
	