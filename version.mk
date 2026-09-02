GO=go
GO111MODULE=on
CMDS=scheduler vGPUmonitor
DEVICES=nvidia
OUTPUT_DIR=bin
TARGET_PLATFORMS=linux/amd64
GOLANG_IMAGE=golang:1.27.0-bookworm@sha256:ded31c68586d2e49e760acc2e65a884b23d032e9bbbed0ae0c55abd3fcaf4452
NVIDIA_IMAGE=nvidia/cuda:13.3.0-cudnn-devel-ubi8@sha256:e5b2b971730b6d0defd6d1bd7697630e0e599c359190cc4351e3032134e7b401
DEST_DIR=/usr/local/vgpu/

VERSION = v0.0.1
IMG_NAME =hami
IMG_TAG=${VERSION}
