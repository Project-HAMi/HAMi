FROM vllm/vllm-openai:v0.33.0@sha256:d825baf9ce4b1b6bfc5ecc7b79880c7e6748c65a72d697ce3d47607aafa84c6e

EXPOSE 8000

# Base image ENTRYPOINT is already python -m vllm.entrypoints.openai.api_server
# so CMD only needs the flags (exec-form, no shell expansion needed)
CMD ["--model", "Qwen/Qwen3-8B", "--host", "0.0.0.0", "--port", "8000", \
     "--tensor-parallel-size", "1", "--max-model-len", "8192", \
     "--gpu-memory-utilization", "0.9", "--dtype", "bfloat16"]
