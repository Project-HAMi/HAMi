/*
Copyright 2025 BaiLian.

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

package common

import (
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"time"

	"google.golang.org/grpc"
	"k8s.io/klog/v2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// BasePlugin implements the DevicePlugin interface.
type BasePlugin struct {
	ResourceName string
	SocketFile   string
	Server       *grpc.Server
	Srv          pluginapi.DevicePluginServer
	StopCh       chan struct{}
	ChangedCh    chan struct{}
}

func (p *BasePlugin) GetDevicePluginOptions(context.Context, *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{}, nil
}
func (p *BasePlugin) ListAndWatch(e *pluginapi.Empty, s pluginapi.DevicePlugin_ListAndWatchServer) error {
	return nil
}
func (p *BasePlugin) GetPreferredAllocation(context.Context, *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	return &pluginapi.PreferredAllocationResponse{}, nil
}
func (p *BasePlugin) Allocate(_ context.Context, in *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	allocations := make([]*pluginapi.ContainerAllocateResponse, len(in.ContainerRequests))
	for i := range allocations {
		allocations[i] = &pluginapi.ContainerAllocateResponse{}
	}
	return &pluginapi.AllocateResponse{
		ContainerResponses: allocations,
	}, nil
}
func (p *BasePlugin) PreStartContainer(context.Context, *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

func (p *BasePlugin) Start() error {
	if err := grpcServe(p.SocketFile, p.ResourceName, p.Server, p.Srv); err != nil {
		klog.Error(fmt.Sprintf("Could not start device plugin for '%s': %s", p.ResourceName, err))
		return err
	}
	klog.Info(fmt.Sprintf("Starting to serve '%s' on %s", p.ResourceName, p.SocketFile))

	if err := registerPlugin(p.SocketFile, p.ResourceName); err != nil {
		klog.Error("Could not register device plugin", "error", err)
		p.Stop()
		return err
	}
	klog.Info(fmt.Sprintf("Registered device plugin for '%s' with Kubelet", p.ResourceName))
	return nil
}

// Stop stops the gRPC server.
func (p *BasePlugin) Stop() error {
	if p == nil || p.Server == nil {
		return nil
	}
	klog.Info(fmt.Sprintf("Stopping to serve '%s' on %s", p.ResourceName, p.SocketFile))
	p.Server.Stop()
	if err := os.Remove(p.SocketFile); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// grpcServe starts the gRPC server of the device plugin.
func grpcServe(socketFile, resourceName string, s *grpc.Server, srv pluginapi.DevicePluginServer) error {
	os.Remove(socketFile)
	sock, err := net.Listen("unix", socketFile)
	if err != nil {
		return err
	}

	pluginapi.RegisterDevicePluginServer(s, srv)

	go func() {
		lastCrashTime := time.Now()
		restartCount := 0
		for {
			klog.Info(fmt.Sprintf("Starting GRPC server for '%s'", resourceName))
			err := s.Serve(sock)
			if err == nil {
				break
			}

			klog.Info(fmt.Sprintf("GRPC server for '%s' crashed with error: %v", resourceName, err))

			// restart if it has not been too often
			// i.e. if server has crashed more than 5 times, and it didn't last more than one hour each time
			if restartCount > 5 {
				// quit
				klog.Error(fmt.Sprintf("GRPC server for '%s' has repeatedly crashed recently. Quitting", resourceName))
				os.Exit(1)
			}
			timeSinceLastCrash := time.Since(lastCrashTime).Seconds()
			lastCrashTime = time.Now()
			if timeSinceLastCrash > 3600 {
				// it has been one hour since the last crash. reset the count
				// to reflect on the frequency
				restartCount = 1
			} else {
				restartCount++
			}
		}
	}()

	// Wait for server to start by launching a blocking connexion
	conn, err := dialKubelet(socketFile, 5*time.Second)
	if err != nil {
		return err
	}
	conn.Close()

	return nil
}

// registerPlugin registers the device plugin for the given resourceName with Kubelet.
func registerPlugin(socketFile, resourceName string) error {
	conn, err := dialKubelet(pluginapi.KubeletSocket, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pluginapi.NewRegistrationClient(conn)
	reqt := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     path.Base(socketFile),
		ResourceName: resourceName,
	}

	_, err = client.Register(context.Background(), reqt)
	return err
}

func dialKubelet(unixSocketPath string, timeout time.Duration) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	c, err := grpc.DialContext(ctx, unixSocketPath, grpc.WithInsecure(), grpc.WithBlock(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}),
	)
	if err != nil {
		return nil, err
	}

	return c, nil
}
