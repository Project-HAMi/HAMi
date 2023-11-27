package nvidia

import (
	"flag"
	"fmt"
	"strconv"
	"strings"

	"4pd.io/k8s-vgpu/pkg/api"
	"4pd.io/k8s-vgpu/pkg/scheduler/config"
	"4pd.io/k8s-vgpu/pkg/util"
	corev1 "k8s.io/api/core/v1"
)

const (
	HandshakeAnnos      = "4pd.io/node-handshake"
	RegisterAnnos       = "4pd.io/node-nvidia-register"
	NvidiaGPUDevice     = "NVIDIA"
	NvidiaGPUCommonWord = "GPU"
	GPUInUse            = "nvidia.com/use-gputype"
	GPUNoUse            = "nvidia.com/nouse-gputype"
	NumaBind            = "nvidia.com/numa-bind"
)

var (
	ResourceName          string
	ResourceMem           string
	ResourceCores         string
	ResourceMemPercentage string
	ResourcePriority      string
	DebugMode             bool
)

type NvidiaGPUDevices struct {
}

func InitNvidiaDevice() *NvidiaGPUDevices {
	return &NvidiaGPUDevices{}
}

func (dev *NvidiaGPUDevices) ParseConfig(fs *flag.FlagSet) {
	fs.StringVar(&ResourceName, "resource-name", "nvidia.com/gpu", "resource name")
	fs.StringVar(&ResourceMem, "resource-mem", "nvidia.com/gpumem", "gpu memory to allocate")
	fs.StringVar(&ResourceMemPercentage, "resource-mem-percentage", "nvidia.com/gpumem-percentage", "gpu memory fraction to allocate")
	fs.StringVar(&ResourceCores, "resource-cores", "nvidia.com/gpucores", "cores percentage to use")
	fs.StringVar(&ResourcePriority, "resource-priority", "vgputaskpriority", "vgpu task priority 0 for high and 1 for low")
}

func (dev *NvidiaGPUDevices) MutateAdmission(ctr *corev1.Container) bool {
	/*gpu related */
	priority, ok := ctr.Resources.Limits[corev1.ResourceName(ResourcePriority)]
	if ok {
		ctr.Env = append(ctr.Env, corev1.EnvVar{
			Name:  api.TaskPriority,
			Value: fmt.Sprint(priority.Value()),
		})
	}
	_, ok = ctr.Resources.Limits[corev1.ResourceName(ResourceName)]
	return ok
}

func checkGPUtype(annos map[string]string, cardtype string, numa int) bool {
	inuse, ok := annos[GPUInUse]
	if ok {
		if !strings.Contains(inuse, ",") {
			if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(inuse)) {
				return true
			}
		} else {
			for _, val := range strings.Split(inuse, ",") {
				if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(val)) {
					return true
				}
			}
		}
		return false
	}
	nouse, ok := annos[GPUNoUse]
	if ok {
		if !strings.Contains(nouse, ",") {
			if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(nouse)) {
				return false
			}
		} else {
			for _, val := range strings.Split(nouse, ",") {
				if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(val)) {
					return false
				}
			}
		}
		return true
	}
	return true
}

func assertNuma(annos map[string]string) bool {
	numabind, ok := annos[NumaBind]
	if ok {
		enforce, err := strconv.ParseBool(numabind)
		if err == nil && enforce {
			return true
		}
	}
	return false
}

func (dev *NvidiaGPUDevices) CheckType(annos map[string]string, d util.DeviceUsage, n util.ContainerDeviceRequest) (bool, bool, bool) {
	if strings.Compare(n.Type, NvidiaGPUDevice) == 0 {
		return true, checkGPUtype(annos, d.Type, d.Numa), assertNuma(annos)
	}
	return false, false, false
}

func (dev *NvidiaGPUDevices) GenerateResourceRequests(ctr *corev1.Container) util.ContainerDeviceRequest {
	resourceName := corev1.ResourceName(ResourceName)
	resourceMem := corev1.ResourceName(ResourceMem)
	resourceMemPercentage := corev1.ResourceName(ResourceMemPercentage)
	resourceCores := corev1.ResourceName(ResourceCores)
	v, ok := ctr.Resources.Limits[resourceName]
	if !ok {
		v, ok = ctr.Resources.Requests[resourceName]
	}
	if ok {
		if n, ok := v.AsInt64(); ok {
			memnum := 0
			mem, ok := ctr.Resources.Limits[resourceMem]
			if !ok {
				mem, ok = ctr.Resources.Requests[resourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
				if ok {
					memnum = int(memnums)
				}
			}
			mempnum := int32(101)
			mem, ok = ctr.Resources.Limits[resourceMemPercentage]
			if !ok {
				mem, ok = ctr.Resources.Requests[resourceMemPercentage]
			}
			if ok {
				mempnums, ok := mem.AsInt64()
				if ok {
					mempnum = int32(mempnums)
				}
			}
			if mempnum == 101 && memnum == 0 {
				if config.DefaultMem != 0 {
					memnum = int(config.DefaultMem)
				} else {
					mempnum = 100
				}
			}
			corenum := config.DefaultCores
			core, ok := ctr.Resources.Limits[resourceCores]
			if !ok {
				core, ok = ctr.Resources.Requests[resourceCores]
			}
			if ok {
				corenums, ok := core.AsInt64()
				if ok {
					corenum = int32(corenums)
				}
			}
			return util.ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             NvidiaGPUDevice,
				Memreq:           int32(memnum),
				MemPercentagereq: int32(mempnum),
				Coresreq:         int32(corenum),
			}
		}
	}
	return util.ContainerDeviceRequest{}
}
