package main

import (
	"flag"
	"fmt"
)

var addr = flag.String("listen-address", ":9394", "The address to listen on for HTTP requests.")

const shared_directory = "/usr/local/vgpu/shared"

func main() {
	errchannel := make(chan error)
	//go serveinfo(errchannel)
	go initmetrics()
	for {
		err := <-errchannel
		fmt.Println(err.Error())
	}

}
