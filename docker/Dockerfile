ARG GOLANG_IMAGE=golang:1.22.5-bullseye
ARG NVIDIA_IMAGE=nvidia/cuda:12.2.0-devel-ubuntu20.04

FROM $GOLANG_IMAGE AS build
FROM $GOLANG_IMAGE AS gobuild
ARG GOPROXY
ARG VERSION
ADD . /k8s-vgpu
#RUN --mount=type=cache,target=/go/pkg/mod \
#    cd /k8s-vgpu && make all
RUN cd /k8s-vgpu && make all VERSION=$VERSION
RUN go install github.com/NVIDIA/mig-parted/cmd/nvidia-mig-parted@v0.10.0

FROM $NVIDIA_IMAGE AS nvbuild
COPY ./libvgpu /libvgpu
WORKDIR /libvgpu
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get -y update; apt-get -y install cmake
RUN bash ./build.sh

FROM nvidia/cuda:12.6.3-base-ubi8
RUN rm -rf /usr/local/cuda-12.6/compat/libcuda.so*
ENV NVIDIA_DISABLE_REQUIRE="true"
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility

ARG VERSION
LABEL version="$VERSION"
LABEL maintainer="opensource@4paradigm.com"
COPY ./LICENSE /k8s-vgpu/LICENSE
COPY --from=gobuild /k8s-vgpu/bin /k8s-vgpu/bin
COPY --from=gobuild /go/bin/nvidia-mig-parted /k8s-vgpu/bin/
COPY ./docker/entrypoint.sh /k8s-vgpu/bin/entrypoint.sh
COPY ./lib /k8s-vgpu/lib
COPY --from=nvbuild /libvgpu/build/libvgpu.so /k8s-vgpu/lib/nvidia/libvgpu.so."$VERSION"
COPY ./docker/vgpu-init.sh /k8s-vgpu/bin/vgpu-init.sh

ENV PATH="/k8s-vgpu/bin:${PATH}"
ARG DEST_DIR
ENTRYPOINT ["/bin/bash", "-c", "entrypoint.sh  $DEST_DIR"]
