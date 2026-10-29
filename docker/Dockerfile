ARG GOLANG_IMAGE=golang:1.27.3-bookworm@sha256:972b4110ba0f47539a975c31e52c64765e1ff19f2d74469ae4a595c6676e80a5
ARG NVIDIA_IMAGE=nvidia/cuda:13.4.3-cudnn-devel-ubi8@sha256:53d7fab8667b6524238d2ff5b03b4be788e7afd7c00e5cd15cc2af8f911d8010

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
# datacenter-gpu-manager debs. The libdcgm package provides libdcgm.so.4; the
# -proprietary package provides libdcgmmoduleprofiling.so.4, which DCGM needs to
# serve DCGM_FI_PROF_GR_ENGINE_UTIL_RATIO for MIG instances; the monitor dlopens
# both at runtime.
FROM debian:bookworm-slim@sha256:7ff6ba31527b9e336162fa061746c366c4b2032f4d2cff2078bea43f78efe3f8 AS dcgm
ARG DCGM_DEB=v4.7.0-2
ARG DCGM_AMD64_SHA256=175b7166b86f01afb240a87224aed8aad8ab7bac9f425e655e9be4f5b83ec478
ARG DCGM_ARM64_SHA256=eb54d0a43294831ff36fb7c5aeb2d947899062be6391696a16edca9b05031b9d
# Profiling-module package SHA256 (the -proprietary package, for DCGM 4.7.0).
# Generate on a Linux host (amd64/arm64) with:
#   curl -fsSL https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2204/x86_64/datacenter-gpu-manager-4-proprietary_4.7.0-2_amd64.deb | sha256sum
#   curl -fsSL https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2204/sbsa/datacenter-gpu-manager-4-proprietary_4.7.0-2_arm64.deb | sha256sum
# Leave empty to skip the checksum (the package is still installed).
ARG DCGM_PROF_AMD64_SHA256=3ca1503292cdeb92207a062fc5c53915bb0a79358372716d3162a6ee3f2b4bc5
ARG DCGM_PROF_ARM64_SHA256=a8e91bb90b61df8c93c2e302e796047ec9cf62128f8ec7ad69e2da4ad8d521f5
RUN set -eux; \
    apt-get update; \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends curl ca-certificates; \
    rm -rf /var/lib/apt/lists/*; \
    dcgm_ver="${DCGM_DEB#v}"; \
    arch="$(dpkg --print-architecture)"; \
    case "$arch" in \
      amd64) dl_arch="x86_64"; \
             core="libdcgm_${dcgm_ver}_amd64.deb"; core_sha="$DCGM_AMD64_SHA256"; \
             prof="datacenter-gpu-manager-4-proprietary_${dcgm_ver}_amd64.deb"; prof_sha="$DCGM_PROF_AMD64_SHA256" ;; \
      arm64) dl_arch="sbsa"; \
             core="libdcgm_${dcgm_ver}_arm64.deb"; core_sha="$DCGM_ARM64_SHA256"; \
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

FROM nvidia/cuda:13.4.3-base-ubuntu22.04@sha256:7416c5229cb6c5b2e7cd7a5930ec97db03f3e064d56b8abacf525f6b4da0b271
RUN apt-get update && \
    apt-mark hold cuda-toolkit-config-common cuda-toolkit-13-config-common && \
    DEBIAN_FRONTEND=noninteractive apt-get upgrade -y && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*
RUN rm -rf /usr/local/cuda-*/compat/libcuda.so*
ENV NVIDIA_DISABLE_REQUIRE="true"
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility

# DCGM runtime for MIG GPU-instance utilization: libdcgm.so.4 from the libdcgm
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
