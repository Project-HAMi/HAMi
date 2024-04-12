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

// Kubernetes (k8s) device plugin to enable registration of AMD GPU to a container cluster
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Project-HAMi/HAMi/pkg/device-plugin/hygon/dcu"

	"github.com/golang/glog"
	"github.com/kubevirt/device-plugin-manager/pkg/dpm"
)

// Lister serves as an interface between imlementation and Manager machinery. User passes
// implementation of this interface to NewManager function. Manager will use it to obtain resource
// namespace, monitor available resources and instantate a new plugin for them.
type Lister struct {
	ResUpdateChan chan dpm.PluginNameList
	Heartbeat     chan bool
}

// GetResourceNamespace must return namespace (vendor ID) of implemented Lister. e.g. for
// resources in format "color.example.com/<color>" that would be "color.example.com".
func (l *Lister) GetResourceNamespace() string {
	return "hygon.com"
}

// Discover notifies manager with a list of currently available resources in its namespace.
// e.g. if "color.example.com/red" and "color.example.com/blue" are available in the system,
// it would pass PluginNameList{"red", "blue"} to given channel. In case list of
// resources is static, it would use the channel only once and then return. In case the list is
// dynamic, it could block and pass a new list each times resources changed. If blocking is
// used, it should check whether the channel is closed, i.e. Discover should stop.
func (l *Lister) Discover(pluginListCh chan dpm.PluginNameList) {
	for {
		select {
		case newResourcesList := <-l.ResUpdateChan: // New resources found
			pluginListCh <- newResourcesList
		case <-pluginListCh: // Stop message received
			// Stop resourceUpdateCh
			return
		}
	}
}

// NewPlugin instantiates a plugin implementation. It is given the last name of the resource,
// e.g. for resource name "color.example.com/red" that would be "red". It must return valid
// implementation of a PluginInterface.
func (l *Lister) NewPlugin(resourceLastName string) dpm.PluginInterface {
	return &dcu.Plugin{
		Heartbeat: l.Heartbeat,
	}
}

var gitDescribe string

func main() {
	versions := [...]string{
		"DCU device plugin for Kubernetes",
		fmt.Sprintf("%s version %s", os.Args[0], gitDescribe),
	}

	flag.Usage = func() {
		for _, v := range versions {
			fmt.Fprintf(os.Stderr, "%s\n", v)
		}
		fmt.Fprintln(os.Stderr, "Usage:")
		flag.PrintDefaults()
	}
	var pulse int
	flag.IntVar(&pulse, "pulse", 0, "time between health check polling in seconds.  Set to 0 to disable.")
	// this is also needed to enable glog usage in dpm
	flag.Parse()

	for _, v := range versions {
		glog.Infof("%s", v)
	}

	l := Lister{
		ResUpdateChan: make(chan dpm.PluginNameList),
		Heartbeat:     make(chan bool),
	}
	manager := dpm.NewManager(&l)

	if pulse > 0 {
		go func() {
			glog.Infof("Heart beating every %d seconds", pulse)
			for {
				time.Sleep(time.Second * time.Duration(pulse))
				l.Heartbeat <- true
			}
		}()
	}

	go func() {
		// /sys/class/kfd only exists if ROCm kernel/driver is installed
		var path = "/sys/class/kfd"
		if _, err := os.Stat(path); err == nil {
			l.ResUpdateChan <- []string{"dcunum"}
		}
	}()
	manager.Run()

}
