package client

import (
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

var (
	kubeClient kubernetes.Interface
)

func init() {
	kubeClient, _ = NewClient()
}

func GetClient() kubernetes.Interface {
	return kubeClient
}

// NewClient connects to an API server
func NewClient() (kubernetes.Interface, error) {
	kubeConfig := os.Getenv("KUBECONFIG")
	if kubeConfig == "" {
		kubeConfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		klog.Infoln("InClusterConfig failed", err.Error())
		config, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
		if err != nil {
			klog.Errorln("BuildFromFlags failed", err.Error())
			return nil, err
		}
	}
	client, err := kubernetes.NewForConfig(config)
	return client, err
}
