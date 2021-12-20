package main

import (
	"flag"
	"fmt"
)

var addr = flag.String("listen-address", ":9394", "The address to listen on for HTTP requests.")

const shared_directory = "/usr/local/vgpu/shared"

func main() {
	errchannel := make(chan error)
	go serveinfo(errchannel)
	/*
		ret := nvml.Init()
		if ret != nil {
			log.Fatalf("Unable to initialize NVML: %v", ret.Error())
		}
		devnum, err := nvml.GetDeviceCount()
		if err != nil {
			fmt.Println(err.Error())
		}
		fmt.Println("devnum=", devnum)*/
	go initmetrics()
	for {
		err := <-errchannel
		fmt.Println(err.Error())
	}

}
