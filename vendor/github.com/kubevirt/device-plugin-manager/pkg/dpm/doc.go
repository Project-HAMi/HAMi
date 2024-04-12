// Package dpm (Device Plugin Manager) provides a framework that makes implementation of
// Device Plugins https://kubernetes.io/docs/concepts/cluster-administration/device-plugins/
// easier. It provides abstraction of Plugins, thanks to it a user does not need to implement
// actual gRPC server. It also handles dynamic management of available resources and their
// respective plugins.
//
// Usage
//
// The framework contains two main interfaces which must be implemented by user. ListerInterface
// handles resource management, it notifies DPM about available resources. Plugin interface then
// represents a plugin that handles available devices of one resource.
//
// See Also
//
// Repository of this package and some plugins using it can be found on
// https://github.com/kubevirt/kubernetes-device-plugins/.
package dpm
