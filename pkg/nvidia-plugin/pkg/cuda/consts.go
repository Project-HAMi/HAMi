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

// Result represents the CUresult return type.
type Result int32

const (
	SUCCESS                              Result = 0
	ERROR_INVALID_VALUE                  Result = 1
	ERROR_OUT_OF_MEMORY                  Result = 2
	ERROR_NOT_INITIALIZED                Result = 3
	ERROR_DEINITIALIZED                  Result = 4
	ERROR_PROFILER_DISABLED              Result = 5
	ERROR_PROFILER_NOT_INITIALIZED       Result = 6
	ERROR_PROFILER_ALREADY_STARTED       Result = 7
	ERROR_PROFILER_ALREADY_STOPPED       Result = 8
	ERROR_NO_DEVICE                      Result = 100
	ERROR_INVALID_DEVICE                 Result = 101
	ERROR_INVALID_IMAGE                  Result = 200
	ERROR_INVALID_CONTEXT                Result = 201
	ERROR_CONTEXT_ALREADY_CURRENT        Result = 202
	ERROR_MAP_FAILED                     Result = 205
	ERROR_UNMAP_FAILED                   Result = 206
	ERROR_ARRAY_IS_MAPPED                Result = 207
	ERROR_ALREADY_MAPPED                 Result = 208
	ERROR_NO_BINARY_FOR_GPU              Result = 209
	ERROR_ALREADY_ACQUIRED               Result = 210
	ERROR_NOT_MAPPED                     Result = 211
	ERROR_NOT_MAPPED_AS_ARRAY            Result = 212
	ERROR_NOT_MAPPED_AS_POINTER          Result = 213
	ERROR_ECC_UNCORRECTABLE              Result = 214
	ERROR_UNSUPPORTED_LIMIT              Result = 215
	ERROR_CONTEXT_ALREADY_IN_USE         Result = 216
	ERROR_PEER_ACCESS_UNSUPPORTED        Result = 217
	ERROR_INVALID_PTX                    Result = 218
	ERROR_INVALID_GRAPHICS_CONTEXT       Result = 219
	ERROR_NVLINK_UNCORRECTABLE           Result = 220
	ERROR_JIT_COMPILER_NOT_FOUND         Result = 221
	ERROR_INVALID_SOURCE                 Result = 300
	ERROR_FILE_NOT_FOUND                 Result = 301
	ERROR_SHARED_OBJECT_SYMBOL_NOT_FOUND Result = 302
	ERROR_SHARED_OBJECT_INIT_FAILED      Result = 303
	ERROR_OPERATING_SYSTEM               Result = 304
	ERROR_INVALID_HANDLE                 Result = 400
	ERROR_NOT_FOUND                      Result = 500
	ERROR_NOT_READY                      Result = 600
	ERROR_ILLEGAL_ADDRESS                Result = 700
	ERROR_LAUNCH_OUT_OF_RESOURCES        Result = 701
	ERROR_LAUNCH_TIMEOUT                 Result = 702
	ERROR_LAUNCH_INCOMPATIBLE_TEXTURING  Result = 703
	ERROR_PEER_ACCESS_ALREADY_ENABLED    Result = 704
	ERROR_PEER_ACCESS_NOT_ENABLED        Result = 705
	ERROR_PRIMARY_CONTEXT_ACTIVE         Result = 708
	ERROR_CONTEXT_IS_DESTROYED           Result = 709
	ERROR_ASSERT                         Result = 710
	ERROR_TOO_MANY_PEERS                 Result = 711
	ERROR_HOST_MEMORY_ALREADY_REGISTERED Result = 712
	ERROR_HOST_MEMORY_NOT_REGISTERED     Result = 713
	ERROR_HARDWARE_STACK_ERROR           Result = 714
	ERROR_ILLEGAL_INSTRUCTION            Result = 715
	ERROR_MISALIGNED_ADDRESS             Result = 716
	ERROR_INVALID_ADDRESS_SPACE          Result = 717
	ERROR_INVALID_PC                     Result = 718
	ERROR_LAUNCH_FAILED                  Result = 719
	ERROR_COOPERATIVE_LAUNCH_TOO_LARGE   Result = 720
	ERROR_NOT_PERMITTED                  Result = 800
	ERROR_NOT_SUPPORTED                  Result = 801
	ERROR_UNKNOWN                        Result = 99
)

// DeviceAttribute represents the CUdevice_attribute type
type DeviceAttribute int32

const (
	COMPUTE_CAPABILITY_MAJOR DeviceAttribute = 75
	COMPUTE_CAPABILITY_MINOR DeviceAttribute = 76
)

// Device represents a CUDA device handle
type Device int32
