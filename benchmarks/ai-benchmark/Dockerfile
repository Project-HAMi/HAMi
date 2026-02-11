# This Dockerfile is used to build a Docker image for running the AI Benchmark.
# It is based on the tensorflow/tensorflow:latest-gpu image.

FROM tensorflow/tensorflow:2.21.0rc0-gpu

# Set the working directory to /ai-benchmark
WORKDIR ai-benchmark

# Update the package list and install git and apt-utils
RUN apt-get update && \
    apt-get install -y --no-install-recommends apt-utils git && \
    rm -rf /var/lib/apt/lists/* && \
    pip install --no-cache-dir --upgrade pip && \
    git clone https://github.com/Project-HAMi/ai-benchmark . && \
    pip install --no-cache-dir -r requirements.txt

# Set the default command to run when the container starts
CMD ["python", "./main.py"]