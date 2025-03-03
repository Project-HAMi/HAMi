/*
Copyright 2024 The HAMi Authors.

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

package client

import (
	"sync"

	"k8s.io/client-go/kubernetes/fake"
)

// KubeClientFactory is a factory class (singleton pattern).
type KubeClientFactory struct {
	client KubeInterface
}

var factoryOnce sync.Once

// GetFactory gets the singleton factory object.
// Uses closure to maintain instance as a local variable.
var GetFactory = (func() func() *KubeClientFactory {
	var instance *KubeClientFactory

	return func() *KubeClientFactory {
		factoryOnce.Do(func() {
			instance = &KubeClientFactory{}
			instance.SetReal() // Use the real client by default.
		})
		return instance
	}
})()

// NewInstance directly gets the Kubernetes client instance.
func NewInstance() KubeInterface {
	return GetFactory().GetClient()
}

func (f *KubeClientFactory) GetClient() KubeInterface {
	//TODO: For compatibility with other direct "KubeClient" assignment call points, this line needs to be removed after replacement.
	if KubeClient == nil {
		f.client = &K8sClient{
			client: GetK8sClient().client,
		}
		KubeClient = GetK8sClient().client
	} else {
		f.client = &K8sClient{
			client: KubeClient,
		}
	}
	return f.client
}

func (f *KubeClientFactory) SetFake() *KubeClientFactory {
	f.client = &K8sClient{
		client: fake.NewSimpleClientset(),
	}
	//TODO: For compatibility with other direct "KubeClient" assignment call points, this line needs to be removed after replacement.
	KubeClient = fake.NewSimpleClientset()
	return f
}

func (f *KubeClientFactory) SetReal() *KubeClientFactory {
	//TODO: For compatibility with other direct "KubeClient" assignment call points, this line needs to be removed after replacement.
	if KubeClient == nil {
		f.client = &K8sClient{
			client: GetK8sClient().client,
		}
		KubeClient = GetK8sClient().client
	} else {
		f.client = &K8sClient{
			client: KubeClient,
		}
	}
	return f
}
