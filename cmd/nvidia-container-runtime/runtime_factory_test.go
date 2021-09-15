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
	"path/filepath"
	"strings"
	"testing"

	testlog "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
)

func TestConstructor(t *testing.T) {
	shim, err := newRuntime([]string{})

	require.NoError(t, err)
	require.NotNil(t, shim)
}

func TestGetBundlePath(t *testing.T) {
	type expected struct {
		bundle  string
		isError bool
	}
	testCases := []struct {
		argv     []string
		expected expected
	}{
		{
			argv: []string{},
		},
		{
			argv: []string{"create"},
		},
		{
			argv: []string{"--bundle"},
			expected: expected{
				isError: true,
			},
		},
		{
			argv: []string{"-b"},
			expected: expected{
				isError: true,
			},
		},
		{
			argv: []string{"--bundle", "/foo/bar"},
			expected: expected{
				bundle: "/foo/bar",
			},
		},
		{
			argv: []string{"--not-bundle", "/foo/bar"},
		},
		{
			argv: []string{"--"},
		},
		{
			argv: []string{"-bundle", "/foo/bar"},
			expected: expected{
				bundle: "/foo/bar",
			},
		},
		{
			argv: []string{"--bundle=/foo/bar"},
			expected: expected{
				bundle: "/foo/bar",
			},
		},
		{
			argv: []string{"-b=/foo/bar"},
			expected: expected{
				bundle: "/foo/bar",
			},
		},
		{
			argv: []string{"-b=/foo/=bar"},
			expected: expected{
				bundle: "/foo/=bar",
			},
		},
		{
			argv: []string{"-b", "/foo/bar"},
			expected: expected{
				bundle: "/foo/bar",
			},
		},
		{
			argv: []string{"create", "-b", "/foo/bar"},
			expected: expected{
				bundle: "/foo/bar",
			},
		},
		{
			argv: []string{"-b", "create", "create"},
			expected: expected{
				bundle: "create",
			},
		},
		{
			argv: []string{"-b=create", "create"},
			expected: expected{
				bundle: "create",
			},
		},
		{
			argv: []string{"-b", "create"},
			expected: expected{
				bundle: "create",
			},
		},
	}

	for i, tc := range testCases {
		bundle, err := getBundlePath(tc.argv)

		if tc.expected.isError {
			require.Errorf(t, err, "%d: %v", i, tc)
		} else {
			require.NoErrorf(t, err, "%d: %v", i, tc)
		}

		require.Equalf(t, tc.expected.bundle, bundle, "%d: %v", i, tc)
	}
}

func TestFindRunc(t *testing.T) {
	testLogger, _ := testlog.NewNullLogger()
	logger.Logger = testLogger

	runcPath, err := findRunc()
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(runcPath, "runc"))
	require.True(t, strings.HasPrefix(runcPath, cfg.root))
}

func TestFindRuntime(t *testing.T) {
	testLogger, _ := testlog.NewNullLogger()
	logger.Logger = testLogger

	testCases := []struct {
		candidates   []string
		expectedPath string
	}{
		{
			candidates: []string{},
		},
		{
			candidates: []string{"not-runc"},
		},
		{
			candidates: []string{"not-runc", "also-not-runc"},
		},
		{
			candidates:   []string{"runc"},
			expectedPath: filepath.Join(cfg.root, "runc"),
		},
		{
			candidates:   []string{"runc", "not-runc"},
			expectedPath: filepath.Join(cfg.root, "runc"),
		},
		{
			candidates:   []string{"not-runc", "runc"},
			expectedPath: filepath.Join(cfg.root, "runc"),
		},
	}

	for i, tc := range testCases {
		runcPath, err := findRuntime(tc.candidates)
		if tc.expectedPath == "" {
			require.Error(t, err, "%d: %v", i, tc)
		} else {
			require.NoError(t, err, "%d: %v", i, tc)
		}
		require.Equal(t, tc.expectedPath, runcPath, "%d: %v", i, tc)
	}

}
