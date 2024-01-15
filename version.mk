GO=go
GO111MODULE=on
CMDS=scheduler vGPUmonitor
DEVICES=mlu nvidia
OUTPUT_DIR=bin
TARGET_ARCH=amd64
GOLANG_IMAGE=golang:1.21-bullseye
NVIDIA_IMAGE=nvidia/cuda:11.2.2-base-ubuntu20.04

VERSION = v0.0.1
IMG_NAME ="k8s-vgpu-scheduler"
IMG_TAG="${IMG_NAME}:${VERSION}"