/*
# Copyright (c) 2021, NVIDIA CORPORATION.  All rights reserved.
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
*/

package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"4pd.io/k8s-vgpu/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	log "github.com/sirupsen/logrus"
)

// nvidiaContainerRuntime encapsulates the NVIDIA Container Runtime. It wraps the specified runtime, conditionally
// modifying the specified OCI specification before invoking the runtime.
type nvidiaContainerRuntime struct {
	logger  *log.Logger
	runtime oci.Runtime
	ociSpec oci.Spec
}

var _ oci.Runtime = (*nvidiaContainerRuntime)(nil)

// newNvidiaContainerRuntime is a constructor for a standard runtime shim.
func newNvidiaContainerRuntimeWithLogger(logger *log.Logger, runtime oci.Runtime, ociSpec oci.Spec) (oci.Runtime, error) {
	r := nvidiaContainerRuntime{
		logger:  logger,
		runtime: runtime,
		ociSpec: ociSpec,
	}

	return &r, nil
}

// Exec defines the entrypoint for the NVIDIA Container Runtime. A check is performed to see whether modifications
// to the OCI spec are required -- and applicable modifcations applied. The supplied arguments are then
// forwarded to the underlying runtime's Exec method.
func (r nvidiaContainerRuntime) Exec(args []string) error {
	if r.modificationRequired(args) {
		//		fmt.Println("NEED modification")
		err := r.modifyOCISpec()
		if err != nil {
			return fmt.Errorf("error modifying OCI spec: %v", err)
		}
	} else {
		//fmt.Println("Need not modification")
	}

	r.logger.Println("Forwarding command to runtime")
	return r.runtime.Exec(args)
}

// modificationRequired checks the intput arguments to determine whether a modification
// to the OCI spec is required.
func (r nvidiaContainerRuntime) modificationRequired(args []string) bool {
	var previousWasBundle bool
	for _, a := range args {
		// We check for '--bundle create' explicitly to ensure that we
		// don't inadvertently trigger a modification if the bundle directory
		// is specified as `create`
		if !previousWasBundle && isBundleFlag(a) {
			previousWasBundle = true
			continue
		}

		if !previousWasBundle && a == "create" {
			r.logger.Infof("'create' command detected; modification required")
			return true
		}

		previousWasBundle = false
	}

	r.logger.Infof("No modification required")
	return false
}

// modifyOCISpec loads and modifies the OCI spec specified in the nvidiaContainerRuntime
// struct. The spec is modified in-place and written to the same file as the input after
// modifcationas are applied.
func (r nvidiaContainerRuntime) modifyOCISpec() error {
	err := r.ociSpec.Load()
	if err != nil {
		return fmt.Errorf("error loading OCI specification for modification: %v", err)
	}

	err = r.ociSpec.Modify(r.addNVIDIAHook)
	if err != nil {
		r.logger.Println("!!!!!!!!!!spec Modify failed", err.Error())
		return fmt.Errorf("error injecting NVIDIA Container Runtime hook: %v", err)
	}

	err = r.ociSpec.Flush()
	if err != nil {
		r.logger.Println("!!!!!!!!!!spec Modify failed", err.Error())
		return fmt.Errorf("error writing modified OCI specification: %v", err)
	}
	return nil
}

const SharedPath = "/tmp/vgpu/containers/"

func (r nvidiaContainerRuntime) addMonitor(ctrmsg []string, spec *specs.Spec) error {
	if len(ctrmsg) == 0 {
		return errors.New("ctrmsg not matched")
	}
	os.MkdirAll(SharedPath, os.ModePerm)
	currentbundle, _ := os.Getwd()
	currentbundle = currentbundle + "/vgpucache/"
	os.MkdirAll(currentbundle, os.ModePerm)
	vgpupath := SharedPath + ctrmsg[0]
	os.Remove(vgpupath)
	err := os.Symlink(currentbundle, vgpupath)
	if err != nil {
		return errors.New("symbolic symbol creation failed")
	}
	dpath := SharedPath
	os.MkdirAll(dpath, os.ModePerm)
	sharedmnt := specs.Mount{
		Destination: "/tmp/vgpu/",
		Source:      currentbundle,
		Type:        "bind",
		Options:     []string{"rbind", "rw"},
	}
	spec.Mounts = append(spec.Mounts, sharedmnt)
	r.logger.Println("mounts=", spec.Mounts)
	dirname, _ := os.Getwd()
	r.logger.Println("pwd=", dirname)
	return nil
}

func appendtofilestr(idx string, val string, res string) string {
	tmp := res + idx + "=" + val + "\n"
	return tmp
}

// addNVIDIAHook modifies the specified OCI specification in-place, inserting a
// prestart hook.
func (r nvidiaContainerRuntime) addNVIDIAHook(spec *specs.Spec) error {
	path, err := exec.LookPath("nvidia-container-runtime-hook")
	if err != nil {
		path = hookDefaultFilePath
		_, err = os.Stat(path)
		if err != nil {
			return err
		}
	}

	r.logger.Printf("prestart hook path: %s %s\n", path)
	envmap, newuuids, err := GetNvidiaUUID(r, spec.Process.Env)
	if err != nil {
		r.logger.Println("GetNvidiaUUID failed")
	} else {
		if len(envmap) > 0 {
			restr := ""
			for idx, val := range envmap {
				restr = appendtofilestr(idx, val, restr)

				tmp1 := idx + "=" + val
				found := false
				for idx1, val1 := range spec.Process.Env {
					if strings.Compare(strings.Split(val1, "=")[0], idx) == 0 {
						spec.Process.Env[idx1] = tmp1
						found = true
						r.logger.Println("modified env", tmp1)
						continue
					}
				}
				if !found {
					spec.Process.Env = append(spec.Process.Env, tmp1)
					r.logger.Println("appended env", tmp1)
				}
			}
			restr = appendtofilestr("CUDA_DEVICE_MEMORY_SHARED_CACHE", "/tmp/vgpu/cudevshr.cache", restr)
			ioutil.WriteFile("envfile.vgpu", []byte(restr), os.ModePerm)
			dir, _ := os.Getwd()
			sharedmnt := specs.Mount{
				Destination: "/tmp/envfile.vgpu",
				Source:      dir + "/envfile.vgpu",
				Type:        "bind",
				Options:     []string{"rbind", "rw"},
			}
			spec.Mounts = append(spec.Mounts, sharedmnt)

			//spec.Mounts = append(spec.Mounts, )
		}
		if len(newuuids) > 0 {
			//r.logger.Println("Get new uuids", newuuids)
			//spec.Process.Env = append(spec.Process.Env, newuuids[0])
			err1 := r.addMonitor(newuuids, spec)
			if err1 != nil {
				r.logger.Println("addMonitorPath failed", err1.Error())
			}
		}
	}
	args := []string{path}
	if spec.Hooks == nil {
		spec.Hooks = &specs.Hooks{}
	} else if len(spec.Hooks.Prestart) != 0 {
		for _, hook := range spec.Hooks.Prestart {
			if !strings.Contains(hook.Path, "nvidia-container-runtime-hook") {
				continue
			}
			r.logger.Println("existing nvidia prestart hook in OCI spec file")
			return nil
		}
	}

	spec.Hooks.Prestart = append(spec.Hooks.Prestart, specs.Hook{
		Path: path,
		Args: append(args, "prestart"),
	})

	r.logger.Println("newEnvs=", spec.Process.Env)
	return nil
}
