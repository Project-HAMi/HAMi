#!/bin/bash
set -e

# --- Configuration ---
IMAGE_NAME="qwen3-vllm-service"
CONTAINER_NAME="qwen3-bench-runner"
SCRIPT_NAME="benchmark.py"
VLLM_URL="http://127.0.0.1:8000"

# 1. Start the container in background
echo "Starting vLLM container in background..."
docker run -d \
  --name "$CONTAINER_NAME" \
  --gpus all \
  --rm \
  -p 8000:8000 \
  "$IMAGE_NAME"

# 2. Wait for vLLM to be ready by polling /v1/models
echo "Waiting for vLLM to load model and become ready..."
while true; do
  if curl -s "$VLLM_URL/v1/models" > /dev/null 2>&1; then
    echo "Service is ready!"
    break
  fi
  echo "  ...waiting (checking every 5s)..."
  sleep 5
done

# 3. Run the benchmark, forwarding any CLI args
echo "Starting benchmark..."
python3 "$SCRIPT_NAME" "$@"

# 4. Cleanup
echo "Stopping container..."
docker stop "$CONTAINER_NAME"
echo "All done."
