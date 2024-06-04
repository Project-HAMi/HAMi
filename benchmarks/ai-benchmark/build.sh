#!/bin/bash
set -e

IMAGE="vgpu-benchmark"
TAG="v0.0.1"
PLATFORM="linux/amd64"

docker buildx build --push \
  --platform $PLATFORM \
  --no-cache \
  -t "$IMAGE:$TAG" \
  -f Dockerfile .