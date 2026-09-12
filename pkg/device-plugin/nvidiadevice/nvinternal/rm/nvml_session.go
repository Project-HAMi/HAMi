/*
 * Copyright (c) 2026, HAMi.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package rm

import (
	"fmt"
	"sync"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"k8s.io/klog/v2"
)

// NVMLSession provides a centralized, reference-counted manager for NVML
// initialization and shutdown across components.
type NVMLSession struct {
	mu          sync.Mutex
	nvmllib     nvml.Interface
	refCount    int
	initialized bool
	owned       bool
}

var (
	defaultSession     *NVMLSession
	defaultSessionOnce sync.Once
)

// GetNVMLSession returns the default singleton NVMLSession instance.
func GetNVMLSession(nvmllib nvml.Interface) *NVMLSession {
	defaultSessionOnce.Do(func() {
		defaultSession = NewNVMLSession(nvmllib)
	})
	if nvmllib != nil && defaultSession.nvmllib == nil {
		defaultSession.nvmllib = nvmllib
	}
	return defaultSession
}

// NewNVMLSession constructs a new NVMLSession with the provided nvml.Interface.
func NewNVMLSession(nvmllib nvml.Interface) *NVMLSession {
	if nvmllib == nil {
		nvmllib = nvml.New()
	}
	return &NVMLSession{
		nvmllib: nvmllib,
	}
}

// Init initializes NVML if not already initialized, and increments reference count.
func (s *NVMLSession) Init() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.initialized {
		s.refCount++
		return nil
	}

	ret := s.nvmllib.Init()
	if ret != nvml.SUCCESS && ret != nvml.ERROR_ALREADY_INITIALIZED {
		return fmt.Errorf("failed to initialize NVML: %s", nvml.ErrorString(ret))
	}

	s.initialized = true
	s.refCount = 1
	if ret == nvml.SUCCESS {
		s.owned = true
	} else {
		s.owned = false
	}
	return nil
}

// Shutdown decrements the reference count and shuts down NVML when count reaches 0.
func (s *NVMLSession) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.initialized {
		return
	}

	s.refCount--
	if s.refCount <= 0 {
		if s.owned {
			ret := s.nvmllib.Shutdown()
			if ret != nvml.SUCCESS && ret != nvml.ERROR_UNINITIALIZED {
				klog.ErrorS(fmt.Errorf("%s", nvml.ErrorString(ret)), "NVML shutdown error")
			}
		}
		s.initialized = false
		s.owned = false
		s.refCount = 0
	}
}

// Interface returns the underlying nvml.Interface handle.
func (s *NVMLSession) Interface() nvml.Interface {
	return s.nvmllib
}
