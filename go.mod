module 4pd.io/k8s-vgpu

go 1.16

require (
	github.com/NVIDIA/go-gpuallocator v0.2.1
	github.com/NVIDIA/gpu-monitoring-tools v0.0.0-20210624153948-4902944b3b52
	github.com/fsnotify/fsnotify v1.4.7
	github.com/golang/protobuf v1.4.3
	github.com/google/go-cmp v0.5.5 // indirect
	github.com/json-iterator/go v1.1.11 // indirect
	github.com/julienschmidt/httprouter v1.2.0
	github.com/spf13/cobra v1.1.3
	github.com/spf13/viper v1.7.0
	github.com/stretchr/testify v1.7.0 // indirect
	golang.org/x/net v0.0.0-20210405180319-a5a99cb37ef4
	golang.org/x/text v0.3.5 // indirect
	google.golang.org/grpc v1.27.1
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
	k8s.io/api v0.21.2
	k8s.io/apimachinery v0.21.2
	k8s.io/client-go v0.21.2
	k8s.io/klog/v2 v2.9.0
	k8s.io/kube-scheduler v0.21.2
	k8s.io/kubelet v0.21.2
)
