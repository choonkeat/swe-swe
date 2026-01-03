#!/bin/bash
set -euox pipefail

# Run the test container

CONTAINER_NAME="swe-swe-test"
HOST_PORT="${HOST_PORT:?HOST_PORT environment variable is required}"
CONTAINER_PORT=9898

# Host path that maps to container /workspace
HOST_WORKSPACE="/workspace/swe-swe"

# Host IP for curl test (optional - localhost won't work from inside another container)
HOST_IP="${HOST_IP:-}"

# Stop and remove existing container if it exists (idempotent)
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true

# Run the test container
docker run -d \
    --name "$CONTAINER_NAME" \
    -p "$HOST_PORT:$CONTAINER_PORT" \
    -v "$HOST_WORKSPACE:/workspace" \
    swe-swe-test:latest

# Wait for startup
echo "Waiting for container to start..."
sleep 3

# Show container status
docker ps --filter "name=$CONTAINER_NAME"

# Show logs
echo "Container logs:"
docker logs "$CONTAINER_NAME"

# Verify server responds (only if HOST_IP is set)
if [[ -n "$HOST_IP" ]]; then
    echo "Testing server response at http://$HOST_IP:$HOST_PORT/ ..."
    if curl -s -o /dev/null -w "%{http_code}" "http://$HOST_IP:$HOST_PORT/" | grep -q "200\|302"; then
        echo "Phase 3 complete: Test container running at http://$HOST_IP:$HOST_PORT/"
    else
        echo "Warning: Server may not be responding correctly, check logs above"
        exit 1
    fi
else
    echo "Phase 3 complete: Test container running (set HOST_IP to enable curl check)"
fi
