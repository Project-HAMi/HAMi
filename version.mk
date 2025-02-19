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
NVIDIA_DEVEL_IMAGE:= nvcr.io/nvidia/cuda:12.6.3-devel-ubuntu22.04
NVIDIA_IMAGE := nvcr.io/nvidia/cuda:12.6.3-base-ubuntu22.04

# Version control
VERSION := v0.0.1
IMG_NAME := hami-device-plugin
IMG_TAG := ${IMG_NAME}:${VERSION}