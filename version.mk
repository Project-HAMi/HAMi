GO=go
GO111MODULE=on
CMDS=scheduler vGPUmonitor
DEVICES=nvidia
OUTPUT_DIR=bin
TARGET_ARCH=amd64
<<<<<<< HEAD
GOLANG_IMAGE=golang:1.25.5-bookworm
NVIDIA_IMAGE=nvidia/cuda:12.3.2-devel-ubuntu20.04
DEST_DIR=/usr/local/vgpu/

VERSION = v0.0.1
IMG_NAME =hami
IMG_TAG="${IMG_NAME}:${VERSION}"
=======
GOLANG_IMAGE=golang:1.21-bullseye
NVIDIA_IMAGE=nvidia/cuda:11.2.2-base-ubuntu20.04
DEST_DIR=/usr/local/vgpu/

VERSION = v0.0.1
IMG_NAME ="k8s-vgpu-scheduler"
IMG_TAG="${IMG_NAME}:${VERSION}"
>>>>>>> c7a3893 (Remake this repo to HAMi)
