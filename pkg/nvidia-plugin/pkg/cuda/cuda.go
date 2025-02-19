/**
# Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package cuda

import (
	"unsafe"
)

/*
#cgo linux LDFLAGS: -Wl,--export-dynamic -Wl,--unresolved-symbols=ignore-in-object-files
#cgo darwin LDFLAGS: -Wl,-undefined,dynamic_lookup

#ifdef _WIN32
#define CUDAAPI __stdcall
#else
#define CUDAAPI
#endif

typedef int CUdevice;

typedef enum CUdevice_attribute_enum {
    CU_DEVICE_ATTRIBUTE_COMPUTE_CAPABILITY_MAJOR = 75,
    CU_DEVICE_ATTRIBUTE_COMPUTE_CAPABILITY_MINOR = 76
} CUdevice_attribute;

typedef enum cudaError_enum {
    CUDA_SUCCESS                              = 0,
    CUDA_ERROR_INVALID_VALUE                  = 1,
    CUDA_ERROR_OUT_OF_MEMORY                  = 2,
    CUDA_ERROR_NOT_INITIALIZED                = 3,
    CUDA_ERROR_DEINITIALIZED                  = 4,
    CUDA_ERROR_PROFILER_DISABLED              = 5,
    CUDA_ERROR_PROFILER_NOT_INITIALIZED       = 6,
    CUDA_ERROR_PROFILER_ALREADY_STARTED       = 7,
    CUDA_ERROR_PROFILER_ALREADY_STOPPED       = 8,
    CUDA_ERROR_NO_DEVICE                      = 100,
    CUDA_ERROR_INVALID_DEVICE                 = 101,
    CUDA_ERROR_INVALID_IMAGE                  = 200,
    CUDA_ERROR_INVALID_CONTEXT                = 201,
    CUDA_ERROR_CONTEXT_ALREADY_CURRENT        = 202,
    CUDA_ERROR_MAP_FAILED                     = 205,
    CUDA_ERROR_UNMAP_FAILED                   = 206,
    CUDA_ERROR_ARRAY_IS_MAPPED                = 207,
    CUDA_ERROR_ALREADY_MAPPED                 = 208,
    CUDA_ERROR_NO_BINARY_FOR_GPU              = 209,
    CUDA_ERROR_ALREADY_ACQUIRED               = 210,
    CUDA_ERROR_NOT_MAPPED                     = 211,
    CUDA_ERROR_NOT_MAPPED_AS_ARRAY            = 212,
    CUDA_ERROR_NOT_MAPPED_AS_POINTER          = 213,
    CUDA_ERROR_ECC_UNCORRECTABLE              = 214,
    CUDA_ERROR_UNSUPPORTED_LIMIT              = 215,
    CUDA_ERROR_CONTEXT_ALREADY_IN_USE         = 216,
    CUDA_ERROR_PEER_ACCESS_UNSUPPORTED        = 217,
    CUDA_ERROR_INVALID_PTX                    = 218,
    CUDA_ERROR_INVALID_GRAPHICS_CONTEXT       = 219,
    CUDA_ERROR_NVLINK_UNCORRECTABLE           = 220,
    CUDA_ERROR_JIT_COMPILER_NOT_FOUND         = 221,
    CUDA_ERROR_INVALID_SOURCE                 = 300,
    CUDA_ERROR_FILE_NOT_FOUND                 = 301,
    CUDA_ERROR_SHARED_OBJECT_SYMBOL_NOT_FOUND = 302,
    CUDA_ERROR_SHARED_OBJECT_INIT_FAILED      = 303,
    CUDA_ERROR_OPERATING_SYSTEM               = 304,
    CUDA_ERROR_INVALID_HANDLE                 = 400,
    CUDA_ERROR_NOT_FOUND                      = 500,
    CUDA_ERROR_NOT_READY                      = 600,
    CUDA_ERROR_ILLEGAL_ADDRESS                = 700,
    CUDA_ERROR_LAUNCH_OUT_OF_RESOURCES        = 701,
    CUDA_ERROR_LAUNCH_TIMEOUT                 = 702,
    CUDA_ERROR_LAUNCH_INCOMPATIBLE_TEXTURING  = 703,
    CUDA_ERROR_PEER_ACCESS_ALREADY_ENABLED    = 704,
    CUDA_ERROR_PEER_ACCESS_NOT_ENABLED        = 705,
    CUDA_ERROR_PRIMARY_CONTEXT_ACTIVE         = 708,
    CUDA_ERROR_CONTEXT_IS_DESTROYED           = 709,
    CUDA_ERROR_ASSERT                         = 710,
    CUDA_ERROR_TOO_MANY_PEERS                 = 711,
    CUDA_ERROR_HOST_MEMORY_ALREADY_REGISTERED = 712,
    CUDA_ERROR_HOST_MEMORY_NOT_REGISTERED     = 713,
    CUDA_ERROR_HARDWARE_STACK_ERROR           = 714,
    CUDA_ERROR_ILLEGAL_INSTRUCTION            = 715,
    CUDA_ERROR_MISALIGNED_ADDRESS             = 716,
    CUDA_ERROR_INVALID_ADDRESS_SPACE          = 717,
    CUDA_ERROR_INVALID_PC                     = 718,
    CUDA_ERROR_LAUNCH_FAILED                  = 719,
    CUDA_ERROR_COOPERATIVE_LAUNCH_TOO_LARGE   = 720,
    CUDA_ERROR_NOT_PERMITTED                  = 800,
    CUDA_ERROR_NOT_SUPPORTED                  = 801,
    CUDA_ERROR_UNKNOWN                        = 999
} CUresult;

CUresult CUDAAPI cuInit(unsigned int Flags);
CUresult CUDAAPI cuDriverGetVersion(int *driverVersion);
CUresult CUDAAPI cuDeviceGet(CUdevice *device, int ordinal);
CUresult CUDAAPI cuDeviceGetAttribute(int *pi, CUdevice_attribute attrib, CUdevice dev);
CUresult CUDAAPI cuDeviceGetCount(int *count);
CUresult CUDAAPI cuDeviceTotalMem(size_t *bytes, CUdevice dev);
CUresult CUDAAPI cuDeviceGetName(char *name, int len, CUdevice dev);
*/
import "C"

// cuInit function as declared in cuda.h
func cuInit(flags uint32) Result {
	cFlags := (C.uint)(flags)
	_ret := C.cuInit(cFlags)

	return Result(_ret)
}

// cuDeviceGet function as declared in cuda.h
func cuDeviceGet(device *Device, index int32) Result {
	cDevice := (*C.CUdevice)(unsafe.Pointer(device))
	cIndex := (C.int)(index)

	_ret := C.cuDeviceGet(cDevice, cIndex)

	return Result(_ret)
}

// cuDeviceGetAttribute function as declared in cuda.h
func cuDeviceGetAttribute(value *int32, attribute DeviceAttribute, dev Device) Result {
	cValue := (*C.int)(unsafe.Pointer(value))
	cAttribute := (C.CUdevice_attribute)(attribute)
	cDev := (C.CUdevice)(dev)

	_ret := C.cuDeviceGetAttribute(cValue, cAttribute, cDev)

	return Result(_ret)
}

// cuDeviceGetCount function as declared in cuda.h
func cuDeviceGetCount(count *int32) Result {
	cCount := (*C.int)(unsafe.Pointer(count))
	_ret := C.cuDeviceGetCount(cCount)

	return Result(_ret)
}

// cuDriverGetVersion function as declared in cuda.h
func cuDriverGetVersion(version *int32) Result {
	cVersion := (*C.int)(version)
	_ret := C.cuDriverGetVersion(cVersion)

	return Result(_ret)
}

// cuDeviceTotalMem function as declared in cuda.h
func cuDeviceTotalMem(bytes *uint64, dev Device) Result {
	cBytes := (*C.size_t)(unsafe.Pointer(bytes))
	cDev := (C.CUdevice)(dev)
	_ret := C.cuDeviceTotalMem(cBytes, cDev)

	return Result(_ret)
}

// cuDeviceGetName function as declared in cuda.h
func cuDeviceGetName(name *byte, len int32, dev Device) Result {
	cName := (*C.char)(unsafe.Pointer(name))
	cLen := (C.int)(len)
	cDev := (C.CUdevice)(dev)
	_ret := C.cuDeviceGetName(cName, cLen, cDev)

	return Result(_ret)
}
