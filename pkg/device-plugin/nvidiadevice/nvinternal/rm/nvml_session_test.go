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
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/NVIDIA/go-nvml/pkg/nvml/mock"
)

func TestNVMLSessionLifecycle(t *testing.T) {
	initCount := 0
	shutdownCount := 0

	mockNVML := &mock.Interface{
		InitFunc: func() nvml.Return {
			initCount++
			return nvml.SUCCESS
		},
		ShutdownFunc: func() nvml.Return {
			shutdownCount++
			return nvml.SUCCESS
		},
	}

	session := NewNVMLSession(mockNVML)

	// First Init should call nvml.Init()
	err := session.Init()
	if err != nil {
		t.Fatalf("unexpected error on Init: %v", err)
	}
	if initCount != 1 {
		t.Errorf("expected initCount=1, got %d", initCount)
	}
	if !session.initialized {
		t.Errorf("expected session.initialized=true")
	}
	if !session.owned {
		t.Errorf("expected session.owned=true")
	}
	if session.refCount != 1 {
		t.Errorf("expected session.refCount=1, got %d", session.refCount)
	}

	// Second Init should increment refCount without calling nvml.Init() again
	err = session.Init()
	if err != nil {
		t.Fatalf("unexpected error on second Init: %v", err)
	}
	if initCount != 1 {
		t.Errorf("expected initCount=1, got %d", initCount)
	}
	if session.refCount != 2 {
		t.Errorf("expected session.refCount=2, got %d", session.refCount)
	}

	// First Shutdown should decrement refCount without calling nvml.Shutdown()
	session.Shutdown()
	if shutdownCount != 0 {
		t.Errorf("expected shutdownCount=0, got %d", shutdownCount)
	}
	if session.refCount != 1 {
		t.Errorf("expected session.refCount=1, got %d", session.refCount)
	}

	// Second Shutdown should decrement refCount to 0 and call nvml.Shutdown()
	session.Shutdown()
	if shutdownCount != 1 {
		t.Errorf("expected shutdownCount=1, got %d", shutdownCount)
	}
	if session.refCount != 0 {
		t.Errorf("expected session.refCount=0, got %d", session.refCount)
	}
	if session.initialized {
		t.Errorf("expected session.initialized=false")
	}
}

func TestNVMLSessionAlreadyInitialized(t *testing.T) {
	initCount := 0
	shutdownCount := 0

	mockNVML := &mock.Interface{
		InitFunc: func() nvml.Return {
			initCount++
			return nvml.ERROR_ALREADY_INITIALIZED
		},
		ShutdownFunc: func() nvml.Return {
			shutdownCount++
			return nvml.SUCCESS
		},
	}

	session := NewNVMLSession(mockNVML)

	// Init returning ERROR_ALREADY_INITIALIZED should succeed but owned=false
	err := session.Init()
	if err != nil {
		t.Fatalf("unexpected error on Init: %v", err)
	}
	if initCount != 1 {
		t.Errorf("expected initCount=1, got %d", initCount)
	}
	if !session.initialized {
		t.Errorf("expected session.initialized=true")
	}
	if session.owned {
		t.Errorf("expected session.owned=false")
	}

	// Shutdown should not call nvml.Shutdown() because owned=false
	session.Shutdown()
	if shutdownCount != 0 {
		t.Errorf("expected shutdownCount=0 since session did not own initialization, got %d", shutdownCount)
	}
	if session.refCount != 0 {
		t.Errorf("expected refCount=0, got %d", session.refCount)
	}
}

func TestNVMLSessionInitFailure(t *testing.T) {
	mockNVML := &mock.Interface{
		InitFunc: func() nvml.Return {
			return nvml.ERROR_UNKNOWN
		},
	}

	session := NewNVMLSession(mockNVML)
	err := session.Init()
	if err == nil {
		t.Fatal("expected error on failed Init, got nil")
	}
	if session.initialized {
		t.Errorf("expected session.initialized=false on error")
	}
	if session.refCount != 0 {
		t.Errorf("expected session.refCount=0 on error, got %d", session.refCount)
	}
}
