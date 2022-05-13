ARG GOLANG_IMAGE=golang:1.16.6-buster
ARG NVIDIA_IMAGE=nvidia/cuda:11.2.1-base-ubuntu20.04
FROM $GOLANG_IMAGE AS build

ADD . /k8s-vgpu
#ENV GOPRIVATE="gitlab.4pd.io/*"
ARG GOPROXY=https://goproxy.cn,direct
ARG VERSION="unknown"
RUN cd /k8s-vgpu && make all


FROM $NVIDIA_IMAGE
ENV NVIDIA_DISABLE_REQUIRE="true"
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=utility

ARG VERSION="unknown"
LABEL version="$VERSION"
LABEL maintainer="opensource@4paradigm.com"

COPY ./LICENSE /k8s-vgpu/LICENSE
COPY --from=build /k8s-vgpu/bin /k8s-vgpu/bin
COPY ./docker/entrypoint.sh /k8s-vgpu/bin/entrypoint.sh
COPY ./lib /k8s-vgpu/lib

ENV PATH="/k8s-vgpu/bin:${PATH}"

ENTRYPOINT ["entrypoint.sh"]
