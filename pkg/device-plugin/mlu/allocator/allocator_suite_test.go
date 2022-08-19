// Copyright 2021 Cambricon, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package allocator

import (
	"testing"

	"4pd.io/k8s-vgpu/pkg/device-plugin/mlu/cntopo/mock"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var (
	cntopoMock *mock.Cntopo
	mockCtrl   *gomock.Controller
)

func TestAllocator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Allocator Suite")
}

var _ = BeforeSuite(func() {
	By("Bootstrap test environment")
	mockCtrl = gomock.NewController(GinkgoT())
	cntopoMock = mock.NewCntopo(mockCtrl)
})

var _ = AfterSuite(func() {
	By("Tear down the test environment")
	mockCtrl.Finish()
})
