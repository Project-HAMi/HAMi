FROM vllm/vllm-openai:v0.34.0@sha256:20fdb089454afbb324efa4110a2c025ff7b27cc1a086e07aa580b18df8ad1ce1

EXPOSE 8000

# Base image ENTRYPOINT is already python -m vllm.entrypoints.openai.api_server
# so CMD only needs the flags (exec-form, no shell expansion needed)
CMD ["--model", "Qwen/Qwen3-8B", "--host", "0.0.0.0", "--port", "8000", \
     "--tensor-parallel-size", "1", "--max-model-len", "8192", \
     "--gpu-memory-utilization", "0.9", "--dtype", "bfloat16"]
