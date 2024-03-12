package client

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

var (
	kubeClient  kubernetes.Interface
	runE2ETests bool
)

func init() {
	runE2ETests = shouldRunE2ETests()

	if !runE2ETests {
		initKubeClient()
	}
}

func shouldRunE2ETests() bool {
	runE2ETestsStr := os.Getenv("RUN_E2E_TESTS")
	runE2ETests, err := strconv.ParseBool(runE2ETestsStr)
	if err != nil {
		klog.Errorf("Failed to parse RUN_E2E_TESTS env var: %v", err)
		return false
	}

	return runE2ETests
}

func initKubeClient() {
	var err error
	kubeClient, err = NewClient()
	if err != nil {
		klog.Errorf("Failed to init kubernetes client: %v", err)
		panic(err)
	}
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
		klog.Infof("Trying config from file: %s", kubeConfig)
		config, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
		if err != nil {
			return nil, fmt.Errorf("BuildConfigFromFlags failed for file %s: %v", kubeConfig, err)
		}
	}
	return kubernetes.NewForConfig(config)
}
