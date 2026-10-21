FROM vllm/vllm-openai:v0.32.0@sha256:befccf0e7e4089aa23926bb6f887a85c0d46c2a99e6de5c5ffdf5cb7957c5b34

EXPOSE 8000

# Base image ENTRYPOINT is already python -m vllm.entrypoints.openai.api_server
# so CMD only needs the flags (exec-form, no shell expansion needed)
CMD ["--model", "Qwen/Qwen3-8B", "--host", "0.0.0.0", "--port", "8000", \
     "--tensor-parallel-size", "1", "--max-model-len", "8192", \
     "--gpu-memory-utilization", "0.9", "--dtype", "bfloat16"]
