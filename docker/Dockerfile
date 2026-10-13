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

FROM nvidia/cuda:13.4.2-base-ubuntu22.04@sha256:5d8ec7ec049bc403934ac149b56083f27eeba2863c7bccd38d092e7c905d28e0
RUN apt-get update && \
    apt-mark hold cuda-toolkit-config-common cuda-toolkit-13-config-common && \
    DEBIAN_FRONTEND=noninteractive apt-get upgrade -y && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*
RUN rm -rf /usr/local/cuda-13.3/compat/libcuda.so*
ENV NVIDIA_DISABLE_REQUIRE="true"
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility

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
