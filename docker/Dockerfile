ARG GOLANG_IMAGE
ARG NVIDIA_IMAGE

FROM $GOLANG_IMAGE AS build
FROM $GOLANG_IMAGE AS GOBUILD
ARG GOPROXY
ADD . /k8s-vgpu
RUN cd /k8s-vgpu && make all

FROM $NVIDIA_IMAGE AS NVBUILD
COPY ./libvgpu /libvgpu
WORKDIR /libvgpu
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get -y update; apt-get -y install cmake
RUN bash ./build.sh

FROM nvidia/cuda:12.4.1-base-ubuntu22.04
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
