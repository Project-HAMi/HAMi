ARG GOLANG_IMAGE=golang:1.27.2-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61
ARG NVIDIA_IMAGE=nvidia/cuda:13.4.2-cudnn-devel-ubi8@sha256:7bf81df28de77ac76b1415edfa506645118fd72546842c58b50e544a28d69ec2

FROM $GOLANG_IMAGE AS gobuild
ARG GOPROXY
ARG VERSION
ADD . /k8s-vgpu
#RUN --mount=type=cache,target=/go/pkg/mod \
#    cd /k8s-vgpu && make all
RUN cd /k8s-vgpu && make all VERSION=$VERSION

FROM $NVIDIA_IMAGE AS nvbuild
RUN dnf --disablerepo=cuda install -y cmake git && dnf clean all
COPY ./libvgpu /libvgpu
COPY .git/modules/libvgpu /libvgpu-git
RUN rm -rf /libvgpu/.git && echo "gitdir: /libvgpu-git" > /libvgpu/.git
WORKDIR /libvgpu
RUN rm -rf /libvgpu/build
RUN bash ./build.sh

# DCGM runtime for MIG GPU-instance utilization, sourced from the
# datacenter-gpu-manager deb. The core package provides libdcgm.so.4; the
# -proprietary package provides libdcgmmoduleprofiling.so.4, which DCGM needs to
# serve DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO for MIG instances; the monitor dlopens
# both at runtime.
FROM debian:bookworm-slim AS dcgm
ARG DCGM_DEB=v4.5.0-1
ARG DCGM_AMD64_SHA256=b6f83b7c3ba3d7b75843402317fea997bfdd53471a668b832f734db4f77d4b6e
ARG DCGM_ARM64_SHA256=3d6a7bd624ee27ac723a16917187cc7b8d31af07a9c8421f6a3aced051ed1815
# Profiling-module package SHA256 (the -proprietary package, for DCGM 4.5.0).
# Generate on a Linux host (amd64/arm64) with:
#   curl -fsSL https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2204/x86_64/datacenter-gpu-manager-4-proprietary_4.5.0-1_amd64.deb | sha256sum
#   curl -fsSL https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2204/sbsa/datacenter-gpu-manager-4-proprietary_4.5.0-1_arm64.deb | sha256sum
# Leave empty to skip the checksum (the package is still installed).
ARG DCGM_PROF_AMD64_SHA256=868518132bac1710dd2377af930afc7ad95dd71ce918c1340f2f714f5b61cfb5
ARG DCGM_PROF_ARM64_SHA256=6c6291cb494043fd63c76fa605488b524d853864ef8c8c374609d2ab1df7a312
RUN set -eux; \
    apt-get update; \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends curl ca-certificates; \
    rm -rf /var/lib/apt/lists/*; \
    dcgm_ver="${DCGM_DEB#v}"; \
    arch="$(dpkg --print-architecture)"; \
    case "$arch" in \
      amd64) dl_arch="x86_64"; \
             core="datacenter-gpu-manager-4-core_${dcgm_ver}_amd64.deb"; core_sha="$DCGM_AMD64_SHA256"; \
             prof="datacenter-gpu-manager-4-proprietary_${dcgm_ver}_amd64.deb"; prof_sha="$DCGM_PROF_AMD64_SHA256" ;; \
      arm64) dl_arch="sbsa"; \
             core="datacenter-gpu-manager-4-core_${dcgm_ver}_arm64.deb"; core_sha="$DCGM_ARM64_SHA256"; \
             prof="datacenter-gpu-manager-4-proprietary_${dcgm_ver}_arm64.deb"; prof_sha="$DCGM_PROF_ARM64_SHA256" ;; \
      *) echo "unsupported arch: $arch" >&2; exit 1 ;; \
    esac; \
    url_base="https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2204/${dl_arch}"; \
    mkdir -p /out; \
    for spec in core prof; do \
      if [ "$spec" = "core" ]; then pkg="$core"; sha="$core_sha"; else pkg="$prof"; sha="$prof_sha"; fi; \
      curl -fsSL -o "/tmp/$pkg" "${url_base}/${pkg}"; \
      if [ -n "$sha" ]; then echo "${sha}  /tmp/$pkg" | sha256sum -c -; fi; \
      dpkg-deb -x "/tmp/$pkg" /out; \
    done

FROM nvidia/cuda:13.4.2-base-ubuntu22.04@sha256:5d8ec7ec049bc403934ac149b56083f27eeba2863c7bccd38d092e7c905d28e0
RUN apt-get update && \
    apt-mark hold cuda-toolkit-config-common cuda-toolkit-13-config-common && \
    DEBIAN_FRONTEND=noninteractive apt-get upgrade -y && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*
RUN rm -rf /usr/local/cuda-*/compat/libcuda.so*
ENV NVIDIA_DISABLE_REQUIRE="true"
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility

# DCGM runtime for MIG GPU-instance utilization: libdcgm.so.4 from the core
# package and libdcgmmoduleprofiling.so.4 from the -proprietary package. go-dcgm
# dlopens both at runtime; a fresh ldconfig cache keeps the lookup cheap.
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
