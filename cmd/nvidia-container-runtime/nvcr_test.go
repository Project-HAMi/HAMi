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
	"fmt"
	"os"
	"strings"
	"testing"

	"4pd.io/k8s-vgpu/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	testlog "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
)

func TestArgsGetConfigFilePath(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)

	testCases := []struct {
		bundleDir   string
		ociSpecPath string
	}{
		{
			ociSpecPath: fmt.Sprintf("%v/config.json", wd),
		},
		{
			bundleDir:   "/foo/bar",
			ociSpecPath: "/foo/bar/config.json",
		},
		{
			bundleDir:   "/foo/bar/",
			ociSpecPath: "/foo/bar/config.json",
		},
	}

	for i, tc := range testCases {
		cp, err := getOCISpecFilePath(tc.bundleDir)

		require.NoErrorf(t, err, "%d: %v", i, tc)
		require.Equalf(t, tc.ociSpecPath, cp, "%d: %v", i, tc)
	}
}

func TestAddNvidiaHook(t *testing.T) {
	logger, logHook := testlog.NewNullLogger()
	shim := nvidiaContainerRuntime{
		logger: logger,
	}

	testCases := []struct {
		spec         *specs.Spec
		errorPrefix  string
		shouldNotAdd bool
	}{
		{
			spec: &specs.Spec{},
		},
		{
			spec: &specs.Spec{
				Hooks: &specs.Hooks{},
			},
		},
		{
			spec: &specs.Spec{
				Hooks: &specs.Hooks{
					Prestart: []specs.Hook{{
						Path: "some-hook",
					}},
				},
			},
		},
		{
			spec: &specs.Spec{
				Hooks: &specs.Hooks{
					Prestart: []specs.Hook{{
						Path: "nvidia-container-runtime-hook",
					}},
				},
			},
			shouldNotAdd: true,
		},
	}

	for i, tc := range testCases {
		logHook.Reset()

		var numPrestartHooks int
		if tc.spec.Hooks != nil {
			numPrestartHooks = len(tc.spec.Hooks.Prestart)
		}

		err := shim.addNVIDIAHook(tc.spec)

		if tc.errorPrefix == "" {
			require.NoErrorf(t, err, "%d: %v", i, tc)
		} else {
			require.Truef(t, strings.HasPrefix(err.Error(), tc.errorPrefix), "%d: %v", i, tc)

			require.NotNilf(t, tc.spec.Hooks, "%d: %v", i, tc)
			require.Equalf(t, 1, nvidiaHookCount(tc.spec.Hooks), "%d: %v", i, tc)

			if tc.shouldNotAdd {
				require.Equal(t, numPrestartHooks+1, len(tc.spec.Hooks.Poststart), "%d: %v", i, tc)
			} else {
				require.Equal(t, numPrestartHooks+1, len(tc.spec.Hooks.Poststart), "%d: %v", i, tc)

				nvidiaHook := tc.spec.Hooks.Poststart[len(tc.spec.Hooks.Poststart)-1]

				// TODO: This assumes that the hook has been set up in the makefile
				expectedPath := "/usr/bin/nvidia-container-runtime-hook"
				require.Equalf(t, expectedPath, nvidiaHook.Path, "%d: %v", i, tc)
				require.Equalf(t, []string{expectedPath, "prestart"}, nvidiaHook.Args, "%d: %v", i, tc)
				require.Emptyf(t, nvidiaHook.Env, "%d: %v", i, tc)
				require.Nilf(t, nvidiaHook.Timeout, "%d: %v", i, tc)
			}
		}
	}
}

func TestNvidiaContainerRuntime(t *testing.T) {
	logger, hook := testlog.NewNullLogger()

	testCases := []struct {
		shim         nvidiaContainerRuntime
		shouldModify bool
		args         []string
		modifyError  error
		writeError   error
	}{
		{
			shim:         nvidiaContainerRuntime{},
			shouldModify: false,
		},
		{
			shim:         nvidiaContainerRuntime{},
			args:         []string{"create"},
			shouldModify: true,
		},
		{
			shim:         nvidiaContainerRuntime{},
			args:         []string{"--bundle=create"},
			shouldModify: false,
		},
		{
			shim:         nvidiaContainerRuntime{},
			args:         []string{"--bundle", "create"},
			shouldModify: false,
		},
		{
			shim:         nvidiaContainerRuntime{},
			args:         []string{"create"},
			shouldModify: true,
		},
		{
			shim:         nvidiaContainerRuntime{},
			args:         []string{"create"},
			modifyError:  fmt.Errorf("error modifying"),
			shouldModify: true,
		},
		{
			shim:         nvidiaContainerRuntime{},
			args:         []string{"create"},
			writeError:   fmt.Errorf("error writing"),
			shouldModify: true,
		},
	}

	for i, tc := range testCases {
		tc.shim.logger = logger
		hook.Reset()

		spec := &specs.Spec{}
		ociMock := oci.NewMockSpec(spec, tc.writeError, tc.modifyError)

		require.Equal(t, tc.shouldModify, tc.shim.modificationRequired(tc.args), "%d: %v", i, tc)

		tc.shim.ociSpec = ociMock
		tc.shim.runtime = &MockShim{}

		err := tc.shim.Exec(tc.args)
		if tc.modifyError != nil || tc.writeError != nil {
			require.Error(t, err, "%d: %v", i, tc)
		} else {
			require.NoError(t, err, "%d: %v", i, tc)
		}

		if tc.shouldModify {
			require.Equal(t, 1, ociMock.MockModify.Callcount, "%d: %v", i, tc)
			require.Equal(t, 1, nvidiaHookCount(spec.Hooks), "%d: %v", i, tc)
		} else {
			require.Equal(t, 0, ociMock.MockModify.Callcount, "%d: %v", i, tc)
			require.Nil(t, spec.Hooks, "%d: %v", i, tc)
		}

		writeExpected := tc.shouldModify && tc.modifyError == nil
		if writeExpected {
			require.Equal(t, 1, ociMock.MockFlush.Callcount, "%d: %v", i, tc)
		} else {
			require.Equal(t, 0, ociMock.MockFlush.Callcount, "%d: %v", i, tc)
		}
	}
}

type MockShim struct {
	called      bool
	args        []string
	returnError error
}

func (m *MockShim) Exec(args []string) error {
	m.called = true
	m.args = args
	return m.returnError
}
