/*
Copyright 2026 The HAMi Authors.

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

package remotegpu

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

const (
	// ServerClientLabel marks a Lupine server pod and names the UID of the
	// client pod it serves.
	ServerClientLabel = "hami.io/lupine-client"
	// serverClientAnno names the client pod as namespace/name.
	serverClientAnno = "hami.io/lupine-client-pod"

	serverPrefix    = "lupine-"
	serverContainer = "lupine-server"

	// HAMi-core reaches the server through this volume and keeps the memory
	// the server's connections share alongside it.
	libVolumeName = "hami-remote-gpu-lib"
	libMountPath  = "/hami-remote-gpu"
	libSourceGlob = "/k8s-vgpu/lib/nvidia/libvgpu.so.*"

	// serverBindGrace is how long a server may wait for its client to be
	// bound. Bind starts the server just before it binds the client.
	serverBindGrace = heldTTL
)

var errNoServerNamespace = errors.New("no namespace to run Lupine servers in: " + podNamespaceEnv + " is unset")

// serverPod builds the Lupine server a client pod's annotations ask for: on
// the node serving its cards, listening on the endpoint the scheduler
// assigned, and seeing only those cards. The name hashes everything the pod
// is built from, so a pod placed again gets a new server rather than one
// still serving its last placement.
func serverPod(namespace string, clientPod *corev1.Pod, cfg ServerConfig) (*corev1.Pod, error) {
	endpoint := clientPod.Annotations[LupineServerAnno]
	_, rawPort, err := net.SplitHostPort(endpoint)
	if err != nil {
		return nil, fmt.Errorf("pod has no Lupine endpoint: %w", err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port <= 0 || port > 65535 {
		return nil, fmt.Errorf("invalid Lupine endpoint %q", endpoint)
	}
	if cfg.Image == "" {
		return nil, errors.New("no Lupine server image configured")
	}
	shares := podShares(clientPod)
	var node string
	for id := range shares {
		node = serverOf(id)
		break
	}
	cards := strings.Split(clientPod.Annotations[ServerDevicesAnno], ",")
	if node == "" || len(cards) == 0 || cards[0] == "" {
		return nil, errors.New("pod has no RemoteGPU allocation")
	}
	env := []corev1.EnvVar{
		{Name: "NVIDIA_VISIBLE_DEVICES", Value: strings.Join(cards, ",")},
		{Name: "NVIDIA_DRIVER_CAPABILITIES", Value: "compute,utility"},
		// Clients name cards by the server's CUDA ordinals, which the
		// scheduler counted in PCI bus order.
		{Name: "CUDA_DEVICE_ORDER", Value: "PCI_BUS_ID"},
		{Name: "LUPINE_PORT", Value: rawPort},
	}
	// A card the pod shares by memory is limited to its share. The server
	// enforces it, where the workload cannot take the library out.
	var limits []corev1.EnvVar
	if RemoteGPULibImage != "" {
		for i, uuid := range cards {
			if s, ok := shares[deviceID(node, uuid)]; ok && !s.whole && s.mem > 0 {
				limits = append(limits, corev1.EnvVar{Name: fmt.Sprintf("CUDA_DEVICE_MEMORY_LIMIT_%d", i), Value: fmt.Sprintf("%dm", s.mem)})
			}
		}
	}
	ctr := corev1.Container{
		Name:            serverContainer,
		Image:           cfg.Image,
		ImagePullPolicy: cfg.ImagePullPolicy,
		Ports:           []corev1.ContainerPort{{Name: "lupine", ContainerPort: int32(port), HostPort: int32(port)}},
		SecurityContext: cfg.SecurityContext,
		Resources:       cfg.Resources,
		// Probes name the port "lupine", which is this pod's own endpoint.
		LivenessProbe:  cfg.LivenessProbe,
		ReadinessProbe: cfg.ReadinessProbe,
	}
	spec := corev1.PodSpec{
		// Assigned rather than scheduled: the cards are on this node, and the
		// scheduler has already accounted for them.
		NodeName: node,
		// Clients dial the node's InternalIP.
		HostNetwork:                  true,
		DNSPolicy:                    corev1.DNSClusterFirstWithHostNet,
		RestartPolicy:                corev1.RestartPolicyAlways,
		AutomountServiceAccountToken: new(false),
		Tolerations:                  cfg.Tolerations,
	}
	if cfg.RuntimeClassName != "" {
		spec.RuntimeClassName = new(cfg.RuntimeClassName)
	}
	for _, name := range cfg.ImagePullSecrets {
		spec.ImagePullSecrets = append(spec.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
	}
	if len(limits) > 0 {
		mount := corev1.VolumeMount{Name: libVolumeName, MountPath: libMountPath}
		spec.Volumes = []corev1.Volume{{Name: libVolumeName, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}
		spec.InitContainers = []corev1.Container{{
			Name:  libVolumeName,
			Image: RemoteGPULibImage,
			// The library is published under a version suffix, and the glob
			// keeps this from having to track the chart's image tag.
			Command:      []string{"sh", "-c", "cp " + libSourceGlob + " " + libMountPath + "/libvgpu.so"},
			VolumeMounts: []corev1.VolumeMount{mount},
		}}
		ctr.VolumeMounts = []corev1.VolumeMount{mount}
		env = append(env, corev1.EnvVar{Name: "LD_PRELOAD", Value: libMountPath + "/libvgpu.so"},
			// One server process per connection, all held to one limit.
			corev1.EnvVar{Name: "CUDA_DEVICE_MEMORY_SHARED_CACHE", Value: libMountPath + "/vgpu.cache"})
		env = append(env, limits...)
	}
	ctr.Env = env
	spec.Containers = []corev1.Container{ctr}

	h := fnv.New32a()
	fmt.Fprintf(h, "%s|%s|%s|%s", node, endpoint, clientPod.Annotations[ServerDevicesAnno], clientPod.Annotations[AllocatedAnnos])
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s%s-%08x", serverPrefix, clientPod.UID, h.Sum32()),
			Namespace: namespace,
			Labels: map[string]string{
				ServerClientLabel:              string(clientPod.UID),
				"app.kubernetes.io/component":  "hami-lupine-server",
				"app.kubernetes.io/managed-by": "hami-scheduler",
				"hami.io/webhook":              "ignore",
			},
			Annotations: map[string]string{
				LupineServerAnno: endpoint,
				serverClientAnno: clientPod.Namespace + "/" + clientPod.Name,
			},
		},
		Spec: spec,
	}, nil
}

// StartServer starts the Lupine server of a client pod Filter has placed.
// Bind calls it just before it binds the pod, so the server starts while the
// client does. The pod is read back: the scheduler's cached copy may predate
// the annotations the last Filter wrote, or carry an earlier attempt's.
func (dev *RemoteGPUDevices) StartServer(ctx context.Context, clientPod *corev1.Pod) error {
	c := client.GetClient()
	if c == nil {
		return errNoClient
	}
	namespace := os.Getenv(podNamespaceEnv)
	if namespace == "" {
		return errNoServerNamespace
	}
	fresh, err := c.CoreV1().Pods(clientPod.Namespace).Get(ctx, clientPod.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if fresh.UID != clientPod.UID {
		return fmt.Errorf("pod %s/%s was replaced", clientPod.Namespace, clientPod.Name)
	}
	server, err := serverPod(namespace, fresh, dev.server)
	if err != nil {
		return err
	}
	if _, err := c.CoreV1().Pods(namespace).Create(ctx, server, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	klog.InfoS("remotegpu: started Lupine server", "pod", klog.KObj(fresh), "server", server.Name, "endpoint", server.Annotations[LupineServerAnno])
	return nil
}

// StopServer removes the Lupine servers of a client pod whose bind failed,
// so they give their port back now rather than at the next reconcile.
func (dev *RemoteGPUDevices) StopServer(ctx context.Context, clientPod *corev1.Pod) {
	c := client.GetClient()
	namespace := os.Getenv(podNamespaceEnv)
	if c == nil || namespace == "" || clientPod == nil || clientPod.UID == "" {
		return
	}
	servers, err := c.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: ServerClientLabel + "=" + string(clientPod.UID)})
	if err != nil {
		klog.ErrorS(err, "remotegpu: failed to list Lupine servers", "pod", klog.KObj(clientPod))
		return
	}
	for i := range servers.Items {
		deleteServer(ctx, c, &servers.Items[i])
	}
}

// ReconcileServers keeps one Lupine server running for each placed client
// pod, and removes servers whose client is gone, finished, or was placed
// again. Kubernetes cannot do the removal through an owner reference: the
// servers run in the scheduler's namespace, the clients in their own.
//
// Only the leader should call this.
func (dev *RemoteGPUDevices) ReconcileServers(ctx context.Context) {
	c := client.GetClient()
	namespace := os.Getenv(podNamespaceEnv)
	if c == nil || namespace == "" {
		return
	}
	// Both lists must succeed: acting on a partial view would remove the
	// servers of clients that are still running.
	servers, err := c.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: ServerClientLabel})
	if err != nil {
		klog.ErrorS(err, "remotegpu: failed to list Lupine servers")
		return
	}
	clients, err := c.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{LabelSelector: RemoteGPUPodLabel + "=true", ResourceVersion: "0"})
	if err != nil {
		klog.ErrorS(err, "remotegpu: failed to list RemoteGPU pods")
		return
	}
	wanted := map[string]*corev1.Pod{}
	pending := map[types.UID]bool{}
	for i := range clients.Items {
		pod := &clients.Items[i]
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed || pod.Annotations[LupineServerAnno] == "" {
			continue
		}
		if pod.Spec.NodeName == "" {
			pending[pod.UID] = true
			continue
		}
		server, err := serverPod(namespace, pod, dev.server)
		if err != nil {
			klog.V(4).InfoS("remotegpu: cannot build a Lupine server", "pod", klog.KObj(pod), "error", err)
			continue
		}
		wanted[server.Name] = server
	}
	for i := range servers.Items {
		server := &servers.Items[i]
		if server.DeletionTimestamp != nil {
			delete(wanted, server.Name)
			continue
		}
		if _, ok := wanted[server.Name]; ok && server.Status.Phase != corev1.PodFailed && server.Status.Phase != corev1.PodSucceeded {
			delete(wanted, server.Name)
			continue
		}
		// Bind starts a server before it binds the client.
		if pending[types.UID(server.Labels[ServerClientLabel])] && time.Since(server.CreationTimestamp.Time) < serverBindGrace {
			continue
		}
		deleteServer(ctx, c, server)
	}
	for _, server := range wanted {
		if _, err := c.CoreV1().Pods(namespace).Create(ctx, server, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			klog.ErrorS(err, "remotegpu: failed to start Lupine server", "server", server.Name)
		}
	}
}

// deleteServer removes the server that was listed and no other. The UID
// precondition turns a replacement created under the same name meanwhile
// into a Conflict, which like NotFound means the listed pod is gone.
func deleteServer(ctx context.Context, c kubernetes.Interface, server *corev1.Pod) {
	opts := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &server.UID}}
	if err := c.CoreV1().Pods(server.Namespace).Delete(ctx, server.Name, opts); err != nil && !apierrors.IsNotFound(err) && !apierrors.IsConflict(err) {
		klog.ErrorS(err, "remotegpu: failed to remove Lupine server", "server", server.Name)
		return
	}
	klog.InfoS("remotegpu: removed Lupine server", "server", server.Name, "client", server.Annotations[serverClientAnno])
}
