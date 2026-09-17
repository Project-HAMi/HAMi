ARG GOLANG_IMAGE=golang:1.27.0-bookworm@sha256:ded31c68586d2e49e760acc2e65a884b23d032e9bbbed0ae0c55abd3fcaf4452
ARG NVIDIA_IMAGE=nvidia/cuda:13.3.0-cudnn-devel-ubi8@sha256:e5b2b971730b6d0defd6d1bd7697630e0e599c359190cc4351e3032134e7b401

FROM $GOLANG_IMAGE AS gobuild
ARG GOPROXY
ARG VERSION
ADD . /k8s-vgpu
#RUN --mount=type=cache,target=/go/pkg/mod \
#    cd /k8s-vgpu && make all
RUN cd /k8s-vgpu && make all VERSION=$VERSION

FROM $NVIDIA_IMAGE AS nvbuild
RUN dnf install -y cmake git && dnf clean all
COPY ./libvgpu /libvgpu
COPY .git/modules/libvgpu /libvgpu-git
RUN rm -rf /libvgpu/.git && echo "gitdir: /libvgpu-git" > /libvgpu/.git
WORKDIR /libvgpu
RUN rm -rf /libvgpu/build
RUN bash ./build.sh

# DCGM runtime for MIG GPU-instance utilization; same stage as
# Dockerfile.hamimaster — see that file for the rationale.
FROM debian:bookworm-slim AS dcgm
ARG DCGM_DEB=v4.5.0-1
ARG DCGM_AMD64_SHA256=b6f83b7c3ba3d7b75843402317fea997bfdd53471a668b832f734db4f77d4b6e
ARG DCGM_ARM64_SHA256=3d6a7bd624ee27ac723a16917187cc7b8d31af07a9c8421f6a3aced051ed1815
RUN set -eux; \
    apt-get update; \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends curl ca-certificates; \
    rm -rf /var/lib/apt/lists/*; \
    dcgm_ver="${DCGM_DEB#v}"; \
    arch="$(dpkg --print-architecture)"; \
    case "$arch" in \
      amd64) dl_arch="x86_64"; deb="datacenter-gpu-manager-4-core_${dcgm_ver}_amd64.deb"; want="$DCGM_AMD64_SHA256" ;; \
      arm64) dl_arch="sbsa";   deb="datacenter-gpu-manager-4-core_${dcgm_ver}_arm64.deb"; want="$DCGM_ARM64_SHA256" ;; \
      *) echo "unsupported arch: $arch" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /tmp/dcgm.deb "https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2204/${dl_arch}/${deb}"; \
    echo "${want}  /tmp/dcgm.deb" | sha256sum -c -; \
    mkdir -p /out; \
    dpkg-deb -x /tmp/dcgm.deb /out

FROM nvidia/cuda:13.3.1-base-ubuntu22.04@sha256:ea2a388e99273207c6389b3098a61ebca5ab41d31168dfc4350d0cfa614703e4
RUN apt-get update && \
    apt-mark hold cuda-toolkit-config-common cuda-toolkit-13-config-common && \
    DEBIAN_FRONTEND=noninteractive apt-get upgrade -y && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*
RUN rm -rf /usr/local/cuda-13.3/compat/libcuda.so*
ENV NVIDIA_DISABLE_REQUIRE="true"
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility

# DCGM runtime (libdcgm.so.4 and friends) for MIG GPU-instance utilization.
# go-dcgm dlopens libdcgm.so.4 at runtime; a fresh ldconfig cache keeps that
# lookup cheap.
COPY --from=dcgm /out/ /
RUN ldconfig

ARG VERSION
LABEL version="$VERSION"
LABEL maintainer="info@dynamia.ai"
COPY ./LICENSE /k8s-vgpu/LICENSE
COPY --from=gobuild /k8s-vgpu/bin /k8s-vgpu/bin
COPY ./docker/entrypoint.sh /k8s-vgpu/bin/entrypoint.sh
COPY ./lib /k8s-vgpu/lib
COPY --from=nvbuild /libvgpu/build/libvgpu.so /k8s-vgpu/lib/nvidia/libvgpu.so."$VERSION"
COPY ./docker/vgpu-init.sh /k8s-vgpu/bin/vgpu-init.sh

ENV PATH="/k8s-vgpu/bin:${PATH}"
ARG DEST_DIR
ENTRYPOINT ["/bin/bash", "-c", "entrypoint.sh  $DEST_DIR"]
