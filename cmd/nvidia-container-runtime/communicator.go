package main

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	pb1 "4pd.io/k8s-vgpu/pkg/api"
	"google.golang.org/grpc"
)

func GetNvidiaUUID(r nvidiaContainerRuntime, ctrenv []string) (map[string]string, []string, error) {

	socketpath := ""
	// Set up a connection to the server.
	for _, val := range ctrenv {
		if strings.Contains(val, pb1.PluginRuntimeSocket) {
			if strings.Contains(val, "=") {
				socketpath = strings.Split(val, "=")[1]
			}
		}
	}
	envmap := make(map[string]string)
	if len(socketpath) == 0 {
		return envmap, []string{}, errors.New("nvidia not found")
	}
	r.logger.Println("Connect to address", socketpath)
	conn, err := grpc.Dial(socketpath, grpc.WithInsecure(), grpc.WithBlock(), grpc.WithTimeout(time.Second*2))
	if err != nil {
		r.logger.Println("did not connect:", err)
		return envmap, []string{}, errors.New("Dial not established")
	}
	defer conn.Close()
	r.logger.Println("Connect Established")
	//c := pb.NewGreeterClient(conn)

	c1 := pb1.NewVGPURuntimeServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tmpa := []string{}
	for _, val := range ctrenv {
		if strings.Contains(val, pb1.ContainerUID) {
			values := strings.Split(val, "=")[1]
			r.logger.Println("values=", val, values)
			r1, err := c1.GetDevice(ctx, &pb1.GetDeviceRequest{CtrUUID: values})
			if err != nil {
				log.Fatalf("could not greet: %v", err)
			}
			envmap = r1.GetEnvs()

			tmpa = append(tmpa, r1.GetPodUID())
			tmpa[0] = tmpa[0] + "_" + r1.GetCtrName()
			r.logger.Println("meta=", tmpa)
			r.logger.Println("env=", envmap)
		}
	}
	return envmap, tmpa, nil
	//return []string{}, []string{}, errors.New("aaaa")
}
