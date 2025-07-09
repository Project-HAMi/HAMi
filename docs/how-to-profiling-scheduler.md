# How to profiling HAMi scheduler

## Prerequisite
### Enable profiling
- Add the `--profiling` flag to the `extraArgs` field of `scheduler.extender` in the Helm Chart to make pprof available via HTTP(s) server on `<POD_IP>`.
``` yaml
scheduler:
  ...
  extender:
    extraArgs:
      - --debug
      - -v=4
      - --profiling
```

### Prepare profiling environment
- [Install Go](https://go.dev/doc/install) on your system.
- Get HAMi [source code](https://github.com/Project-HAMi/HAMi) and place it at `/k8s-vgpu`
- Install dependencies by running:
``` shell
cd /k8s-vgpu 
make tidy 
go install github.com/NVIDIA/mig-parted/cmd/nvidia-mig-parted@v0.10.0
```

###  (Optional) Prepare profiling image
- Get HAMi source code
- Checkout the target version
- Building image
``` Dockerfile
FROM golang:1.24.4-bullseye  
ADD . /k8s-vgpu
RUN cd /k8s-vgpu && make tidy
RUN go install github.com/NVIDIA/mig-parted/cmd/nvidia-mig-parted@v0.10.0
```

## Profiling scheduler
**Note**: If HAMi source code and dependencies are correctly placed, you can use the `list` command in pprof to view source code. Otherwise, you may encounter a `no such file or directory` error.
For detailed information about pprof, refer to the [official documentation](https://pkg.go.dev/net/http/pprof) 

### CPU Profiling
Run the following command to capture a 120-second CPU profile:
```bash
go tool pprof --seconds 120 https+insecure://<POD_IP>/debug/pprof/profile`
```
Example output:
```bash
root@hami-pprof-76cfcb66f6-jpjnm:/# go tool pprof --seconds 120 https+insecure:://10.42.0.24/debug/pprof/profile
Fetching profile over HTTP from https+insecure://10.42.0.24/debug/pprof/profile?seconds=120
Please wait... (2m0s)
Saved profile in /root/pprof/pprof.scheduler.samples.cpu.002.pb.gz
File: scheduler
Type: cpu
Time: 2025-04-01 07:08:42 UTC
Duration: 120s, Total samples = 10ms (0.0083%)
Entering interactive mode (type "help" for commands, "o" for options)
(pprof) top
Showing nodes accounting for 10ms, 100% of 10ms total
Showing top 10 nodes out of 12
      flat  flat%   sum%        cum   cum%
      10ms   100%   100%       10ms   100%  sigs.k8s.io/json/internal/golang/encoding/json.unquoteBytes
         0     0%   100%       10ms   100%  k8s.io/apimachinery/pkg/runtime.Decode (inline)
         0     0%   100%       10ms   100%  k8s.io/apimachinery/pkg/runtime.WithoutVersionDecoder.Decode
         0     0%   100%       10ms   100%  k8s.io/apimachinery/pkg/runtime/serializer/json.(*Serializer).Decode
         0     0%   100%       10ms   100%  k8s.io/apimachinery/pkg/runtime/serializer/json.(*Serializer).unmarshal
         0     0%   100%       10ms   100%  k8s.io/apimachinery/pkg/watch.(*StreamWatcher).receive
         0     0%   100%       10ms   100%  k8s.io/client-go/rest/watch.(*Decoder).Decode
         0     0%   100%       10ms   100%  sigs.k8s.io/json.UnmarshalCaseSensitivePreserveInts (inline)
         0     0%   100%       10ms   100%  sigs.k8s.io/json/internal/golang/encoding/json.(*decodeState).object
         0     0%   100%       10ms   100%  sigs.k8s.io/json/internal/golang/encoding/json.(*decodeState).unmarshal
```

### Memory Profiling
To analyze memory usage, you have two options:
- Current live objects
```
go tool pprof https+insecure://<POD_IP>/debug/pprof/heap
```
- Cumulative allocation history
```
go tool pprof https+insecure://<POD_IP>/debug/pprof/allocs
```
Example output from the allocation profile:

```bash 
root@hami-scheduler-ffd687cb7-7gqm2:/# /usr/local/go/bin/go tool pprof --seconds 120 https+insecure://10.42.0.24/debug/pprof/allocs
Fetching profile over HTTP from https+insecure://10.42.0.24/debug/pprof/allocs?seconds=120
Saved profile in /root/pprof/pprof.scheduler.alloc_objects.alloc_space.inuse_objects.inuse_space.041.pb.gz
File: scheduler
Type: alloc_space
Time: 2025-04-01 07:03:05 UTC
Entering interactive mode (type "help" for commands, "o" for options)
(pprof) top
Showing nodes accounting for 4383.93MB, 69.18% of 6336.84MB total
Dropped 376 nodes (cum <= 31.68MB)
Showing top 10 nodes out of 164
      flat  flat%   sum%        cum   cum%
 1114.44MB 17.59% 17.59%  1114.94MB 17.59%  io.ReadAll
  980.52MB 15.47% 33.06%   980.52MB 15.47%  sync.(*Pool).pinSlow
  606.88MB  9.58% 42.64%   606.88MB  9.58%  golang.org/x/net/http2.init.func5
  357.15MB  5.64% 48.27%   357.15MB  5.64%  k8s.io/apimachinery/pkg/runtime.(*RawExtension).UnmarshalJSON
  293.20MB  4.63% 52.90%   293.20MB  4.63%  reflect.mapassign_faststr0
  265.58MB  4.19% 57.09%   265.58MB  4.19%  reflect.unsafe_NewArray
  234.07MB  3.69% 60.78%   461.59MB  7.28%  sigs.k8s.io/json/internal/golang/encoding/json.(*decodeState).literalStore
  210.54MB  3.32% 64.11%  3409.63MB 53.81%  github.com/Project-HAMi/HAMi/pkg/scheduler.(*Scheduler).RegisterFromNodeAnnotations
  162.02MB  2.56% 66.66%   331.76MB  5.24%  github.com/Project-HAMi/HAMi/pkg/scheduler.(*Scheduler).getNodesUsage
  159.52MB  2.52% 69.18%   225.53MB  3.56%  encoding/json.Unmarshal
(pprof) list RegisterFromNodeAnnotations
Total: 6.21GB
ROUTINE ======================== github.com/Project-HAMi/HAMi/pkg/scheduler.(*Scheduler).RegisterFromNodeAnnotations in /k8s-vgpu/pkg/scheduler/scheduler.go
  210.54MB     3.33GB (flat, cum) 53.73% of Total
         .          .    158:func (s *Scheduler) RegisterFromNodeAnnotations() {
         .          .    159:	klog.InfoS("Entering RegisterFromNodeAnnotations")
         .          .    160:	defer klog.InfoS("Exiting RegisterFromNodeAnnotations")
         .          .    161:	ticker := time.NewTicker(time.Second * 15)
         .          .    162:	defer ticker.Stop()
         .          .    163:	printedLog := map[string]bool{}
         .          .    164:	for {
  512.05kB   512.05kB    165:		select {
         .          .    166:		case <-s.nodeNotify:
         .          .    167:			klog.V(5).InfoS("Received node notification")
         .          .    168:		case <-ticker.C:
         .    46.84MB    169:			klog.InfoS("Ticker triggered")
         .          .    170:		case <-s.stopCh:
         .          .    171:			klog.InfoS("Received stop signal, exiting RegisterFromNodeAnnotations")
         .          .    172:			return
         .          .    173:		}
         .          .    174:		labelSelector := labels.Everything()
         .          .    175:		if len(config.NodeLabelSelector) > 0 {
         .          .    176:			labelSelector = (labels.Set)(config.NodeLabelSelector).AsSelector()
         .          .    177:			klog.InfoS("Using label selector", "selector", labelSelector.String())
         .          .    178:		}
         .          .    179:		rawNodes, err := s.nodeLister.List(labelSelector)
         .          .    180:		if err != nil {
         .          .    181:			klog.ErrorS(err, "Failed to list nodes with selector", "selector", labelSelector.String())
         .          .    182:			continue
         .          .    183:		}
       1MB        1MB    184:		klog.V(5).InfoS("Listed nodes", "nodeCount", len(rawNodes))
         .          .    185:		var nodeNames []string
         .          .    186:		for _, val := range rawNodes {
    1.50MB     1.50MB    187:			nodeNames = append(nodeNames, val.Name)
    5.50MB     5.50MB    188:			klog.V(5).InfoS("Processing node", "nodeName", val.Name)
         .          .    189:
         .          .    190:			for devhandsk, devInstance := range device.GetDevices() {
   36.50MB    36.50MB    191:				klog.V(5).InfoS("Checking device health", "nodeName", val.Name, "deviceVendor", devhandsk)
         .          .    192:
         .        4MB    193:				health, needUpdate := devInstance.CheckHealth(devhandsk, val)
   72.51MB    72.51MB    194:				klog.V(5).InfoS("Device health check result", "nodeName", val.Name, "deviceVendor", devhandsk, "health", health, "needUpdate", needUpdate)
         .          .    195:
         .          .    196:				if !health {
         .          .    197:					klog.Warning("Device is unhealthy, cleaning up node", "nodeName", val.Name, "deviceVendor", devhandsk)
         .          .    198:					err := devInstance.NodeCleanUp(val.Name)
         .          .    199:					if err != nil {
         .          .    200:						klog.ErrorS(err, "Node cleanup failed", "nodeName", val.Name, "deviceVendor", devhandsk)
         .          .    201:					}
         .          .    202:
         .          .    203:					info, ok := s.nodes[val.Name]
         .          .    204:					if ok {
         .          .    205:						klog.InfoS("Removing device from node", "nodeName", val.Name, "deviceVendor", devhandsk, "remainingDevices", s.nodes[val.Name].Devices)
         .          .    206:						s.rmNodeDevice(val.Name, info, devhandsk)
         .          .    207:					}
         .          .    208:					continue
         .          .    209:				}
         .          .    210:				if !needUpdate {
       8MB        8MB    211:					klog.V(5).InfoS("No update needed for device", "nodeName", val.Name, "deviceVendor", devhandsk)
         .          .    212:					continue
         .          .    213:				}
         .          .    214:				_, ok := util.HandshakeAnnos[devhandsk]
         .          .    215:				if ok {
    1.50MB     1.50MB    216:					tmppat := make(map[string]string)
    5.50MB     5.50MB    217:					tmppat[util.HandshakeAnnos[devhandsk]] = "Requesting_" + time.Now().Format(time.DateTime)
       2MB   203.15MB    218:					klog.InfoS("New timestamp for annotation", "nodeName", val.Name, "annotationKey", util.HandshakeAnnos[devhandsk], "annotationValue", tmppat[util.HandshakeAnnos[devhandsk]])
         .     1.25GB    219:					n, err := util.GetNode(val.Name)
         .          .    220:					if err != nil {
         .          .    221:						klog.ErrorS(err, "Failed to get node", "nodeName", val.Name)
         .          .    222:						continue
         .          .    223:					}
  512.03kB   512.03kB    224:					klog.V(5).InfoS("Patching node annotations", "nodeName", val.Name, "annotations", tmppat)
         .     1.21GB    225:					if err := util.PatchNodeAnnotations(n, tmppat); err != nil {
         .          .    226:						klog.ErrorS(err, "Failed to patch node annotations", "nodeName", val.Name)
         .          .    227:					}
         .          .    228:				}
   11.50MB    11.50MB    229:				nodeInfo := &util.NodeInfo{}
         .          .    230:				nodeInfo.ID = val.Name
         .          .    231:				nodeInfo.Node = val
   24.50MB    24.50MB    232:				klog.V(5).InfoS("Fetching node devices", "nodeName", val.Name, "deviceVendor", devhandsk)
         .    78.03MB    233:				nodedevices, err := devInstance.GetNodeDevices(*val)
         .          .    234:				if err != nil {
         .          .    235:					klog.ErrorS(err, "Failed to get node devices", "nodeName", val.Name, "deviceVendor", devhandsk)
         .          .    236:					continue
         .          .    237:				}
         .          .    238:				nodeInfo.Devices = make([]util.DeviceInfo, 0)
         .          .    239:				for _, deviceinfo := range nodedevices {
   36.03MB    36.03MB    240:					nodeInfo.Devices = append(nodeInfo.Devices, *deviceinfo)
         .          .    241:				}
         .    21.52MB    242:				s.addNode(val.Name, nodeInfo)
         .          .    243:				if s.nodes[val.Name] != nil && len(nodeInfo.Devices) > 0 {
         .          .    244:					if printedLog[val.Name] {
    3.50MB     3.50MB    245:						klog.V(5).InfoS("Node device updated", "nodeName", val.Name, "deviceVendor", devhandsk, "nodeInfo", nodeInfo, "totalDevices", s.nodes[val.Name].Devices)
         .          .    246:					} else {
         .          .    247:						klog.InfoS("Node device added", "nodeName", val.Name, "deviceVendor", devhandsk, "nodeInfo", nodeInfo, "totalDevices", s.nodes[val.Name].Devices)
         .          .    248:						printedLog[val.Name] = true
         .          .    249:					}
         .          .    250:				}
         .          .    251:			}
         .          .    252:		}
         .   332.76MB    253:		_, _, err = s.getNodesUsage(&nodeNames, nil)
         .          .    254:		if err != nil {
         .          .    255:			klog.ErrorS(err, "Failed to get node usage", "nodeNames", nodeNames)
         .          .    256:		}
         .          .    257:	}
         .          .    258:}
```
