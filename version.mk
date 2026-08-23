GO=go
GO111MODULE=on
CMDS=scheduler vGPUmonitor hami-cli
DEVICES=nvidia
OUTPUT_DIR=bin
TARGET_PLATFORMS=linux/amd64
GOLANG_IMAGE=golang:1.26.5-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651
NVIDIA_IMAGE=nvidia/cuda:13.3.0-cudnn-devel-ubi8@sha256:e5b2b971730b6d0defd6d1bd7697630e0e599c359190cc4351e3032134e7b401
DEST_DIR=/usr/local/vgpu/

VERSION = v0.0.1
IMG_NAME =hami
IMG_TAG=${VERSION}
