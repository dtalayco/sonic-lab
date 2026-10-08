#!/usr/bin/env bash
set -e

COMPOSE="docker compose -f example/docker-compose.yml"

OPTSTRING=":l"

while getopts ":l" opt; do
    case "$opt" in
        l)
            echo "Loading docker-sonic-vs image..."
            wget -P /tmp -o /dev/null "https://artprodcus3.artifacts.visualstudio.com/Af91412a5-a906-4990-9d7c-f697b81fc04d/be1b070f-be15-4154-aade-b1d3bfb17054/_apis/artifact/cGlwZWxpbmVhcnRpZmFjdDovL21zc29uaWMvcHJvamVjdElkL2JlMWIwNzBmLWJlMTUtNDE1NC1hYWRlLWIxZDNiZmIxNzA1NC9idWlsZElkLzExOTc5NTIvYXJ0aWZhY3ROYW1lL3NvbmljLWJ1aWxkaW1hZ2UudnM1/content?format=file&subpath=/target/docker-sonic-vs.gz"
            docker load < /tmp/docker-sonic-vs.gz
            ;;
        ?)
            echo "Invalid option: -${OPTARG}."
            exit 1
            ;;
    esac
done

echo "Stopping existing topology..."
$COMPOSE down -v || true

echo "Building images..."
$COMPOSE build

echo "Starting webmesh patch panel..."
$COMPOSE up -d webmesh

echo "Starting backing Linux containers..."
$COMPOSE up -d spine1-linux leaf1-linux leaf2-linux

echo "Waiting for webmesh to wire all links..."
for i in $(seq 1 60); do
    status=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}starting{{end}}' webmesh 2>/dev/null || echo "starting")
    if [ "$status" = "healthy" ]; then
        echo "webmesh is healthy — all links wired"
        break
    fi
    if [ "$i" -eq 60 ]; then
        echo "Timed out waiting for webmesh"
        docker logs webmesh
        exit 1
    fi
    sleep 2
done

echo "Starting SONiC containers..."
$COMPOSE up -d

echo
echo "Running containers:"
docker ps --filter "name=sonic-"
echo
echo "Webmesh UI: http://localhost:8092"