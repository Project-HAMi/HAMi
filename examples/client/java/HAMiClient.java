/**
 * HAMi Java Client Example
 * 
 * This is an example client library for interacting with HAMi 
 * (Heterogeneous AI Computing Virtualization Middleware).
 * 
 * HAMi integrates with Kubernetes to manage heterogeneous devices like GPUs, NPUs, etc.
 * 
 * Requirements:
 * - Kubernetes Java Client: io.kubernetes:client-java
 * - Maven or Gradle for dependency management
 * 
 * Usage:
 *     javac -cp kubernetes-client.jar HAMiClient.java
 *     java -cp .:kubernetes-client.jar HAMiClient
 */

package io.hami.client;

import io.kubernetes.client.openapi.ApiClient;
import io.kubernetes.client.openapi.ApiException;
import io.kubernetes.client.openapi.Configuration;
import io.kubernetes.client.openapi.apis.CoreV1Api;
import io.kubernetes.client.openapi.models.*;
import io.kubernetes.client.util.Config;

import java.io.IOException;
import java.util.*;
import java.util.stream.Collectors;

/**
 * Client for interacting with HAMi through Kubernetes API.
 */
public class HAMiClient {
    
    private final CoreV1Api coreV1Api;
    
    /**
     * Initialize HAMi client with default configuration.
     * 
     * @throws IOException if kubeconfig cannot be loaded
     */
    public HAMiClient() throws IOException {
        ApiClient client = Config.defaultClient();
        Configuration.setDefaultApiClient(client);
        this.coreV1Api = new CoreV1Api();
    }
    
    /**
     * Initialize HAMi client with custom API client.
     * 
     * @param apiClient Custom Kubernetes API client
     */
    public HAMiClient(ApiClient apiClient) {
        Configuration.setDefaultApiClient(apiClient);
        this.coreV1Api = new CoreV1Api();
    }
    
    /**
     * List all nodes with HAMi GPU support.
     * 
     * @param labelSelector Label selector to filter nodes (default: "gpu=on")
     * @return List of node information
     * @throws ApiException if API call fails
     */
    public List<NodeInfo> listGPUNodes(String labelSelector) throws ApiException {
        if (labelSelector == null) {
            labelSelector = "gpu=on";
        }
        
        V1NodeList nodeList = coreV1Api.listNode(
            null, null, null, null, labelSelector, 
            null, null, null, null, null, null
        );
        
        List<NodeInfo> result = new ArrayList<>();
        for (V1Node node : nodeList.getItems()) {
            NodeInfo nodeInfo = new NodeInfo();
            nodeInfo.name = node.getMetadata().getName();
            nodeInfo.labels = node.getMetadata().getLabels();
            nodeInfo.annotations = node.getMetadata().getAnnotations();
            nodeInfo.devices = parseDeviceAnnotations(nodeInfo.annotations);
            result.add(nodeInfo);
        }
        
        System.out.println("Found " + result.size() + " GPU nodes");
        return result;
    }
    
    /**
     * Parse HAMi device annotations from node.
     * 
     * @param annotations Node annotations
     * @return Map of device information by device type
     */
    private Map<String, DeviceInfo> parseDeviceAnnotations(Map<String, String> annotations) {
        Map<String, DeviceInfo> devices = new HashMap<>();
        
        if (annotations == null) {
            return devices;
        }
        
        for (Map.Entry<String, String> entry : annotations.entrySet()) {
            String key = entry.getKey();
            String value = entry.getValue();
            
            // Parse device registration annotations
            if (key.startsWith("hami.io/node-") && key.endsWith("-register")) {
                String deviceType = key.replace("hami.io/node-", "")
                                      .replace("-register", "");
                DeviceInfo deviceInfo = new DeviceInfo();
                deviceInfo.type = deviceType;
                deviceInfo.devices = parseDeviceString(value);
                devices.put(deviceType, deviceInfo);
            }
            
            // Include handshake information
            if (key.startsWith("hami.io/node-handshake-")) {
                String deviceType = key.replace("hami.io/node-handshake-", "");
                DeviceInfo deviceInfo = devices.getOrDefault(deviceType, new DeviceInfo());
                deviceInfo.handshake = value;
                devices.put(deviceType, deviceInfo);
            }
        }
        
        return devices;
    }
    
    /**
     * Parse device registration string.
     * Format: {Device UUID},{split count},{memory},{cores},{type},{numa},{healthy}:...
     * 
     * @param deviceStr Device registration string
     * @return List of device details
     */
    private List<Device> parseDeviceString(String deviceStr) {
        List<Device> devices = new ArrayList<>();
        
        if (deviceStr == null || deviceStr.isEmpty()) {
            return devices;
        }
        
        deviceStr = deviceStr.replaceAll(":+$", "");
        String[] deviceInfos = deviceStr.split(":");
        
        for (String deviceInfo : deviceInfos) {
            if (deviceInfo.isEmpty()) {
                continue;
            }
            
            String[] parts = deviceInfo.split(",");
            if (parts.length >= 7) {
                Device device = new Device();
                device.uuid = parts[0];
                device.splitCount = Integer.parseInt(parts[1]);
                device.memoryMB = Integer.parseInt(parts[2]);
                device.cores = Integer.parseInt(parts[3]);
                device.type = parts[4];
                device.numa = Integer.parseInt(parts[5]);
                device.healthy = Boolean.parseBoolean(parts[6]);
                devices.add(device);
            }
        }
        
        return devices;
    }
    
    /**
     * Get device information for a specific node.
     * 
     * @param nodeName Name of the node
     * @return Map of device information
     * @throws ApiException if API call fails
     */
    public Map<String, DeviceInfo> getNodeDevices(String nodeName) throws ApiException {
        V1Node node = coreV1Api.readNode(nodeName, null);
        Map<String, String> annotations = node.getMetadata().getAnnotations();
        return parseDeviceAnnotations(annotations);
    }
    
    /**
     * Create a pod with GPU resources.
     * 
     * @param podName Name of the pod
     * @param namespace Kubernetes namespace
     * @param image Container image
     * @param gpuCount Number of GPUs to request
     * @param gpuMemoryMB GPU memory in MB (optional, use null to skip)
     * @param gpuCores GPU core percentage (optional, use null to skip)
     * @param command Container command (optional)
     * @return Created pod information
     * @throws ApiException if API call fails
     */
    public PodInfo createGPUPod(String podName, String namespace, String image,
                                int gpuCount, Integer gpuMemoryMB, 
                                Integer gpuCores, List<String> command) throws ApiException {
        // Build resource limits
        Map<String, io.kubernetes.client.custom.Quantity> limits = new HashMap<>();
        limits.put("nvidia.com/gpu", new io.kubernetes.client.custom.Quantity(String.valueOf(gpuCount)));
        
        if (gpuMemoryMB != null) {
            limits.put("nvidia.com/gpumem", new io.kubernetes.client.custom.Quantity(String.valueOf(gpuMemoryMB)));
        }
        
        if (gpuCores != null) {
            limits.put("nvidia.com/gpucores", new io.kubernetes.client.custom.Quantity(String.valueOf(gpuCores)));
        }
        
        // Create container
        V1Container container = new V1Container()
            .name(podName)
            .image(image)
            .command(command)
            .resources(new V1ResourceRequirements().limits(limits));
        
        // Create pod spec
        V1PodSpec podSpec = new V1PodSpec()
            .containers(Collections.singletonList(container))
            .restartPolicy("Never");
        
        // Create pod
        V1Pod pod = new V1Pod()
            .apiVersion("v1")
            .kind("Pod")
            .metadata(new V1ObjectMeta().name(podName))
            .spec(podSpec);
        
        V1Pod createdPod = coreV1Api.createNamespacedPod(namespace, pod, null, null, null, null);
        System.out.println("Created pod " + podName + " in namespace " + namespace);
        
        return podToInfo(createdPod);
    }
    
    /**
     * Get HAMi scheduling information for a pod.
     * 
     * @param podName Name of the pod
     * @param namespace Kubernetes namespace
     * @return Scheduling information
     * @throws ApiException if API call fails
     */
    public SchedulingInfo getPodSchedulingInfo(String podName, String namespace) throws ApiException {
        V1Pod pod = coreV1Api.readNamespacedPod(podName, namespace, null);
        Map<String, String> annotations = pod.getMetadata().getAnnotations();
        
        if (annotations == null) {
            annotations = new HashMap<>();
        }
        
        SchedulingInfo info = new SchedulingInfo();
        info.devicesToAllocate = annotations.getOrDefault("hami.io/devices-to-allocate", "");
        info.node = annotations.getOrDefault("hami.io/vgpu-node", "");
        info.scheduleTime = annotations.getOrDefault("hami.io/vgpu-time", "");
        info.podPhase = pod.getStatus().getPhase();
        info.nodeName = pod.getSpec().getNodeName();
        
        // Parse device allocation
        if (!info.devicesToAllocate.isEmpty()) {
            info.allocatedDevices = parseDeviceAllocation(info.devicesToAllocate);
        }
        
        return info;
    }
    
    /**
     * Parse device allocation string.
     * Format: {device UUID},{type},{memory},{cores}:...
     * 
     * @param allocationStr Device allocation string
     * @return List of allocated devices
     */
    private List<AllocatedDevice> parseDeviceAllocation(String allocationStr) {
        List<AllocatedDevice> devices = new ArrayList<>();
        
        if (allocationStr == null || allocationStr.isEmpty()) {
            return devices;
        }
        
        allocationStr = allocationStr.replaceAll(":+$", "");
        String[] deviceInfos = allocationStr.split(":");
        
        for (String deviceInfo : deviceInfos) {
            if (deviceInfo.isEmpty()) {
                continue;
            }
            
            String[] parts = deviceInfo.split(",");
            if (parts.length >= 4) {
                AllocatedDevice device = new AllocatedDevice();
                device.uuid = parts[0];
                device.type = parts[1];
                device.memoryMB = Integer.parseInt(parts[2]);
                device.cores = Integer.parseInt(parts[3]);
                devices.add(device);
            }
        }
        
        return devices;
    }
    
    /**
     * Delete a pod.
     * 
     * @param podName Name of the pod
     * @param namespace Kubernetes namespace
     * @throws ApiException if API call fails
     */
    public void deletePod(String podName, String namespace) throws ApiException {
        coreV1Api.deleteNamespacedPod(
            podName, namespace, null, null, 
            null, null, null, null
        );
        System.out.println("Deleted pod " + podName + " from namespace " + namespace);
    }
    
    /**
     * Convert pod object to info.
     */
    private PodInfo podToInfo(V1Pod pod) {
        PodInfo info = new PodInfo();
        info.name = pod.getMetadata().getName();
        info.namespace = pod.getMetadata().getNamespace();
        info.uid = pod.getMetadata().getUid();
        info.phase = pod.getStatus().getPhase();
        info.nodeName = pod.getSpec().getNodeName();
        return info;
    }
    
    // Data classes
    
    public static class NodeInfo {
        public String name;
        public Map<String, String> labels;
        public Map<String, String> annotations;
        public Map<String, DeviceInfo> devices;
        
        @Override
        public String toString() {
            return "NodeInfo{name='" + name + "', devices=" + devices + "}";
        }
    }
    
    public static class DeviceInfo {
        public String type;
        public String handshake;
        public List<Device> devices;
        
        @Override
        public String toString() {
            return "DeviceInfo{type='" + type + "', devices=" + devices + "}";
        }
    }
    
    public static class Device {
        public String uuid;
        public int splitCount;
        public int memoryMB;
        public int cores;
        public String type;
        public int numa;
        public boolean healthy;
        
        @Override
        public String toString() {
            return "Device{uuid='" + uuid + "', memory=" + memoryMB + "MB, cores=" + cores + "%, healthy=" + healthy + "}";
        }
    }
    
    public static class PodInfo {
        public String name;
        public String namespace;
        public String uid;
        public String phase;
        public String nodeName;
        
        @Override
        public String toString() {
            return "PodInfo{name='" + name + "', namespace='" + namespace + "', phase='" + phase + "'}";
        }
    }
    
    public static class SchedulingInfo {
        public String devicesToAllocate;
        public String node;
        public String scheduleTime;
        public String podPhase;
        public String nodeName;
        public List<AllocatedDevice> allocatedDevices;
        
        @Override
        public String toString() {
            return "SchedulingInfo{node='" + node + "', allocatedDevices=" + allocatedDevices + "}";
        }
    }
    
    public static class AllocatedDevice {
        public String uuid;
        public String type;
        public int memoryMB;
        public int cores;
        
        @Override
        public String toString() {
            return "AllocatedDevice{uuid='" + uuid + "', type='" + type + "', memory=" + memoryMB + "MB, cores=" + cores + "%}";
        }
    }
    
    /**
     * Example usage of HAMi client.
     */
    public static void main(String[] args) {
        try {
            // Initialize client
            HAMiClient hami = new HAMiClient();
            
            // List GPU nodes
            System.out.println("\n=== GPU Nodes ===");
            List<NodeInfo> nodes = hami.listGPUNodes("gpu=on");
            for (NodeInfo node : nodes) {
                System.out.println(node);
            }
            
            // Example: Create a GPU pod
            System.out.println("\n=== Creating GPU Pod ===");
            PodInfo podInfo = hami.createGPUPod(
                "hami-example-pod",
                "default",
                "nvidia/cuda:11.0-base",
                1,           // GPU count
                3000,        // GPU memory MB
                50,          // GPU cores %
                Arrays.asList("nvidia-smi")
            );
            System.out.println(podInfo);
            
            // Wait for scheduling
            Thread.sleep(2000);
            
            // Get scheduling info
            System.out.println("\n=== Pod Scheduling Info ===");
            SchedulingInfo schedulingInfo = hami.getPodSchedulingInfo("hami-example-pod", "default");
            System.out.println(schedulingInfo);
            
            // Clean up
            System.out.println("\n=== Cleaning Up ===");
            hami.deletePod("hami-example-pod", "default");
            
        } catch (Exception e) {
            System.err.println("Example failed: " + e.getMessage());
            e.printStackTrace();
        }
    }
}
