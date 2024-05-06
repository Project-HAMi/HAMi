#!/bin/bash
#
# Copyright © 2024 HAMi Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
set -e
[[ -z ${SHORT_VERSION} ]] && SHORT_VERSION=$(git rev-parse --abbrev-ref HEAD)
[[ -z ${COMMIT_CODE} ]] && COMMIT_CODE=$(git describe --abbrev=100 --always)

export SHORT_VERSION
export COMMIT_CODE
export VERSION="${SHORT_VERSION}-${COMMIT_CODE}"
export LATEST_VERSION="latest"
export GOLANG_IMAGE="golang:1.21-bullseye"
export NVIDIA_IMAGE="nvidia/cuda:12.2.0-devel-ubuntu20.04"
export DEST_DIR="/usr/local"

IMAGE=${IMAGE-"projecthami/hami"}

function go_build() {
  [[ -z "$J" ]] && J=$(nproc | awk '{print int(($0 + 1)/ 2)}')
  make -j$J
}

function docker_build() {
    docker build --build-arg VERSION="${VERSION}" --build-arg GOLANG_IMAGE=${GOLANG_IMAGE} --build-arg NVIDIA_IMAGE=${NVIDIA_IMAGE} --build-arg DEST_DIR=${DEST_DIR} -t "${IMAGE}:${VERSION}" -f docker/Dockerfile .
    docker tag "${IMAGE}:${VERSION}" "${IMAGE}:${SHORT_VERSION}"
    docker tag "${IMAGE}:${VERSION}" "${IMAGE}:${LATEST_VERSION}"
}

function docker_push() {
    #docker push "${IMAGE}:${VERSION}"
    docker push "${IMAGE}:${SHORT_VERSION}"
    docker push "${IMAGE}:${LATEST_VERSION}"
}

go_build
docker_build
docker_push