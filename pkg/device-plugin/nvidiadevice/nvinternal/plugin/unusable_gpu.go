/*
 * Copyright (c) 2026, HAMi.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package plugin

import (
	"errors"
	"fmt"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

// unusableGPUError reports an NVML result that means the GPU cannot be used
// until it recovers or is reset: it has fallen off the bus (Xid 79, reported as
// GPU_IS_LOST or, often, as UNKNOWN), lacks power, has an interrupt problem, or
// needs a reset after a MIG mode change that could not be activated. The startup walks skip such a GPU instead of
// failing, so one GPU does not stop the plugin from serving the others.
type unusableGPUError struct {
	msg string
	ret nvml.Return
}

func (e *unusableGPUError) Error() string { return e.msg }

func isUnusableGPUReturn(ret nvml.Return) bool {
	switch ret {
	case nvml.ERROR_GPU_IS_LOST, nvml.ERROR_UNKNOWN, nvml.ERROR_RESET_REQUIRED,
		nvml.ERROR_INSUFFICIENT_POWER, nvml.ERROR_IRQ_ISSUE:
		return true
	default:
		return false
	}
}

// nvmlError formats an NVML failure. The message is the same for every result;
// only the type differs, so isUnusableGPU can recognize the skippable ones.
func nvmlError(ret nvml.Return, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if isUnusableGPUReturn(ret) {
		return &unusableGPUError{msg: msg, ret: ret}
	}
	return errors.New(msg)
}

func isUnusableGPU(err error) bool {
	var unusable *unusableGPUError
	return errors.As(err, &unusable)
}
