#!/bin/bash
set -e

# --- Configuration ---
IMAGE_NAME="vllm-bench-server"
CONTAINER_NAME="qwen3-bench-runner"
SCRIPT_NAME="benchmark.py"
VLLM_URL="http://127.0.0.1:8000"
MAX_RETRIES=${MAX_RETRIES:-120}

if ! [ "$MAX_RETRIES" -gt 0 ] 2>/dev/null; then
  echo "MAX_RETRIES must be a positive integer" >&2
  exit 1
fi

cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  echo "Stopping container..."
  docker stop "$container_id" || {
    stop_status=$?
    if [ "$status" -eq 0 ]; then
      status=$stop_status
    fi
  }
  if [ "$status" -eq 0 ]; then
    echo "All done."
  fi
  exit "$status"
}

# 1. Start the container in background
echo "Starting vLLM container in background..."
container_id=$(docker run -d \
  --name "$CONTAINER_NAME" \
  --gpus all \
  --rm \
  -p 8000:8000 \
  "$IMAGE_NAME")
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

# 2. Wait for vLLM to be ready by polling /v1/models
echo "Waiting for vLLM to load model and become ready..."
i=1
while [ "$i" -le "$MAX_RETRIES" ]; do
  if http_status=$(curl --silent --output /dev/null --write-out '%{http_code}' \
    --connect-timeout 2 --max-time 5 "$VLLM_URL/v1/models") && [ "$http_status" = 200 ]; then
    echo "Service is ready!"
    break
  fi
  if [ "$i" -eq "$MAX_RETRIES" ]; then
    echo "vLLM did not become ready after $MAX_RETRIES attempts" >&2
    exit 1
  fi
  echo "  ...waiting (checking every 5s)..."
  sleep 5
  i=$((i + 1))
done

# 3. Run the benchmark, forwarding any CLI args
echo "Starting benchmark..."
python3 "$SCRIPT_NAME" "$@"
