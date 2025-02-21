// Code generated by moq; DO NOT EDIT.
// github.com/matryer/moq

package resource

import (
	"sync"
)

// Ensure, that DeviceMock does implement Device.
// If this is not the case, regenerate this file with moq.
var _ Device = &DeviceMock{}

// DeviceMock is a mock implementation of Device.
//
//	func TestSomethingThatUsesDevice(t *testing.T) {
//
//		// make and configure a mocked Device
//		mockedDevice := &DeviceMock{
//			GetAttributesFunc: func() (map[string]interface{}, error) {
//				panic("mock out the GetAttributes method")
//			},
//			GetCudaComputeCapabilityFunc: func() (int, int, error) {
//				panic("mock out the GetCudaComputeCapability method")
//			},
//			GetDeviceHandleFromMigDeviceHandleFunc: func() (Device, error) {
//				panic("mock out the GetDeviceHandleFromMigDeviceHandle method")
//			},
//			GetFabricIDsFunc: func() (string, string, error) {
//				panic("mock out the GetFabricIDs method")
//			},
//			GetMigDevicesFunc: func() ([]Device, error) {
//				panic("mock out the GetMigDevices method")
//			},
//			GetNameFunc: func() (string, error) {
//				panic("mock out the GetName method")
//			},
//			GetPCIClassFunc: func() (uint32, error) {
//				panic("mock out the GetPCIClass method")
//			},
//			GetTotalMemoryMBFunc: func() (uint64, error) {
//				panic("mock out the GetTotalMemoryMB method")
//			},
//			IsFabricAttachedFunc: func() (bool, error) {
//				panic("mock out the IsFabricAttached method")
//			},
//			IsMigCapableFunc: func() (bool, error) {
//				panic("mock out the IsMigCapable method")
//			},
//			IsMigEnabledFunc: func() (bool, error) {
//				panic("mock out the IsMigEnabled method")
//			},
//		}
//
//		// use mockedDevice in code that requires Device
//		// and then make assertions.
//
//	}
type DeviceMock struct {
	// GetAttributesFunc mocks the GetAttributes method.
	GetAttributesFunc func() (map[string]interface{}, error)

	// GetCudaComputeCapabilityFunc mocks the GetCudaComputeCapability method.
	GetCudaComputeCapabilityFunc func() (int, int, error)

	// GetDeviceHandleFromMigDeviceHandleFunc mocks the GetDeviceHandleFromMigDeviceHandle method.
	GetDeviceHandleFromMigDeviceHandleFunc func() (Device, error)

	// GetFabricIDsFunc mocks the GetFabricIDs method.
	GetFabricIDsFunc func() (string, string, error)

	// GetMigDevicesFunc mocks the GetMigDevices method.
	GetMigDevicesFunc func() ([]Device, error)

	// GetNameFunc mocks the GetName method.
	GetNameFunc func() (string, error)

	// GetPCIClassFunc mocks the GetPCIClass method.
	GetPCIClassFunc func() (uint32, error)

	// GetTotalMemoryMBFunc mocks the GetTotalMemoryMB method.
	GetTotalMemoryMBFunc func() (uint64, error)

	// IsFabricAttachedFunc mocks the IsFabricAttached method.
	IsFabricAttachedFunc func() (bool, error)

	// IsMigCapableFunc mocks the IsMigCapable method.
	IsMigCapableFunc func() (bool, error)

	// IsMigEnabledFunc mocks the IsMigEnabled method.
	IsMigEnabledFunc func() (bool, error)

	// calls tracks calls to the methods.
	calls struct {
		// GetAttributes holds details about calls to the GetAttributes method.
		GetAttributes []struct {
		}
		// GetCudaComputeCapability holds details about calls to the GetCudaComputeCapability method.
		GetCudaComputeCapability []struct {
		}
		// GetDeviceHandleFromMigDeviceHandle holds details about calls to the GetDeviceHandleFromMigDeviceHandle method.
		GetDeviceHandleFromMigDeviceHandle []struct {
		}
		// GetFabricIDs holds details about calls to the GetFabricIDs method.
		GetFabricIDs []struct {
		}
		// GetMigDevices holds details about calls to the GetMigDevices method.
		GetMigDevices []struct {
		}
		// GetName holds details about calls to the GetName method.
		GetName []struct {
		}
		// GetPCIClass holds details about calls to the GetPCIClass method.
		GetPCIClass []struct {
		}
		// GetTotalMemoryMB holds details about calls to the GetTotalMemoryMB method.
		GetTotalMemoryMB []struct {
		}
		// IsFabricAttached holds details about calls to the IsFabricAttached method.
		IsFabricAttached []struct {
		}
		// IsMigCapable holds details about calls to the IsMigCapable method.
		IsMigCapable []struct {
		}
		// IsMigEnabled holds details about calls to the IsMigEnabled method.
		IsMigEnabled []struct {
		}
	}
	lockGetAttributes                      sync.RWMutex
	lockGetCudaComputeCapability           sync.RWMutex
	lockGetDeviceHandleFromMigDeviceHandle sync.RWMutex
	lockGetFabricIDs                       sync.RWMutex
	lockGetMigDevices                      sync.RWMutex
	lockGetName                            sync.RWMutex
	lockGetPCIClass                        sync.RWMutex
	lockGetTotalMemoryMB                   sync.RWMutex
	lockIsFabricAttached                   sync.RWMutex
	lockIsMigCapable                       sync.RWMutex
	lockIsMigEnabled                       sync.RWMutex
}

// GetAttributes calls GetAttributesFunc.
func (mock *DeviceMock) GetAttributes() (map[string]interface{}, error) {
	if mock.GetAttributesFunc == nil {
		panic("DeviceMock.GetAttributesFunc: method is nil but Device.GetAttributes was just called")
	}
	callInfo := struct {
	}{}
	mock.lockGetAttributes.Lock()
	mock.calls.GetAttributes = append(mock.calls.GetAttributes, callInfo)
	mock.lockGetAttributes.Unlock()
	return mock.GetAttributesFunc()
}

// GetAttributesCalls gets all the calls that were made to GetAttributes.
// Check the length with:
//
//	len(mockedDevice.GetAttributesCalls())
func (mock *DeviceMock) GetAttributesCalls() []struct {
} {
	var calls []struct {
	}
	mock.lockGetAttributes.RLock()
	calls = mock.calls.GetAttributes
	mock.lockGetAttributes.RUnlock()
	return calls
}

// GetCudaComputeCapability calls GetCudaComputeCapabilityFunc.
func (mock *DeviceMock) GetCudaComputeCapability() (int, int, error) {
	if mock.GetCudaComputeCapabilityFunc == nil {
		panic("DeviceMock.GetCudaComputeCapabilityFunc: method is nil but Device.GetCudaComputeCapability was just called")
	}
	callInfo := struct {
	}{}
	mock.lockGetCudaComputeCapability.Lock()
	mock.calls.GetCudaComputeCapability = append(mock.calls.GetCudaComputeCapability, callInfo)
	mock.lockGetCudaComputeCapability.Unlock()
	return mock.GetCudaComputeCapabilityFunc()
}

// GetCudaComputeCapabilityCalls gets all the calls that were made to GetCudaComputeCapability.
// Check the length with:
//
//	len(mockedDevice.GetCudaComputeCapabilityCalls())
func (mock *DeviceMock) GetCudaComputeCapabilityCalls() []struct {
} {
	var calls []struct {
	}
	mock.lockGetCudaComputeCapability.RLock()
	calls = mock.calls.GetCudaComputeCapability
	mock.lockGetCudaComputeCapability.RUnlock()
	return calls
}

// GetDeviceHandleFromMigDeviceHandle calls GetDeviceHandleFromMigDeviceHandleFunc.
func (mock *DeviceMock) GetDeviceHandleFromMigDeviceHandle() (Device, error) {
	if mock.GetDeviceHandleFromMigDeviceHandleFunc == nil {
		panic("DeviceMock.GetDeviceHandleFromMigDeviceHandleFunc: method is nil but Device.GetDeviceHandleFromMigDeviceHandle was just called")
	}
	callInfo := struct {
	}{}
	mock.lockGetDeviceHandleFromMigDeviceHandle.Lock()
	mock.calls.GetDeviceHandleFromMigDeviceHandle = append(mock.calls.GetDeviceHandleFromMigDeviceHandle, callInfo)
	mock.lockGetDeviceHandleFromMigDeviceHandle.Unlock()
	return mock.GetDeviceHandleFromMigDeviceHandleFunc()
}

// GetDeviceHandleFromMigDeviceHandleCalls gets all the calls that were made to GetDeviceHandleFromMigDeviceHandle.
// Check the length with:
//
//	len(mockedDevice.GetDeviceHandleFromMigDeviceHandleCalls())
func (mock *DeviceMock) GetDeviceHandleFromMigDeviceHandleCalls() []struct {
} {
	var calls []struct {
	}
	mock.lockGetDeviceHandleFromMigDeviceHandle.RLock()
	calls = mock.calls.GetDeviceHandleFromMigDeviceHandle
	mock.lockGetDeviceHandleFromMigDeviceHandle.RUnlock()
	return calls
}

// GetFabricIDs calls GetFabricIDsFunc.
func (mock *DeviceMock) GetFabricIDs() (string, string, error) {
	if mock.GetFabricIDsFunc == nil {
		panic("DeviceMock.GetFabricIDsFunc: method is nil but Device.GetFabricIDs was just called")
	}
	callInfo := struct {
	}{}
	mock.lockGetFabricIDs.Lock()
	mock.calls.GetFabricIDs = append(mock.calls.GetFabricIDs, callInfo)
	mock.lockGetFabricIDs.Unlock()
	return mock.GetFabricIDsFunc()
}

// GetFabricIDsCalls gets all the calls that were made to GetFabricIDs.
// Check the length with:
//
//	len(mockedDevice.GetFabricIDsCalls())
func (mock *DeviceMock) GetFabricIDsCalls() []struct {
} {
	var calls []struct {
	}
	mock.lockGetFabricIDs.RLock()
	calls = mock.calls.GetFabricIDs
	mock.lockGetFabricIDs.RUnlock()
	return calls
}

// GetMigDevices calls GetMigDevicesFunc.
func (mock *DeviceMock) GetMigDevices() ([]Device, error) {
	if mock.GetMigDevicesFunc == nil {
		panic("DeviceMock.GetMigDevicesFunc: method is nil but Device.GetMigDevices was just called")
	}
	callInfo := struct {
	}{}
	mock.lockGetMigDevices.Lock()
	mock.calls.GetMigDevices = append(mock.calls.GetMigDevices, callInfo)
	mock.lockGetMigDevices.Unlock()
	return mock.GetMigDevicesFunc()
}

// GetMigDevicesCalls gets all the calls that were made to GetMigDevices.
// Check the length with:
//
//	len(mockedDevice.GetMigDevicesCalls())
func (mock *DeviceMock) GetMigDevicesCalls() []struct {
} {
	var calls []struct {
	}
	mock.lockGetMigDevices.RLock()
	calls = mock.calls.GetMigDevices
	mock.lockGetMigDevices.RUnlock()
	return calls
}

// GetName calls GetNameFunc.
func (mock *DeviceMock) GetName() (string, error) {
	if mock.GetNameFunc == nil {
		panic("DeviceMock.GetNameFunc: method is nil but Device.GetName was just called")
	}
	callInfo := struct {
	}{}
	mock.lockGetName.Lock()
	mock.calls.GetName = append(mock.calls.GetName, callInfo)
	mock.lockGetName.Unlock()
	return mock.GetNameFunc()
}

// GetNameCalls gets all the calls that were made to GetName.
// Check the length with:
//
//	len(mockedDevice.GetNameCalls())
func (mock *DeviceMock) GetNameCalls() []struct {
} {
	var calls []struct {
	}
	mock.lockGetName.RLock()
	calls = mock.calls.GetName
	mock.lockGetName.RUnlock()
	return calls
}

// GetPCIClass calls GetPCIClassFunc.
func (mock *DeviceMock) GetPCIClass() (uint32, error) {
	if mock.GetPCIClassFunc == nil {
		panic("DeviceMock.GetPCIClassFunc: method is nil but Device.GetPCIClass was just called")
	}
	callInfo := struct {
	}{}
	mock.lockGetPCIClass.Lock()
	mock.calls.GetPCIClass = append(mock.calls.GetPCIClass, callInfo)
	mock.lockGetPCIClass.Unlock()
	return mock.GetPCIClassFunc()
}

// GetPCIClassCalls gets all the calls that were made to GetPCIClass.
// Check the length with:
//
//	len(mockedDevice.GetPCIClassCalls())
func (mock *DeviceMock) GetPCIClassCalls() []struct {
} {
	var calls []struct {
	}
	mock.lockGetPCIClass.RLock()
	calls = mock.calls.GetPCIClass
	mock.lockGetPCIClass.RUnlock()
	return calls
}

// GetTotalMemoryMB calls GetTotalMemoryMBFunc.
func (mock *DeviceMock) GetTotalMemoryMB() (uint64, error) {
	if mock.GetTotalMemoryMBFunc == nil {
		panic("DeviceMock.GetTotalMemoryMBFunc: method is nil but Device.GetTotalMemoryMB was just called")
	}
	callInfo := struct {
	}{}
	mock.lockGetTotalMemoryMB.Lock()
	mock.calls.GetTotalMemoryMB = append(mock.calls.GetTotalMemoryMB, callInfo)
	mock.lockGetTotalMemoryMB.Unlock()
	return mock.GetTotalMemoryMBFunc()
}

// GetTotalMemoryMBCalls gets all the calls that were made to GetTotalMemoryMB.
// Check the length with:
//
//	len(mockedDevice.GetTotalMemoryMBCalls())
func (mock *DeviceMock) GetTotalMemoryMBCalls() []struct {
} {
	var calls []struct {
	}
	mock.lockGetTotalMemoryMB.RLock()
	calls = mock.calls.GetTotalMemoryMB
	mock.lockGetTotalMemoryMB.RUnlock()
	return calls
}

// IsFabricAttached calls IsFabricAttachedFunc.
func (mock *DeviceMock) IsFabricAttached() (bool, error) {
	if mock.IsFabricAttachedFunc == nil {
		panic("DeviceMock.IsFabricAttachedFunc: method is nil but Device.IsFabricAttached was just called")
	}
	callInfo := struct {
	}{}
	mock.lockIsFabricAttached.Lock()
	mock.calls.IsFabricAttached = append(mock.calls.IsFabricAttached, callInfo)
	mock.lockIsFabricAttached.Unlock()
	return mock.IsFabricAttachedFunc()
}

// IsFabricAttachedCalls gets all the calls that were made to IsFabricAttached.
// Check the length with:
//
//	len(mockedDevice.IsFabricAttachedCalls())
func (mock *DeviceMock) IsFabricAttachedCalls() []struct {
} {
	var calls []struct {
	}
	mock.lockIsFabricAttached.RLock()
	calls = mock.calls.IsFabricAttached
	mock.lockIsFabricAttached.RUnlock()
	return calls
}

// IsMigCapable calls IsMigCapableFunc.
func (mock *DeviceMock) IsMigCapable() (bool, error) {
	if mock.IsMigCapableFunc == nil {
		panic("DeviceMock.IsMigCapableFunc: method is nil but Device.IsMigCapable was just called")
	}
	callInfo := struct {
	}{}
	mock.lockIsMigCapable.Lock()
	mock.calls.IsMigCapable = append(mock.calls.IsMigCapable, callInfo)
	mock.lockIsMigCapable.Unlock()
	return mock.IsMigCapableFunc()
}

// IsMigCapableCalls gets all the calls that were made to IsMigCapable.
// Check the length with:
//
//	len(mockedDevice.IsMigCapableCalls())
func (mock *DeviceMock) IsMigCapableCalls() []struct {
} {
	var calls []struct {
	}
	mock.lockIsMigCapable.RLock()
	calls = mock.calls.IsMigCapable
	mock.lockIsMigCapable.RUnlock()
	return calls
}

// IsMigEnabled calls IsMigEnabledFunc.
func (mock *DeviceMock) IsMigEnabled() (bool, error) {
	if mock.IsMigEnabledFunc == nil {
		panic("DeviceMock.IsMigEnabledFunc: method is nil but Device.IsMigEnabled was just called")
	}
	callInfo := struct {
	}{}
	mock.lockIsMigEnabled.Lock()
	mock.calls.IsMigEnabled = append(mock.calls.IsMigEnabled, callInfo)
	mock.lockIsMigEnabled.Unlock()
	return mock.IsMigEnabledFunc()
}

// IsMigEnabledCalls gets all the calls that were made to IsMigEnabled.
// Check the length with:
//
//	len(mockedDevice.IsMigEnabledCalls())
func (mock *DeviceMock) IsMigEnabledCalls() []struct {
} {
	var calls []struct {
	}
	mock.lockIsMigEnabled.RLock()
	calls = mock.calls.IsMigEnabled
	mock.lockIsMigEnabled.RUnlock()
	return calls
}
