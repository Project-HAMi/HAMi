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
)

// 工厂类（单例模式）.
type KubeClientFactory struct {
	client KubeInterface
}

var (
	instance    *KubeClientFactory
	factoryOnce sync.Once
)

// GetInstance 直接获取Kubernetes客户端实例.
func GetInstance() KubeInterface {
	return GetFactory().GetClient()
}

// GetFactory 获取单例工厂对象.
func GetFactory() *KubeClientFactory {
	factoryOnce.Do(func() {
		instance = &KubeClientFactory{
			client: NewRealClient(), // 默认使用 RealClient
		}
	})
	return instance
}

// GetClient 获取当前 client.
func (f *KubeClientFactory) GetClient() KubeInterface {
	return f.client
}

// SetFake 将工厂客户端设置为FakeClient.
func (f *KubeClientFactory) SetFake() *KubeClientFactory {
	f.client = NewFakeClient()
	return f
}

// SetReal 将工厂客户端设置为RealClient.
func (f *KubeClientFactory) SetReal() *KubeClientFactory {
	f.client = NewRealClient()
	return f
}
