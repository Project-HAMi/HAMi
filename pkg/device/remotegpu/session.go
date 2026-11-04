/*
Copyright 2025 The HAMi Authors.

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
	"fmt"
	"net"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/Project-HAMi/HAMi/pkg/util/client"
)

const (
	// sessionStubLabel marks a stub and names the server it fronts, which is
	// how a stub left behind by a server that has gone is recognised.
	sessionStubLabel = "hami.io/lupine-session"

	sessionStubPrefix = "lupine-session-"
	// sessionPullSecretsAnno records the scheduler-owned pull-secret list. It
	// lets reconciliation detect configuration changes without comparing an
	// empty desired list to ServiceAccount secrets injected by the apiserver.
	sessionPullSecretsAnno = "hami.io/lupine-session-image-pull-secrets"

	// podNamespaceEnv is where the scheduler learns its own namespace, the
	// same variable pkg/scheduler reads to find its peers.
	podNamespaceEnv    = "POD_NAMESPACE"
	sessionEndpointEnv = "LUPINE_ENDPOINT"
)

// ReconcileSessionStubs keeps one relay pod on each lupine server's node.
//
// A user reaching the fleet by hand port-forwards to the stub, never to the
// server, so forwarding rights can be granted on a pod that only relays rather
// than on the namespace running the servers. The stub exists for as long as its
// server does, so there is always something to forward to and nothing to create
// at the moment somebody wants it.
//
// Only the leader should call this. Two schedulers reconciling the same stubs
// would fight over creating and deleting them.
func (dev *RemoteGPUDevices) ReconcileSessionStubs(ctx context.Context) {
	if RemoteGPUSessionImage == "" {
		return
	}
	c := client.GetClient()
	if c == nil {
		return
	}
	namespace := os.Getenv(podNamespaceEnv)
	if namespace == "" {
		klog.V(4).InfoS("remotegpu: no namespace to run session stubs in, skipping")
		return
	}

	nodes, err := listLupineNodes(ctx)
	if err != nil {
		// Acting on a partial view would delete the stubs of servers that are
		// still there.
		klog.ErrorS(err, "remotegpu: failed to list lupine server nodes, leaving session stubs alone")
		return
	}
	wanted := map[string]string{} // node name -> endpoint
	for i := range nodes {
		if endpoint, ok := dev.pool.endpointOf(&nodes[i]); ok {
			wanted[nodes[i].Name] = endpoint
		}
	}

	existing, err := c.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: sessionStubLabel})
	if err != nil {
		klog.ErrorS(err, "remotegpu: failed to list session stubs")
		return
	}
	for i := range existing.Items {
		stub := &existing.Items[i]
		node := stub.Labels[sessionStubLabel]
		endpoint, keep := wanted[node]
		// A stub whose endpoint or pull credentials changed is replaced rather
		// than edited, since the fields that carry them cannot be changed on a
		// running pod.
		if keep && stubEndpoint(stub) == endpoint && sessionPullSecretsMatch(stub, RemoteGPUSessionImagePullSecrets) {
			delete(wanted, node)
			continue
		}
		// Only the pod that was listed. Stub names are fixed per node, and a
		// leader handing over can overlap the next one for a moment: the
		// other scheduler may already have replaced this stub under the same
		// name, and a delete by name alone would take its replacement down.
		// The UID precondition turns that into a Conflict, which like
		// NotFound means the pod listed here is already gone.
		opts := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &stub.UID}}
		if err := c.CoreV1().Pods(namespace).Delete(ctx, stub.Name, opts); err != nil && !apierrors.IsNotFound(err) && !apierrors.IsConflict(err) {
			klog.ErrorS(err, "remotegpu: failed to remove session stub", "pod", stub.Name)
		}
	}

	for node, endpoint := range wanted {
		stub, err := sessionStub(namespace, node, endpoint, RemoteGPUSessionImage, RemoteGPUSessionImagePullSecrets)
		if err != nil {
			klog.ErrorS(err, "remotegpu: cannot build a session stub", "node", node, "endpoint", endpoint)
			continue
		}
		if _, err := c.CoreV1().Pods(namespace).Create(ctx, stub, metav1.CreateOptions{}); err != nil {
			if apierrors.IsAlreadyExists(err) {
				// The stub this one replaces is still terminating: the delete
				// above is graceful, so the old pod outlives the call that
				// asked for it. Leave the replacement to the next pass rather
				// than reporting a stub that still relays to the old endpoint.
				klog.V(4).InfoS("remotegpu: session stub still terminating, retrying next pass", "node", node)
			} else {
				klog.ErrorS(err, "remotegpu: failed to create session stub", "node", node)
			}
			continue
		}
		klog.InfoS("remotegpu: session stub ready", "node", node, "endpoint", endpoint)
	}
}

// stubEndpoint reports which server a stub was built to relay to.
func stubEndpoint(stub *corev1.Pod) string {
	for i := range stub.Spec.Containers {
		for _, env := range stub.Spec.Containers[i].Env {
			if env.Name == sessionEndpointEnv {
				return env.Value
			}
		}
	}
	return ""
}

func sessionPullSecretsMatch(stub *corev1.Pod, names []string) bool {
	configured, managed := stub.Annotations[sessionPullSecretsAnno]
	if len(names) == 0 {
		// Ignore PodSpec.ImagePullSecrets here: ServiceAccount admission may
		// populate it when the scheduler did not configure any secrets.
		return !managed
	}
	if !managed || configured != strings.Join(names, ",") || len(stub.Spec.ImagePullSecrets) != len(names) {
		return false
	}
	for i, name := range names {
		if stub.Spec.ImagePullSecrets[i].Name != name {
			return false
		}
	}
	return true
}

func sessionStub(namespace, node, endpoint, image string, pullSecrets []string) (*corev1.Pod, error) {
	// The stub answers on the port its server does, so the port a user
	// forwards is the one they would have used against the server itself.
	_, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return nil, err
	}
	listen, err := net.LookupPort("tcp", port)
	if err != nil {
		return nil, err
	}
	var imagePullSecrets []corev1.LocalObjectReference
	for _, name := range pullSecrets {
		imagePullSecrets = append(imagePullSecrets, corev1.LocalObjectReference{Name: name})
	}
	var annotations map[string]string
	if len(pullSecrets) > 0 {
		annotations = map[string]string{sessionPullSecretsAnno: strings.Join(pullSecrets, ",")}
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        sessionStubPrefix + node,
			Namespace:   namespace,
			Annotations: annotations,
			Labels: map[string]string{
				sessionStubLabel:               node,
				"app.kubernetes.io/component":  "hami-lupine-session",
				"app.kubernetes.io/managed-by": "hami-scheduler",
				"hami.io/webhook":              "ignore",
			},
		},
		Spec: corev1.PodSpec{
			// Assigned rather than scheduled. It belongs on this node and
			// nowhere else, and a lupine node reports no devices, so there is
			// nothing for a scheduler to place against.
			NodeName: node,
			// Whatever keeps other workloads off a lupine node must not keep
			// that node's own relay off it.
			Tolerations:      []corev1.Toleration{{Operator: corev1.TolerationOpExists}},
			RestartPolicy:    corev1.RestartPolicyAlways,
			ImagePullSecrets: imagePullSecrets,
			Containers: []corev1.Container{{
				Name:    "relay",
				Image:   image,
				Env:     []corev1.EnvVar{{Name: sessionEndpointEnv, Value: endpoint}},
				Command: []string{"sh", "-c"},
				Args: []string{fmt.Sprintf("exec socat TCP-LISTEN:%d,fork,reuseaddr TCP:$%s",
					listen, sessionEndpointEnv)},
				Ports: []corev1.ContainerPort{{Name: "lupine", ContainerPort: int32(listen)}},
			}},
		},
	}, nil
}
