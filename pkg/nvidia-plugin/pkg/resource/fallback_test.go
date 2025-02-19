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

package resource

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFallback(t *testing.T) {
	testCases := []struct {
		initError     error
		shutdownError error
	}{
		{
			initError: fmt.Errorf("init failed"),
		},
		{
			shutdownError: fmt.Errorf("should not be called"),
		},
	}

	for _, tc := range testCases {

		m := &ManagerMock{
			InitFunc: func() error {
				return tc.initError
			},
			ShutdownFunc: func() error {
				return tc.shutdownError
			},
		}

		f := NewFallbackToNullOnInitError(m)

		require.NoError(t, f.Init())

		err := f.Shutdown()
		if tc.shutdownError == nil {
			require.NoError(t, err)
		} else {
			require.EqualError(t, err, tc.shutdownError.Error())
		}

	}
}
