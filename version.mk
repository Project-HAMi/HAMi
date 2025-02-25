# Build configuration
GO := go
GO111MODULE := on
CMDS := scheduler vGPUmonitor
DEVICES := nvidia
ARCH := linux-amd64

# Path configuration
OUTPUT_DIR := bin
TARGET_ARCH := amd64
DEST_DIR := /usr/local/vgpu

# Base images
GOLANG_IMAGE := golang:1.22.5-bullseye
NVIDIA_IMAGE := nvidia/cuda:12.3.2-devel-ubuntu20.04

# Version control
VERSION := v0.0.1
IMG_NAME := hami
IMG_TAG := ${IMG_NAME}:${VERSION}