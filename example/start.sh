#!/usr/bin/env bash
	set -e

	echo "Building images..."
	docker compose build

	echo "Starting topology..."
	docker compose up -d

	echo
	echo "Running containers:"
	docker ps --filter "name=sonic-"
	echo
	echo "Webmesh UI: http://localhost:8092"
	