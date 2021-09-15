package main

import (
	"bytes"
	"context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	client "github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
)

func GetDockerClient() *client.Client {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		panic(err)
	}
	return cli
}

func InspecctContainer(ctx context.Context, ctrname string, logger *log.Logger) {
	client := GetDockerClient()
	json, err := client.ContainerInspect(ctx, ctrname)
	if err != nil {
		logger.Println("err=", err.Error())
	}
	logger.Println("LABELS=", json.Config.Labels)
}

func RunContainer(ctx context.Context, image string, cmd []string, env []string, mount []mount.Mount, shmSize int64) string {
	client := GetDockerClient()
	resp, err := client.ContainerCreate(ctx, &container.Config{
		Image: image,
		Cmd:   cmd,
		Env:   env,
	}, &container.HostConfig{
		Mounts:  mount,
		ShmSize: shmSize,
	}, nil, nil, "")
	if err != nil {
		panic(err)
	}
	if err := client.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		panic(err)
	}
	statusCh, errCh := client.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			panic(err)
		}
	case <-statusCh:
	}

	out, err := client.ContainerLogs(ctx, resp.ID, types.ContainerLogsOptions{ShowStdout: true})
	if err != nil {
		panic(err)
	}
	buf := new(bytes.Buffer)
	buf.ReadFrom(out)
	return buf.String()
}
