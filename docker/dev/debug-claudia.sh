#!/bin/bash

echo "Building and running claudia debug container..."
echo "This will show available scripts and build output."
echo ""

# Build and run the debug container
docker-compose -f docker/dev/docker-compose.yml build claudia-debug
docker-compose -f docker/dev/docker-compose.yml run --rm claudia-debug

echo ""
echo "Debug complete. To inspect the container interactively, run:"
echo "docker-compose -f docker/dev/docker-compose.yml run --rm claudia-debug sh"