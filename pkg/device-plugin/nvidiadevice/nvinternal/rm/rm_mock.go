/*
 * SPDX-License-Identifier: Apache-2.0
 *
 * The HAMi Contributors require contributions made to
 * this file be licensed under the Apache-2.0 license or a
 * compatible open source license.
 */

/*
 * Licensed to NVIDIA CORPORATION under one or more contributor
 * license agreements. See the NOTICE file distributed with
 * this work for additional information regarding copyright
 * ownership. NVIDIA CORPORATION licenses this file to you under
 * the Apache License, Version 2.0 (the "License"); you may
 * not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/*
 * Modifications Copyright The HAMi Authors. See
 * GitHub history for details.
 */

package rm

import (
	"sync"

	spec "github.com/NVIDIA/k8s-device-plugin/api/config/v1"
)

// Ensure, that ResourceManagerMock does implement ResourceManager.
// If this is not the case, regenerate this file with moq.
var _ ResourceManager = &ResourceManagerMock{}

// ResourceManagerMock is a mock implementation of ResourceManager.
//
//	func TestSomethingThatUsesResourceManager(t *testing.T) {
//
//		// make and configure a mocked ResourceManager
//		mockedResourceManager := &ResourceManagerMock{
//			CheckHealthFunc: func(stop <-chan interface{}, unhealthy chan<- *Device) error {
//				panic("mock out the CheckHealth method")
//			},
//			DevicesFunc: func() Devices {
//				panic("mock out the Devices method")
//			},
//			GetDevicePathsFunc: func(strings []string) []string {
//				panic("mock out the GetDevicePaths method")
//			},
//			GetPreferredAllocationFunc: func(available []string, required []string, size int) ([]string, error) {
//				panic("mock out the GetPreferredAllocation method")
//			},
//			ResourceFunc: func() spec.ResourceName {
//				panic("mock out the Resource method")
//			},
//			ValidateRequestFunc: func(annotatedIDs AnnotatedIDs) error {
//				panic("mock out the ValidateRequest method")
//			},
//		}
//
//		// use mockedResourceManager in code that requires ResourceManager
//		// and then make assertions.
//
//	}
type ResourceManagerMock struct {
	// CheckHealthFunc mocks the CheckHealth method.
	CheckHealthFunc func(stop <-chan interface{}, unhealthy chan<- *Device) error

	// DevicesFunc mocks the Devices method.
	DevicesFunc func() Devices

	// GetDevicePathsFunc mocks the GetDevicePaths method.
	GetDevicePathsFunc func(strings []string) []string

	// GetPreferredAllocationFunc mocks the GetPreferredAllocation method.
	GetPreferredAllocationFunc func(available []string, required []string, size int) ([]string, error)

	// ResourceFunc mocks the Resource method.
	ResourceFunc func() spec.ResourceName

	// ValidateRequestFunc mocks the ValidateRequest method.
	ValidateRequestFunc func(annotatedIDs AnnotatedIDs) error

	// calls tracks calls to the methods.
	calls struct {
		// CheckHealth holds details about calls to the CheckHealth method.
		CheckHealth []struct {
			// Stop is the stop argument value.
			Stop <-chan interface{}
			// Unhealthy is the unhealthy argument value.
			Unhealthy chan<- *Device
		}
		// Devices holds details about calls to the Devices method.
		Devices []struct {
		}
		// GetDevicePaths holds details about calls to the GetDevicePaths method.
		GetDevicePaths []struct {
			// Strings is the strings argument value.
			Strings []string
		}
		// GetPreferredAllocation holds details about calls to the GetPreferredAllocation method.
		GetPreferredAllocation []struct {
			// Available is the available argument value.
			Available []string
			// Required is the required argument value.
			Required []string
			// Size is the size argument value.
			Size int
		}
		// Resource holds details about calls to the Resource method.
		Resource []struct {
		}
		// ValidateRequest holds details about calls to the ValidateRequest method.
		ValidateRequest []struct {
			// AnnotatedIDs is the annotatedIDs argument value.
			AnnotatedIDs AnnotatedIDs
		}
	}
	lockCheckHealth            sync.RWMutex
	lockDevices                sync.RWMutex
	lockGetDevicePaths         sync.RWMutex
	lockGetPreferredAllocation sync.RWMutex
	lockResource               sync.RWMutex
	lockValidateRequest        sync.RWMutex
}

// CheckHealth calls CheckHealthFunc.
func (mock *ResourceManagerMock) CheckHealth(stop <-chan interface{}, unhealthy chan<- *Device, disableNVML <-chan bool, ackDisableHealthChecks chan<- bool) error {
	callInfo := struct {
		Stop      <-chan interface{}
		Unhealthy chan<- *Device
	}{
		Stop:      stop,
		Unhealthy: unhealthy,
	}
	mock.lockCheckHealth.Lock()
	mock.calls.CheckHealth = append(mock.calls.CheckHealth, callInfo)
	mock.lockCheckHealth.Unlock()
	if mock.CheckHealthFunc == nil {
		var (
			errOut error
		)
		return errOut
	}
	return mock.CheckHealthFunc(stop, unhealthy)
}

// CheckHealthCalls gets all the calls that were made to CheckHealth.
// Check the length with:
//
//	len(mockedResourceManager.CheckHealthCalls())
func (mock *ResourceManagerMock) CheckHealthCalls() []struct {
	Stop      <-chan interface{}
	Unhealthy chan<- *Device
} {
	var calls []struct {
		Stop      <-chan interface{}
		Unhealthy chan<- *Device
	}
	mock.lockCheckHealth.RLock()
	calls = mock.calls.CheckHealth
	mock.lockCheckHealth.RUnlock()
	return calls
}

// Devices calls DevicesFunc.
func (mock *ResourceManagerMock) Devices() Devices {
	callInfo := struct {
	}{}
	mock.lockDevices.Lock()
	mock.calls.Devices = append(mock.calls.Devices, callInfo)
	mock.lockDevices.Unlock()
	if mock.DevicesFunc == nil {
		var (
			devicesOut Devices
		)
		return devicesOut
	}
	return mock.DevicesFunc()
}

// DevicesCalls gets all the calls that were made to Devices.
// Check the length with:
//
//	len(mockedResourceManager.DevicesCalls())
func (mock *ResourceManagerMock) DevicesCalls() []struct {
} {
	var calls []struct {
	}
	mock.lockDevices.RLock()
	calls = mock.calls.Devices
	mock.lockDevices.RUnlock()
	return calls
}

// GetDevicePaths calls GetDevicePathsFunc.
func (mock *ResourceManagerMock) GetDevicePaths(strings []string) []string {
	callInfo := struct {
		Strings []string
	}{
		Strings: strings,
	}
	mock.lockGetDevicePaths.Lock()
	mock.calls.GetDevicePaths = append(mock.calls.GetDevicePaths, callInfo)
	mock.lockGetDevicePaths.Unlock()
	if mock.GetDevicePathsFunc == nil {
		var (
			stringsOut []string
		)
		return stringsOut
	}
	return mock.GetDevicePathsFunc(strings)
}

// GetDevicePathsCalls gets all the calls that were made to GetDevicePaths.
// Check the length with:
//
//	len(mockedResourceManager.GetDevicePathsCalls())
func (mock *ResourceManagerMock) GetDevicePathsCalls() []struct {
	Strings []string
} {
	var calls []struct {
		Strings []string
	}
	mock.lockGetDevicePaths.RLock()
	calls = mock.calls.GetDevicePaths
	mock.lockGetDevicePaths.RUnlock()
	return calls
}

// GetPreferredAllocation calls GetPreferredAllocationFunc.
func (mock *ResourceManagerMock) GetPreferredAllocation(available []string, required []string, size int) ([]string, error) {
	callInfo := struct {
		Available []string
		Required  []string
		Size      int
	}{
		Available: available,
		Required:  required,
		Size:      size,
	}
	mock.lockGetPreferredAllocation.Lock()
	mock.calls.GetPreferredAllocation = append(mock.calls.GetPreferredAllocation, callInfo)
	mock.lockGetPreferredAllocation.Unlock()
	if mock.GetPreferredAllocationFunc == nil {
		var (
			stringsOut []string
			errOut     error
		)
		return stringsOut, errOut
	}
	return mock.GetPreferredAllocationFunc(available, required, size)
}

// GetPreferredAllocationCalls gets all the calls that were made to GetPreferredAllocation.
// Check the length with:
//
//	len(mockedResourceManager.GetPreferredAllocationCalls())
func (mock *ResourceManagerMock) GetPreferredAllocationCalls() []struct {
	Available []string
	Required  []string
	Size      int
} {
	var calls []struct {
		Available []string
		Required  []string
		Size      int
	}
	mock.lockGetPreferredAllocation.RLock()
	calls = mock.calls.GetPreferredAllocation
	mock.lockGetPreferredAllocation.RUnlock()
	return calls
}

// Resource calls ResourceFunc.
func (mock *ResourceManagerMock) Resource() spec.ResourceName {
	callInfo := struct {
	}{}
	mock.lockResource.Lock()
	mock.calls.Resource = append(mock.calls.Resource, callInfo)
	mock.lockResource.Unlock()
	if mock.ResourceFunc == nil {
		var (
			resourceNameOut spec.ResourceName
		)
		return resourceNameOut
	}
	return mock.ResourceFunc()
}

// ResourceCalls gets all the calls that were made to Resource.
// Check the length with:
//
//	len(mockedResourceManager.ResourceCalls())
func (mock *ResourceManagerMock) ResourceCalls() []struct {
} {
	var calls []struct {
	}
	mock.lockResource.RLock()
	calls = mock.calls.Resource
	mock.lockResource.RUnlock()
	return calls
}

// ValidateRequest calls ValidateRequestFunc.
func (mock *ResourceManagerMock) ValidateRequest(annotatedIDs AnnotatedIDs) error {
	callInfo := struct {
		AnnotatedIDs AnnotatedIDs
	}{
		AnnotatedIDs: annotatedIDs,
	}
	mock.lockValidateRequest.Lock()
	mock.calls.ValidateRequest = append(mock.calls.ValidateRequest, callInfo)
	mock.lockValidateRequest.Unlock()
	if mock.ValidateRequestFunc == nil {
		var (
			errOut error
		)
		return errOut
	}
	return mock.ValidateRequestFunc(annotatedIDs)
}

// ValidateRequestCalls gets all the calls that were made to ValidateRequest.
// Check the length with:
//
//	len(mockedResourceManager.ValidateRequestCalls())
func (mock *ResourceManagerMock) ValidateRequestCalls() []struct {
	AnnotatedIDs AnnotatedIDs
} {
	var calls []struct {
		AnnotatedIDs AnnotatedIDs
	}
	mock.lockValidateRequest.RLock()
	calls = mock.calls.ValidateRequest
	mock.lockValidateRequest.RUnlock()
	return calls
}
