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
package oci

import (
	"fmt"
	"strings"
	"testing"

	testlog "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
)

func TestSyscallExecConstructor(t *testing.T) {
	r, err := NewSyscallExecRuntime("////an/invalid/path")
	require.Error(t, err)
	require.Nil(t, r)

	r, err = NewSyscallExecRuntime("/tmp")
	require.Error(t, err)
	require.Nil(t, r)

	r, err = NewSyscallExecRuntime("/dev/null")
	require.Error(t, err)
	require.Nil(t, r)

	r, err = NewSyscallExecRuntime("/bin/sh")
	require.NoError(t, err)

	f, ok := r.(*SyscallExecRuntime)
	require.True(t, ok)

	require.Equal(t, "/bin/sh", f.path)
}

func TestSyscallExecForwardsArgs(t *testing.T) {
	logger, _ := testlog.NewNullLogger()
	f := SyscallExecRuntime{
		logger: logger,
		path:   "runtime",
	}

	testCases := []struct {
		returnError error
		args        []string
		errorPrefix string
	}{
		{
			returnError: nil,
			errorPrefix: "unexpected return from exec",
		},
		{
			returnError: fmt.Errorf("error from exec"),
			errorPrefix: "could not exec",
		},
		{
			returnError: nil,
			args:        []string{"otherargv0"},
			errorPrefix: "unexpected return from exec",
		},
		{
			returnError: nil,
			args:        []string{"otherargv0", "arg1", "arg2", "arg3"},
			errorPrefix: "unexpected return from exec",
		},
	}

	for i, tc := range testCases {
		execMock := WithMockExec(f, tc.returnError)

		err := execMock.Exec(tc.args)

		require.Errorf(t, err, "%d: %v", i, tc)
		require.Truef(t, strings.HasPrefix(err.Error(), tc.errorPrefix), "%d: %v", i, tc)
		if tc.returnError != nil {
			require.Truef(t, strings.HasSuffix(err.Error(), tc.returnError.Error()), "%d: %v", i, tc)
		}

		require.Equalf(t, f.path, execMock.argv0, "%d: %v", i, tc)
		require.Equalf(t, f.path, execMock.argv[0], "%d: %v", i, tc)

		require.LessOrEqualf(t, len(tc.args), len(execMock.argv), "%d: %v", i, tc)
		if len(tc.args) > 1 {
			require.Equalf(t, tc.args[1:], execMock.argv[1:], "%d: %v", i, tc)
		}
	}
}
