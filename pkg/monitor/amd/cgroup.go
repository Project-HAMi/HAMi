/*
Copyright 2026 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package amd reads per-container AMD GPU memory from the dmem cgroup
// controller, the same kernel counter the AMD device plugin caps VRAM with.
package amd

import (
	"bufio"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ContainerVRAM is the VRAM one container holds on one device region.
type ContainerVRAM struct {
	PodUID      string
	ContainerID string
	// Region is the dmem region name, for example "drm/0000:06:00.0/vram".
	Region string
	// Used is dmem.current. Limit is dmem.max, or 0 when the container is uncapped.
	Used, Limit uint64
}

// kubelet's systemd cgroup driver names a pod slice kubepods[-qos]-pod<uid>.slice
// with the dashes of the UID turned into underscores.
var podSlice = regexp.MustCompile(`^kubepods(?:-[a-z]+)?-pod([0-9a-f_]+)\.slice$`)

var runtimePrefixes = []string{"cri-containerd-", "crio-", "docker-", "cri-dockerd-"}

// ReadContainerVRAM returns the dmem usage of every container scope under
// <cgroupRoot>/kubepods.slice. A kernel without the dmem controller, or a node
// that does not use the systemd cgroup driver, yields no entries and no error.
func ReadContainerVRAM(cgroupRoot string) ([]ContainerVRAM, error) {
	root := filepath.Join(cgroupRoot, "kubepods.slice")
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []ContainerVRAM
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A container exiting mid-walk removes its directory.
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !d.IsDir() {
			return nil
		}
		id, ok := containerID(d.Name())
		if !ok {
			return nil
		}
		uid, ok := podUID(filepath.Dir(path))
		if !ok {
			return fs.SkipDir
		}
		used, err := readDmem(filepath.Join(path, "dmem.current"))
		if err != nil {
			if os.IsNotExist(err) {
				return fs.SkipDir
			}
			return err
		}
		limits, _ := readDmem(filepath.Join(path, "dmem.max"))
		for region, bytes := range used {
			c := ContainerVRAM{PodUID: uid, ContainerID: id, Region: region, Used: bytes}
			if l := limits[region]; l != math.MaxUint64 {
				c.Limit = l
			}
			out = append(out, c)
		}
		return fs.SkipDir
	})
	return out, err
}

func containerID(name string) (string, bool) {
	if !strings.HasSuffix(name, ".scope") {
		return "", false
	}
	for _, p := range runtimePrefixes {
		if id, ok := strings.CutPrefix(name, p); ok {
			return strings.TrimSuffix(id, ".scope"), true
		}
	}
	return "", false
}

func podUID(dir string) (string, bool) {
	m := podSlice.FindStringSubmatch(filepath.Base(dir))
	if m == nil {
		return "", false
	}
	return strings.ReplaceAll(m[1], "_", "-"), true
}

// readDmem parses a dmem.current or dmem.max file: one "<region> <bytes|max>"
// line per region. "max" is reported as math.MaxUint64.
func readDmem(path string) (map[string]uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		region, value, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if !ok {
			continue
		}
		if value == "max" {
			out[region] = math.MaxUint64
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		if err != nil {
			continue
		}
		out[region] = n
	}
	return out, sc.Err()
}
