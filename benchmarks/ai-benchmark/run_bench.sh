#!/bin/bash
set -e

# --- Configuration ---
IMAGE_NAME="vllm-bench-server"
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
MAX_RETRIES=${MAX_RETRIES:-120}
i=1
while [ "$i" -le "$MAX_RETRIES" ]; do
  if curl -s "$VLLM_URL/v1/models" > /dev/null 2>&1; then
    echo "Service is ready!"
    break
  fi
  if [ "$i" -eq "$MAX_RETRIES" ]; then
    echo "vLLM did not become ready after $((MAX_RETRIES * 5))s" >&2
    docker stop "$CONTAINER_NAME"
    exit 1
  fi
  echo "  ...waiting (checking every 5s)..."
  sleep 5
  i=$((i + 1))
done

# 3. Run the benchmark, forwarding any CLI args
echo "Starting benchmark..."
python3 "$SCRIPT_NAME" "$@"

# 4. Cleanup
echo "Stopping container..."
docker stop "$CONTAINER_NAME"
echo "All done."
