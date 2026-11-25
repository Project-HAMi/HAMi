//go:build linux && cgo

// Copyright 2026 The HAMi Authors.
// SPDX-License-Identifier: Apache-2.0
package cudainfo

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>
typedef int (*init_fn)(unsigned);
typedef int (*count_fn)(int*);
typedef int (*uuid_fn)(char*, int);
typedef int (*attr_fn)(int*, int, int);
// Device attributes only: never create a CUDA context or launch GPU work.
static int geometry(char* uuid, int* sms, int* major, int* integrated, int index) {
 void* lib = dlopen("libcuda.so.1", RTLD_NOW | RTLD_LOCAL);
 if (!lib) return -1;
 init_fn init = (init_fn)dlsym(lib,"cuInit");
 count_fn count = (count_fn)dlsym(lib,"cuDeviceGetCount");
 uuid_fn getuuid = (uuid_fn)dlsym(lib,"cuDeviceGetUuid");
 attr_fn attr = (attr_fn)dlsym(lib,"cuDeviceGetAttribute");
 int rc=-2, n=0;
 if (!init || !count || !getuuid || !attr) goto done;
 if ((rc=init(0)) || (rc=count(&n))) goto done;
 if (index >= n) { rc=-3; goto done; }
 if ((rc=getuuid(uuid,index)) || (rc=attr(sms,16,index)) ||
     (rc=attr(major,75,index)) || (rc=attr(integrated,18,index))) goto done;
 done: dlclose(lib); return rc;
}
*/
import "C"

import (
	"fmt"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
)

func Geometry() (map[string]nvidia.MPSV3Geometry, error) {
	result := make(map[string]nvidia.MPSV3Geometry)
	for i := 0; ; i++ {
		var uuid [16]C.char
		var sms, major, integrated C.int
		rc := C.geometry(&uuid[0], &sms, &major, &integrated, C.int(i))
		if rc == -3 {
			break
		}
		if rc != 0 {
			return nil, fmt.Errorf("query CUDA device geometry: code %d", rc)
		}
		bytes := make([]byte, 16)
		for j := range bytes {
			bytes[j] = byte(uuid[j])
		}
		id := fmt.Sprintf("GPU-%x-%x-%x-%x-%x", bytes[:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:])
		chunk := int64(0)
		if major >= 8 {
			chunk = 2
			if major >= 9 && integrated == 0 {
				chunk = 8
			}
		}
		result[id] = nvidia.MPSV3Geometry{SMs: int64(sms), Chunk: chunk}
	}
	return result, nil
}
