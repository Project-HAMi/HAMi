GO=go
GO111MODULE=on
CMDS=scheduler vGPUmonitor
DEVICES=nvidia
OUTPUT_DIR=bin
TARGET_PLATFORMS=linux/amd64
GOLANG_IMAGE=golang:1.26.2-bookworm
NVIDIA_IMAGE=nvidia/cuda:13.3.0-cudnn-devel-ubi8
DEST_DIR=/usr/local/vgpu/

VERSION = v0.0.1
IMG_NAME =hami
IMG_TAG=${VERSION}
