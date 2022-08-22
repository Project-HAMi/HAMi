#!/bin/bash
#
# Copyright © 2021 peizhaoyou <peizhaoyou@4paradigm.com>
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

#IMAGE=${IMAGE-"m7-ieg-pico-test01:5000/k8s-vgpu"}
IMAGE=${IMAGE-"4pdosc/k8s-vdevice"}

function go_build() {
  [[ -z "$J" ]] && J=$(nproc | awk '{print int(($0 + 1)/ 2)}')
  make -j$J
}

function docker_build() {
    docker build --build-arg VERSION="${VERSION}" -t "${IMAGE}:${VERSION}" -f docker/Dockerfile .
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