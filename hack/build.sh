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
export GOLANG_IMAGE="golang:1.26.2-bookworm"
export NVIDIA_IMAGE="nvidia/cuda:13.3.0-cudnn-devel-ubi8"
export DEST_DIR="/usr/local"

IMAGE=${IMAGE-"projecthami/hami"}

function go_build() {
  [[ -z "$J" ]] && J=$(nproc | awk '{print int(($0 + 1)/ 2)}')
  make -j$J
}

function docker_build() {
    # Linked git worktrees have .git as a file, not a directory.
    # Docker build context then lacks .git/modules/libvgvu, which
    # the Dockerfile COPYs to preserve libvgpu git metadata for
    # 'git describe' inside the container build.
    # Temporarily create it; restore on exit.
    local _restore_git=""
    if [ -f .git ]; then
      local _git_common
      _git_common=$(git rev-parse --git-common-dir 2>/dev/null)
      if [ -n "$_git_common" ] && [ -d "$_git_common/modules/libvgpu" ]; then
        _restore_git=$(mktemp)
        cp .git "$_restore_git"
        # shellcheck disable=SC2064
        trap "rm -rf .git && cp '$_restore_git' .git && rm -f '$_restore_git'" EXIT
        rm -f .git
        mkdir -p .git/modules
        cp -r "$_git_common/modules/libvgpu" .git/modules/
      fi
    fi

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
