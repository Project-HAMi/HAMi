package scheduler

import (
	"testing"

	"github.com/Project-HAMi/HAMi/pkg/device/nvidia"
	"github.com/Project-HAMi/HAMi/pkg/util"
	"gotest.tools/v3/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_calcScore(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			nodes *map[string]*NodeUsage
			nums  util.PodDeviceRequests
			annos map[string]string
			task  *v1.Pod
		}
		wants struct {
			want *NodeScoreList
			err  error
		}
	}{
		{
			name: "one node two device one pod two container use two device",
			args: struct {
				nodes *map[string]*NodeUsage
				nums  util.PodDeviceRequests
				annos map[string]string
				task  *v1.Pod
			}{
				nodes: &map[string]*NodeUsage{
					"node1": {
						Devices: DeviceUsageList{
							&util.DeviceUsage{
								Id:        "uuid1",
								Index:     0,
								Used:      0,
								Count:     10,
								Usedmem:   0,
								Totalmem:  8000,
								Totalcore: 100,
								Usedcores: 0,
								Numa:      0,
								Type:      nvidia.NvidiaGPUDevice,
								Health:    true,
							},
						},
					},
				},
				nums: util.PodDeviceRequests{
					{
						nvidia.NvidiaGPUDevice: util.ContainerDeviceRequest{
							Nums:             1,
							Type:             nvidia.NvidiaGPUDevice,
							Memreq:           1000,
							MemPercentagereq: 101,
							Coresreq:         30,
						},
					},
					{},
				},
				annos: make(map[string]string),
				task: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test1",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "gpu-burn1",
								Image: "chrstnhntschl/gpu_burn",
								Args:  []string{"6000"},
								Resources: v1.ResourceRequirements{
									Limits: v1.ResourceList{
										"hami.io/gpu":      *resource.NewQuantity(1, resource.BinarySI),
										"hami.io/gpucores": *resource.NewQuantity(30, resource.BinarySI),
										"hami.io/gpumem":   *resource.NewQuantity(1000, resource.BinarySI),
									},
								},
							},
							{
								Name:      "gpu-burn2",
								Image:     "chrstnhntschl/gpu_burn",
								Args:      []string{"6000"},
								Resources: v1.ResourceRequirements{},
							},
						},
					},
				},
			},
			wants: struct {
				want *NodeScoreList
				err  error
			}{
				want: &NodeScoreList{
					{
						nodeID: "node1",
						devices: util.PodDevices{
							"NVIDIA": util.PodSingleDevice{
								{
									{
										Idx:       0,
										UUID:      "uuid1",
										Type:      nvidia.NvidiaGPUDevice,
										Usedcores: 30,
										Usedmem:   1000,
									},
								},
								{
									{
										Idx:       0,
										UUID:      "",
										Type:      "",
										Usedcores: 0,
										Usedmem:   0,
									},
								},
							},
						},
						score: 1,
					},
				},
				err: nil,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotErr := calcScore(test.args.nodes, nil, test.args.nums, test.args.annos, test.args.task)
			assert.DeepEqual(t, test.wants.err, gotErr)
			wantMap := make(map[string]*NodeScore)
			for index, node := range *(test.wants.want) {
				wantMap[node.nodeID] = (*(test.wants.want))[index]
			}
			for i := 0; i < got.Len(); i++ {
				gotI := (*(got))[i]
				wantI := wantMap[gotI.nodeID]
				assert.DeepEqual(t, wantI.nodeID, gotI.nodeID)
				assert.DeepEqual(t, wantI.devices, gotI.devices)
				assert.DeepEqual(t, wantI.score, gotI.score)
			}
		})
	}
}
