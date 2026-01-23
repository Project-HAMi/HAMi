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

package util

import (
	"context"
	"encoding/json"
<<<<<<< HEAD
	"flag"
	"fmt"
=======
	"errors"
	"fmt"
	"strconv"
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	"strings"
	"time"

<<<<<<< HEAD
	"github.com/Project-HAMi/HAMi/pkg/util/client"
	"github.com/Project-HAMi/HAMi/pkg/util/nodelock"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
=======
	"4pd.io/k8s-vgpu/pkg/api"
	"4pd.io/k8s-vgpu/pkg/util/client"
	v1 "k8s.io/api/core/v1"
>>>>>>> 21785f7 (update to v2.3.2)
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

<<<<<<< HEAD
var (
	HandshakeAnnos map[string]string
)

func init() {
	HandshakeAnnos = make(map[string]string)
}

func GetNode(nodename string) (*corev1.Node, error) {
	if nodename == "" {
		klog.ErrorS(nil, "Node name is empty")
		return nil, fmt.Errorf("nodename is empty")
	}

	klog.V(5).InfoS("Fetching node", "nodeName", nodename)
	n, err := client.GetClient().CoreV1().Nodes().Get(context.Background(), nodename, metav1.GetOptions{})
	if err != nil {
		switch {
		case apierrors.IsNotFound(err):
			klog.ErrorS(err, "Node not found", "nodeName", nodename)
			return nil, fmt.Errorf("node %s not found", nodename)
		case apierrors.IsUnauthorized(err):
			klog.ErrorS(err, "Unauthorized to access node", "nodeName", nodename)
			return nil, fmt.Errorf("unauthorized to access node %s", nodename)
		default:
			klog.ErrorS(err, "Failed to get node", "nodeName", nodename)
			return nil, fmt.Errorf("failed to get node %s: %v", nodename, err)
		}
	}

	klog.V(5).InfoS("Successfully fetched node", "nodeName", nodename)
	return n, nil
=======
func GetNode(nodename string) (*v1.Node, error) {
	n, err := client.GetClient().CoreV1().Nodes().Get(context.Background(), nodename, metav1.GetOptions{})
	return n, err
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
}

<<<<<<< HEAD
func GetPendingPod(ctx context.Context, node string) (*corev1.Pod, error) {
	pod, err := GetAllocatePodByNode(ctx, node)
	if err != nil {
		return nil, err
	}
	if pod != nil {
		return pod, nil
	}
	// filter pods for this node.
	selector := fmt.Sprintf("spec.nodeName=%s", node)
	podListOptions := metav1.ListOptions{
		FieldSelector: selector,
	}
	podlist, err := client.GetClient().CoreV1().Pods("").List(ctx, podListOptions)
=======
func GetPendingPod(node string) (*v1.Pod, error) {
	podlist, err := client.GetClient().CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
>>>>>>> 21785f7 (update to v2.3.2)
	if err != nil {
		return nil, err
	}
	for _, p := range podlist.Items {
		if p.Status.Phase != corev1.PodPending {
			continue
		}
		if _, ok := p.Annotations[BindTimeAnnotations]; !ok {
			continue
		}
		if phase, ok := p.Annotations[DeviceBindPhase]; !ok {
			continue
		} else {
			if strings.Compare(phase, DeviceBindAllocating) != 0 {
				continue
			}
		}
		if n, ok := p.Annotations[AssignedNodeAnnotations]; !ok {
			continue
		} else {
			if strings.Compare(n, node) == 0 {
				return &p, nil
			}
		}
	}
	return nil, fmt.Errorf("no binding pod found on node %s", node)
}

func GetAllocatePodByNode(ctx context.Context, nodeName string) (*corev1.Pod, error) {
	node, err := client.GetClient().CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if value, ok := node.Annotations[nodelock.NodeLockKey]; ok {
		klog.V(2).Infof("node annotation key is %s, value is %s ", nodelock.NodeLockKey, value)
		_, ns, name, err := nodelock.ParseNodeLock(value)
		if err != nil {
			return nil, err
		}
		if ns == "" || name == "" {
			return nil, nil
		}
		return client.GetClient().CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
	}
	return nil, nil
}

<<<<<<< HEAD
func PatchNodeAnnotations(node *corev1.Node, annotations map[string]string) error {
=======
func DecodeNodeDevices(str string) []*api.DeviceInfo {
	if !strings.Contains(str, ":") {
		return []*api.DeviceInfo{}
	}
	tmp := strings.Split(str, ":")
	var retval []*api.DeviceInfo
	for _, val := range tmp {
		if strings.Contains(val, ",") {
			items := strings.Split(val, ",")
			if len(items) == 6 {
				count, _ := strconv.Atoi(items[1])
				devmem, _ := strconv.Atoi(items[2])
				devcore, _ := strconv.Atoi(items[3])
				health, _ := strconv.ParseBool(items[5])
				i := api.DeviceInfo{
					Id:      items[0],
					Count:   int32(count),
					Devmem:  int32(devmem),
					Devcore: int32(devcore),
					Type:    items[4],
					Health:  health,
				}
				retval = append(retval, &i)
			} else {
				count, _ := strconv.Atoi(items[1])
				devmem, _ := strconv.Atoi(items[2])
				health, _ := strconv.ParseBool(items[4])
				i := api.DeviceInfo{
					Id:      items[0],
					Count:   int32(count),
					Devmem:  int32(devmem),
					Devcore: 100,
					Type:    items[3],
					Health:  health,
				}
				retval = append(retval, &i)
			}
		}
	}
	return retval
}

func EncodeNodeDevices(dlist []*api.DeviceInfo) string {
	tmp := ""
	for _, val := range dlist {
		tmp += val.Id + "," + strconv.FormatInt(int64(val.Count), 10) + "," + strconv.Itoa(int(val.Devmem)) + "," + strconv.Itoa(int(val.Devcore)) + "," + val.Type + "," + strconv.FormatBool(val.Health) + ":"
	}
	klog.V(3).Infoln("Encoded node Devices", tmp)
	return tmp
}

func EncodeContainerDevices(cd ContainerDevices) string {
	tmp := ""
	for _, val := range cd {
		tmp += val.UUID + "," + val.Type + "," + strconv.Itoa(int(val.Usedmem)) + "," + strconv.Itoa(int(val.Usedcores)) + ":"
	}
	fmt.Println("Encoded container Devices=", tmp)
	return tmp
	//return strings.Join(cd, ",")
}

func EncodePodDevices(pd PodDevices) string {
	var ss []string
	for _, cd := range pd {
		ss = append(ss, EncodeContainerDevices(cd))
	}
	return strings.Join(ss, ";")
}

func DecodeContainerDevices(str string) (ContainerDevices, error) {
	if len(str) == 0 {
		return ContainerDevices{}, nil
	}
	cd := strings.Split(str, ":")
	contdev := ContainerDevices{}
	tmpdev := ContainerDevice{}
	//fmt.Println("before container device", str)
	if len(str) == 0 {
		return ContainerDevices{}, nil
	}
	for _, val := range cd {
		if strings.Contains(val, ",") {
			//fmt.Println("cd is ", val)
			tmpstr := strings.Split(val, ",")
			if len(tmpstr) < 4 {
				return ContainerDevices{}, fmt.Errorf("pod annotation format error; information missing, please do not use nodeName field in task")
			}
			tmpdev.UUID = tmpstr[0]
			tmpdev.Type = tmpstr[1]
			devmem, _ := strconv.ParseInt(tmpstr[2], 10, 32)
			tmpdev.Usedmem = int32(devmem)
			devcores, _ := strconv.ParseInt(tmpstr[3], 10, 32)
			tmpdev.Usedcores = int32(devcores)
			contdev = append(contdev, tmpdev)
		}
	}
	//fmt.Println("Decoded container device", contdev)
	return contdev, nil
}

func DecodePodDevices(str string) (PodDevices, error) {
	if len(str) == 0 {
		return PodDevices{}, nil
	}
	var pd PodDevices
	for _, s := range strings.Split(str, ";") {
		cd, err := DecodeContainerDevices(s)
		if err != nil {
			return PodDevices{}, nil
		}
		pd = append(pd, cd)
	}
	return pd, nil
}

func GetNextDeviceRequest(dtype string, p v1.Pod) (v1.Container, ContainerDevices, error) {
	pdevices, err := DecodePodDevices(p.Annotations[AssignedIDsToAllocateAnnotations])
	if err != nil {
		return v1.Container{}, ContainerDevices{}, err
	}
	klog.Infoln("pdevices=", pdevices)
	res := ContainerDevices{}
	for idx, val := range pdevices {
		found := false
		for _, dev := range val {
			if strings.Compare(dtype, dev.Type) == 0 {
				res = append(res, dev)
				found = true
			}
		}
		if found {
			return p.Spec.Containers[idx], res, nil
		}
	}
	return v1.Container{}, res, errors.New("device request not found")
}

func GetContainerDeviceStrArray(c ContainerDevices) []string {
	tmp := []string{}
	for _, val := range c {
		tmp = append(tmp, val.UUID)
	}
	return tmp
}

func EraseNextDeviceTypeFromAnnotation(dtype string, p v1.Pod) error {
	pdevices, err := DecodePodDevices(p.Annotations[AssignedIDsToAllocateAnnotations])
	if err != nil {
		return err
	}
	res := PodDevices{}
	found := false
	for _, val := range pdevices {
		if found {
			res = append(res, val)
			continue
		} else {
			tmp := ContainerDevices{}
			for _, dev := range val {
				klog.Infoln("Selecting erase res=", dtype, ":", dev.Type)
				if strings.Compare(dtype, dev.Type) == 0 {
					found = true
				} else {
					tmp = append(tmp, dev)
				}
			}
			if !found {
				res = append(res, val)
			} else {
				res = append(res, tmp)
			}
		}
	}
	klog.Infoln("After erase res=", res)
	newannos := make(map[string]string)
	newannos[AssignedIDsToAllocateAnnotations] = EncodePodDevices(res)
	return PatchPodAnnotations(&p, newannos)
}

func PatchNodeAnnotations(node *v1.Node, annotations map[string]string) error {
>>>>>>> 32fbedb (update device_plugin version to nvidia v0.14.0)
	type patchMetadata struct {
		Annotations map[string]string `json:"annotations,omitempty"`
	}
	type patchNode struct {
		Metadata patchMetadata `json:"metadata"`
	}

	p := patchNode{}
	p.Metadata.Annotations = annotations

	bytes, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = client.GetClient().CoreV1().Nodes().
<<<<<<< HEAD
		Patch(context.Background(), node.Name, k8stypes.MergePatchType, bytes, metav1.PatchOptions{})
=======
		Patch(context.Background(), node.Name, k8stypes.StrategicMergePatchType, bytes, metav1.PatchOptions{})
>>>>>>> 21785f7 (update to v2.3.2)
	if err != nil {
		klog.Infoln("annotations=", annotations)
		klog.Infof("patch node %v failed, %v", node.Name, err)
	}
	return err
}

func PatchPodAnnotations(pod *corev1.Pod, annotations map[string]string) error {
	type patchMetadata struct {
		Annotations map[string]string `json:"annotations,omitempty"`
		Labels      map[string]string `json:"labels,omitempty"`
	}
	type patchPod struct {
		Metadata patchMetadata `json:"metadata"`
	}

	p := patchPod{}
	p.Metadata.Annotations = annotations
	label := make(map[string]string)
	if v, ok := annotations[AssignedNodeAnnotations]; ok && v != "" {
		label[AssignedNodeAnnotations] = v
		p.Metadata.Labels = label
	}

	bytes, err := json.Marshal(p)
	if err != nil {
		return err
	}
<<<<<<< HEAD
	klog.V(5).Infof("patch pod %s/%s annotation content is %s", pod.Namespace, pod.Name, string(bytes))
	_, err = client.GetClient().CoreV1().Pods(pod.Namespace).
		Patch(context.Background(), pod.Name, k8stypes.MergePatchType, bytes, metav1.PatchOptions{})
=======
	_, err = client.GetClient().CoreV1().Pods(pod.Namespace).
		Patch(context.Background(), pod.Name, k8stypes.StrategicMergePatchType, bytes, metav1.PatchOptions{})
>>>>>>> 21785f7 (update to v2.3.2)
	if err != nil {
		klog.Infof("patch pod %v failed, %v", pod.Name, err)
	}
	return err
}

func InitKlogFlags() *flag.FlagSet {
	// Init log flags
	flagset := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(flagset)

	return flagset
}

func MarkAnnotationsToDelete(devType string, nn string) error {
	tmppat := make(map[string]string)
	tmppat[devType] = "Deleted_" + time.Now().Format(time.DateTime)
	n, err := GetNode(nn)
	if err != nil {
		klog.Errorln("get node failed", err.Error())
		return err
	}
	return PatchNodeAnnotations(n, tmppat)
}

func GetGPUSchedulerPolicyByPod(defaultPolicy string, task *corev1.Pod) string {
	userGPUPolicy := defaultPolicy
	if task != nil && task.Annotations != nil {
		if value, ok := task.Annotations[GPUSchedulerPolicyAnnotationKey]; ok {
			userGPUPolicy = value
		}
	}
	return userGPUPolicy
}

func IsPodInTerminatedState(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded
}

func AllContainersCreated(pod *corev1.Pod) bool {
	return len(pod.Status.ContainerStatuses) >= len(pod.Spec.Containers)
}
