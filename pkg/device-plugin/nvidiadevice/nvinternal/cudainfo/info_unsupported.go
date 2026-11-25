//go:build !linux || !cgo

// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0
package cudainfo

import (
	"fmt"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

func Geometry() (map[string]nvidia.MPSV3Geometry, error) {
	return nil, fmt.Errorf("MPS v3 requires Linux with cgo")
}
