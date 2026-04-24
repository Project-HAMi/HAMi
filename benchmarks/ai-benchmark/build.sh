#!/bin/bash
set -e

SERVER_IMAGE=${SERVER_IMAGE:-"qwen3-vllm-service"}
CLIENT_IMAGE=${CLIENT_IMAGE:-"vllm-bench-client"}
TAG=${TAG:-"latest"}
PLATFORM=${PLATFORM:-"linux/amd64"}

echo "Building vLLM server image..."
docker build \
  ${REGISTRY:+--push} \
  ${PLATFORM:+--platform "$PLATFORM"} \
  -t "${REGISTRY:+$REGISTRY/}${SERVER_IMAGE}:$TAG" \
  -f Dockerfile .

echo "Building benchmark client image..."
docker build \
  ${REGISTRY:+--push} \
  ${PLATFORM:+--platform "$PLATFORM"} \
  -t "${REGISTRY:+$REGISTRY/}${CLIENT_IMAGE}:$TAG" \
  -f Dockerfile.client .
