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

package cdi

import (
	"k8s.io/klog/v2"
)

type null struct{}

var _ Interface = &null{}

// NewNullHandler returns an instance of the 'cdi' interface that can
// be used when CDI specs are not required.
func NewNullHandler() Interface {
	return &null{}
}

// CreateSpecFile is a no-op for the null handler.
func (n *null) CreateSpecFile() error {
	return nil
}

// QualifiedName is a no-op for the null handler. A error message is logged
// inidicating this should never be called for the null handler.
func (n *null) QualifiedName(class string, id string) string {
	klog.Error("cannot return a qualified CDI device name with the null CDI handler")
	return ""
}
