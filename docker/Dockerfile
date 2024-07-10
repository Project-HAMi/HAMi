ARG GOLANG_IMAGE
ARG NVIDIA_RUNTIME_IMAGE=nvidia/cuda:12.2.0-base-ubuntu22.04
ARG NVIDIA_IMAGE

FROM $GOLANG_IMAGE AS build

# cause cgo required now just use QEMU to multi-arch compile
# FIXME could switch CXX/CC/AR
#   with {GNUARCH}-linux-gnu-g++/{GNUARCH}-linux-gnu-gcc/{GNUARCH}-linux-gnu-gcc-ar
#   or zig cc instead.
FROM $GOLANG_IMAGE AS GOBUILD
ADD . /k8s-vgpu
ARG GOPROXY=https://goproxy.cn,direct
RUN apt-get update && apt-get -y install libhwloc-dev libdrm-dev
ARG TARGETARCH
RUN cd /k8s-vgpu && CGO_ENABLED=1 GOARCH=${TARGETARCH} make all

FROM $NVIDIA_IMAGE AS NVBUILD
COPY ./libvgpu /libvgpu
WORKDIR /libvgpu
RUN apt-get -y update && apt-get -y install wget

# install cmake in arch-insensitive path
RUN set -eux; \
    \
    case $(uname -m) in \
        x86_64) CMAKE_DIR=cmake-3.19.8-Linux-x86_64;; \
        aarch64) CMAKE_DIR=cmake-3.19.8-Linux-aarch64;; \
        *) echo "unsupported architecture"; exit 1 ;; \
    esac; \
    wget https://cmake.org/files/v3.19/${CMAKE_DIR}.tar.gz; \
    tar -xf ${CMAKE_DIR}.tar.gz; \
    mv /libvgpu/${CMAKE_DIR} /libvgpu/cmake-3.19.8; \
    cp /libvgpu/cmake-3.19.8/bin/cmake /libvgpu/cmake-3.19.8/bin/cmake3

ENV PATH="/libvgpu/cmake-3.19.8/bin:${PATH}"
RUN apt-get -y install openssl libssl-dev
RUN bash ./build.sh

FROM ${NVIDIA_RUNTIME_IMAGE}
ENV NVIDIA_DISABLE_REQUIRE="true"
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=utility

ARG VERSION
LABEL version="$VERSION"
LABEL maintainer="opensource@4paradigm.com"
COPY ./LICENSE /k8s-vgpu/LICENSE
COPY --from=GOBUILD /k8s-vgpu/bin /k8s-vgpu/bin
COPY ./docker/entrypoint.sh /k8s-vgpu/bin/entrypoint.sh
COPY ./lib /k8s-vgpu/lib
COPY --from=NVBUILD /libvgpu/build/libvgpu.so /k8s-vgpu/lib/nvidia/

ENV PATH="/k8s-vgpu/bin:${PATH}"
ARG DEST_DIR
ENTRYPOINT ["/bin/bash", "-c", "entrypoint.sh  $DEST_DIR"]
