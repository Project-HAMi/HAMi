package main

import (
	"k8s.io/klog"
)

//var addr = flag.String("listen-address", ":9394", "The address to listen on for HTTP requests.")

//const shared_directory = "/usr/local/vgpu/shared"

func main() {

	if err := ValidateEnvVars(); err != nil {
		klog.Fatalf("Failed to validate environment variables: %v", err)
	}
	cgroupDriver = 0
	errchannel := make(chan error)
	go serveInfo(errchannel)
	go initmetrics()
	go watchAndFeedback()
	for {
		err := <-errchannel
		klog.Errorf("failed to serve: %v", err)
	}
}
