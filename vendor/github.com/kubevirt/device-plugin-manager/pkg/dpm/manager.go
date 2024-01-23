package dpm

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/golang/glog"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// TODO: Make plugin server start retries configurable.
const (
	startPluginServerRetries   = 3
	startPluginServerRetryWait = 3 * time.Second
)

// Manager contains the main machinery of this framework. It uses user defined lister to monitor
// available resources and start/stop plugins accordingly. It also handles system signals and
// unexpected kubelet events.
type Manager struct {
	lister ListerInterface
}

// NewManager is the canonical way of initializing Manager. User must provide ListerInterface
// implementation. Lister will provide information about handled resources, monitor their
// availability and provide method to spawn plugins that will handle found resources.
func NewManager(lister ListerInterface) *Manager {
	dpm := &Manager{
		lister: lister,
	}
	return dpm
}

// Run starts the Manager. It sets up the infrastructure and handles system signals, Kubelet socket
// watch and monitoring of available resources as well as starting and stoping of plugins.
func (dpm *Manager) Run() {
	glog.V(3).Info("Starting device plugin manager")

	// First important signal channel is the os signal channel. We only care about (somewhat) small
	// subset of available signals.
	glog.V(3).Info("Registering for system signal notifications")
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT)

	// The other important channel is filesystem notification channel, responsible for watching
	// device plugin directory.
	glog.V(3).Info("Registering for notifications of filesystem changes in device plugin directory")
	fsWatcher, _ := fsnotify.NewWatcher()
	defer fsWatcher.Close()
	fsWatcher.Add(pluginapi.DevicePluginPath)

	// Create list of running plugins and start Discover method of given lister. This method is
	// responsible of notifying manager about changes in available plugins.
	var pluginMap = make(map[string]devicePlugin)
	glog.V(3).Info("Starting Discovery on new plugins")
	pluginsCh := make(chan PluginNameList)
	defer close(pluginsCh)
	go dpm.lister.Discover(pluginsCh)

	// Finally start a loop that will handle messages from opened channels.
	glog.V(3).Info("Handling incoming signals")
HandleSignals:
	for {
		select {
		case newPluginsList := <-pluginsCh:
			glog.V(3).Infof("Received new list of plugins: %s", newPluginsList)
			dpm.handleNewPlugins(pluginMap, newPluginsList)
		case event := <-fsWatcher.Events:
			if event.Name == pluginapi.KubeletSocket {
				glog.V(3).Infof("Received kubelet socket event: %s", event)
				if event.Op&fsnotify.Create == fsnotify.Create {
					dpm.startPluginServers(pluginMap)
				}
				// TODO: Kubelet doesn't really clean-up it's socket, so this is currently
				// manual-testing thing. Could we solve Kubelet deaths better?
				if event.Op&fsnotify.Remove == fsnotify.Remove {
					dpm.stopPluginServers(pluginMap)
				}
			}
		case s := <-signalCh:
			switch s {
			case syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT:
				glog.V(3).Infof("Received signal \"%v\", shutting down", s)
				dpm.stopPlugins(pluginMap)
				break HandleSignals
			}
		}
	}
}

func (dpm *Manager) handleNewPlugins(currentPluginsMap map[string]devicePlugin, newPluginsList PluginNameList) {
	var wg sync.WaitGroup
	var pluginMapMutex = &sync.Mutex{}

	// This map is used for faster searches when removing old plugins
	newPluginsSet := make(map[string]bool)

	// Add new plugins
	for _, newPluginLastName := range newPluginsList {
		newPluginsSet[newPluginLastName] = true
		wg.Add(1)
		go func(name string) {
			if _, ok := currentPluginsMap[name]; !ok {
				// add new plugin only if it doesn't already exist
				glog.V(3).Infof("Adding a new plugin \"%s\"", name)
				plugin := newDevicePlugin(dpm.lister.GetResourceNamespace(), name, dpm.lister.NewPlugin(name))
				startPlugin(name, plugin)
				pluginMapMutex.Lock()
				currentPluginsMap[name] = plugin
				pluginMapMutex.Unlock()
			}
			wg.Done()
		}(newPluginLastName)
	}
	wg.Wait()

	// Remove old plugins
	for pluginLastName, currentPlugin := range currentPluginsMap {
		wg.Add(1)
		go func(name string, plugin devicePlugin) {
			if _, found := newPluginsSet[name]; !found {
				glog.V(3).Infof("Remove unused plugin \"%s\"", name)
				stopPlugin(name, plugin)
				pluginMapMutex.Lock()
				delete(currentPluginsMap, name)
				pluginMapMutex.Unlock()
			}
			wg.Done()
		}(pluginLastName, currentPlugin)
	}
	wg.Wait()
}

func (dpm *Manager) startPluginServers(pluginMap map[string]devicePlugin) {
	var wg sync.WaitGroup

	for pluginLastName, currentPlugin := range pluginMap {
		wg.Add(1)
		go func(name string, plugin devicePlugin) {
			startPluginServer(name, plugin)
			wg.Done()
		}(pluginLastName, currentPlugin)
	}
	wg.Wait()
}

func (dpm *Manager) stopPluginServers(pluginMap map[string]devicePlugin) {
	var wg sync.WaitGroup

	for pluginLastName, currentPlugin := range pluginMap {
		wg.Add(1)
		go func(name string, plugin devicePlugin) {
			stopPluginServer(name, plugin)
			wg.Done()
		}(pluginLastName, currentPlugin)
	}
	wg.Wait()
}

func (dpm *Manager) stopPlugins(pluginMap map[string]devicePlugin) {
	var wg sync.WaitGroup
	var pluginMapMutex = &sync.Mutex{}

	for pluginLastName, currentPlugin := range pluginMap {
		wg.Add(1)
		go func(name string, plugin devicePlugin) {
			stopPlugin(name, plugin)
			pluginMapMutex.Lock()
			delete(pluginMap, name)
			pluginMapMutex.Unlock()
			wg.Done()
		}(pluginLastName, currentPlugin)
	}
	wg.Wait()
}

func startPlugin(pluginLastName string, plugin devicePlugin) {
	var err error
	if devicePluginImpl, ok := plugin.DevicePluginImpl.(PluginInterfaceStart); ok {
		err = devicePluginImpl.Start()
		if err != nil {
			glog.Errorf("Failed to start plugin \"%s\": %s", pluginLastName, err)
		}
	}
	if err == nil {
		startPluginServer(pluginLastName, plugin)
	}
}

func stopPlugin(pluginLastName string, plugin devicePlugin) {
	stopPluginServer(pluginLastName, plugin)
	if devicePluginImpl, ok := plugin.DevicePluginImpl.(PluginInterfaceStop); ok {
		err := devicePluginImpl.Stop()
		if err != nil {
			glog.Errorf("Failed to stop plugin \"%s\": %s", pluginLastName, err)
		}
	}
}

func startPluginServer(pluginLastName string, plugin devicePlugin) {
	for i := 1; i <= startPluginServerRetries; i++ {
		err := plugin.StartServer()
		if err == nil {
			return
		} else if i == startPluginServerRetries {
			glog.V(3).Infof("Failed to start plugin's \"%s\" server, within given %d tries: %s",
				pluginLastName, startPluginServerRetries, err)
		} else {
			glog.Errorf("Failed to start plugin's \"%s\" server, atempt %d ouf of %d waiting %d before next try: %s",
				pluginLastName, i, startPluginServerRetries, startPluginServerRetryWait, err)
			time.Sleep(startPluginServerRetryWait)
		}
	}
}

func stopPluginServer(pluginLastName string, plugin devicePlugin) {
	err := plugin.StopServer()
	if err != nil {
		glog.Errorf("Failed to stop plugin's \"%s\" server: %s", pluginLastName, err)
	}
}
