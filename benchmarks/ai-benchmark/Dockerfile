FROM vllm/vllm-openai:v0.31.0@sha256:c1c9f6fd5c109ba7f0546a59f5b2f15fb87f64c77782e90a27b648b42a8e67c3

EXPOSE 8000

# Base image ENTRYPOINT is already python -m vllm.entrypoints.openai.api_server
# so CMD only needs the flags (exec-form, no shell expansion needed)
CMD ["--model", "Qwen/Qwen3-8B", "--host", "0.0.0.0", "--port", "8000", \
     "--tensor-parallel-size", "1", "--max-model-len", "8192", \
     "--gpu-memory-utilization", "0.9", "--dtype", "bfloat16"]
