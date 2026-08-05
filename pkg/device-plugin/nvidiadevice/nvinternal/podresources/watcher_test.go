package podresources

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	podresourcesv1 "k8s.io/kubelet/pkg/apis/podresources/v1"
)

type fakePodResourcesServer struct {
	podresourcesv1.UnimplementedPodResourcesListerServer
	mu   sync.RWMutex
	resp *podresourcesv1.ListPodResourcesResponse
}
func (s *fakePodResourcesServer) setResponse(resp *podresourcesv1.ListPodResourcesResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resp = resp
}

func (s *fakePodResourcesServer) List(context.Context, *podresourcesv1.ListPodResourcesRequest) (*podresourcesv1.ListPodResourcesResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.resp, nil
}

func startFakePodResourcesServer(t *testing.T, resp *podresourcesv1.ListPodResourcesResponse) (socketPath string, setResp func(*podresourcesv1.ListPodResourcesResponse), stop func()) {
	t.Helper()
	dir := t.TempDir()
	socketPath = filepath.Join(dir, "kubelet.sock")
	l, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}

	srv := grpc.NewServer()
	fake := &fakePodResourcesServer{resp: resp}
	podresourcesv1.RegisterPodResourcesListerServer(srv, fake)
	go func() {
		_ = srv.Serve(l)
	}()

	setResp = fake.setResponse
	stop = func() {
		srv.Stop()
		_ = l.Close()
		_ = os.Remove(socketPath)
	}
	return socketPath, setResp, stop
}

func TestTickCanListPodResources(t *testing.T) {
	initial := &podresourcesv1.ListPodResourcesResponse{
		PodResources: []*podresourcesv1.PodResources{
			{
				Name:      "p1",
				Namespace: "default",
				Containers: []*podresourcesv1.ContainerResources{
					{
						Name: "c1",
						Devices: []*podresourcesv1.ContainerDevices{
							{ResourceName: "nvidia.com/gpu", DeviceIds: []string{"MIG-a", "MIG-b"}},
						},
					},
				},
			},
		},
	}
	socketPath, _, stop := startFakePodResourcesServer(t, initial)
	defer stop()

	w := NewWatcher(socketPath, time.Second, []string{"nvidia.com/gpu"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := w.tick(ctx, true); err != nil {
		t.Fatalf("tick should succeed, got error: %v", err)
	}

	got := w.prev["nvidia.com/gpu"]
	if got == nil {
		t.Fatalf("expected snapshot for nvidia.com/gpu")
	}
	if _, ok := got["MIG-a"]; !ok {
		t.Fatalf("expected MIG-a in snapshot")
	}
	if _, ok := got["MIG-b"]; !ok {
		t.Fatalf("expected MIG-b in snapshot")
	}
}

func TestTickDiffTriggersRelease(t *testing.T) {
	resp1 := &podresourcesv1.ListPodResourcesResponse{
		PodResources: []*podresourcesv1.PodResources{
			{
				Name:      "p1",
				Namespace: "default",
				Containers: []*podresourcesv1.ContainerResources{
					{
						Name: "c1",
						Devices: []*podresourcesv1.ContainerDevices{
							{ResourceName: "nvidia.com/gpu", DeviceIds: []string{"MIG-a", "MIG-b"}},
						},
					},
				},
			},
		},
	}
	resp2 := &podresourcesv1.ListPodResourcesResponse{
		PodResources: []*podresourcesv1.PodResources{
			{
				Name:      "p1",
				Namespace: "default",
				Containers: []*podresourcesv1.ContainerResources{
					{
						Name: "c1",
						Devices: []*podresourcesv1.ContainerDevices{
							{ResourceName: "nvidia.com/gpu", DeviceIds: []string{"MIG-a"}},
						},
					},
				},
			},
		},
	}

	socketPath, setResp, stop := startFakePodResourcesServer(t, resp1)
	defer stop()

	var (
		mu       sync.Mutex
		released []string
	)
	w := NewWatcher(socketPath, time.Second, []string{"nvidia.com/gpu"}, func(_ string, deviceID string) {
		mu.Lock()
		defer mu.Unlock()
		released = append(released, deviceID)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := w.tick(ctx, true); err != nil {
		t.Fatalf("prime tick failed: %v", err)
	}
	setResp(resp2)
	if err := w.tick(ctx, false); err != nil {
		t.Fatalf("diff tick failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(released) != 1 || released[0] != "MIG-b" {
		t.Fatalf("expected one released device MIG-b, got %v", released)
	}
}
