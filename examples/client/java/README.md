# HAMi Java Client Example

This directory contains a Java client library example for interacting with HAMi (Heterogeneous AI Computing Virtualization Middleware).

## Overview

The HAMi Java client provides a high-level interface for:
- Querying GPU/NPU nodes and their device information
- Creating pods with HAMi device resource requests
- Monitoring HAMi scheduling decisions
- Managing device allocations

## Prerequisites

- Java 8 or higher
- Access to a Kubernetes cluster with HAMi installed
- Kubernetes Java client library

## Dependencies

### Maven

Add the following dependency to your `pom.xml`:

```xml
<dependency>
    <groupId>io.kubernetes</groupId>
    <artifactId>client-java</artifactId>
    <version>18.0.0</version>
</dependency>
```

### Gradle

Add the following dependency to your `build.gradle`:

```gradle
dependencies {
    implementation 'io.kubernetes:client-java:18.0.0'
}
```

## Building

### Using Maven

Create a `pom.xml` file:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 
         http://maven.apache.org/xsd/maven-4.0.0.xsd">
    <modelVersion>4.0.0</modelVersion>

    <groupId>io.hami</groupId>
    <artifactId>hami-client-example</artifactId>
    <version>1.0-SNAPSHOT</version>

    <properties>
        <maven.compiler.source>8</maven.compiler.source>
        <maven.compiler.target>8</maven.compiler.target>
    </properties>

    <dependencies>
        <dependency>
            <groupId>io.kubernetes</groupId>
            <artifactId>client-java</artifactId>
            <version>18.0.0</version>
        </dependency>
    </dependencies>

    <build>
        <plugins>
            <plugin>
                <groupId>org.apache.maven.plugins</groupId>
                <artifactId>maven-compiler-plugin</artifactId>
                <version>3.8.1</version>
            </plugin>
        </plugins>
    </build>
</project>
```

Build and run:

```bash
mvn clean package
mvn exec:java -Dexec.mainClass="io.hami.client.HAMiClient"
```

### Using Gradle

Create a `build.gradle` file:

```gradle
plugins {
    id 'java'
    id 'application'
}

group 'io.hami'
version '1.0-SNAPSHOT'

repositories {
    mavenCentral()
}

dependencies {
    implementation 'io.kubernetes:client-java:18.0.0'
}

application {
    mainClassName = 'io.hami.client.HAMiClient'
}

java {
    sourceCompatibility = JavaVersion.VERSION_1_8
    targetCompatibility = JavaVersion.VERSION_1_8
}
```

Build and run:

```bash
gradle build
gradle run
```

## Usage

### Basic Example

```java
import io.hami.client.HAMiClient;

// Initialize the client
HAMiClient hami = new HAMiClient();

// List all GPU nodes
List<HAMiClient.NodeInfo> nodes = hami.listGPUNodes("gpu=on");
for (HAMiClient.NodeInfo node : nodes) {
    System.out.println("Node: " + node.name);
    System.out.println("Devices: " + node.devices);
}
```

### Creating a GPU Pod

```java
// Create a pod with GPU resources
HAMiClient.PodInfo podInfo = hami.createGPUPod(
    "my-gpu-pod",                  // Pod name
    "default",                     // Namespace
    "nvidia/cuda:11.0-base",       // Image
    1,                             // GPU count
    3000,                          // GPU memory in MB
    50,                            // GPU core percentage
    Arrays.asList("nvidia-smi")    // Command
);
```

### Querying Device Information

```java
// Get devices for a specific node
Map<String, HAMiClient.DeviceInfo> devices = hami.getNodeDevices("node-1");
System.out.println(devices);

// Get scheduling information for a pod
HAMiClient.SchedulingInfo schedulingInfo = 
    hami.getPodSchedulingInfo("my-gpu-pod", "default");
System.out.println(schedulingInfo);
```

## Resource Specifications

When creating pods, you can specify the following GPU resource limits:

- `gpuCount`: Number of physical GPUs (required)
- `gpuMemoryMB`: GPU memory in MB (optional, use `null` to skip)
- `gpuCores`: GPU core percentage (0-100) (optional, use `null` to skip)

Example:
```java
hami.createGPUPod(
    "gpu-training-job",
    "ml-team",
    "tensorflow/tensorflow:latest-gpu",
    2,          // Request 2 GPUs
    8000,       // 8GB memory per GPU
    100,        // 100% cores (full GPU)
    null        // No command
);
```

## Understanding HAMi Annotations

HAMi uses Kubernetes annotations to store device information and scheduling decisions.

### Node Annotations

Nodes with HAMi devices have annotations like:
- `hami.io/node-handshake-nvidia`: Handshake timestamp
- `hami.io/node-nvidia-register`: Device registration information

### Pod Annotations

Scheduled pods have annotations like:
- `hami.io/devices-to-allocate`: Allocated device information
- `hami.io/vgpu-node`: Scheduled node name
- `hami.io/vgpu-time`: Scheduling timestamp

## Authentication

The client uses the Kubernetes Java client library, which supports multiple authentication methods:

1. **In-cluster configuration**: Automatically used when running inside a Kubernetes pod
2. **Kubeconfig file**: Uses `~/.kube/config` by default
3. **Custom configuration**: Create a custom `ApiClient`

```java
import io.kubernetes.client.openapi.ApiClient;
import io.kubernetes.client.util.ClientBuilder;
import io.kubernetes.client.util.KubeConfig;

// Use custom kubeconfig
ApiClient client = ClientBuilder.kubeconfig(
    KubeConfig.loadKubeConfig(new FileReader("/path/to/kubeconfig"))
).build();

HAMiClient hami = new HAMiClient(client);
```

## Supported Device Types

HAMi supports various heterogeneous devices:
- NVIDIA GPUs (`nvidia`)
- Cambricon MLU (`mlu`)
- HYGON DCU (`dcu`)
- Iluvatar CoreX GPU (`iluvatar`)
- Moore Threads GPU (`mthreads`)
- HUAWEI Ascend NPU (`ascend`)
- And more...

The client automatically detects and parses information for all supported device types.

## Error Handling

All methods throw `ApiException` on errors. Wrap calls in try-catch blocks:

```java
try {
    HAMiClient.PodInfo podInfo = hami.createGPUPod(...);
} catch (ApiException e) {
    System.err.println("Failed to create pod: " + e.getMessage());
    e.printStackTrace();
}
```

## Advanced Usage

### Custom Pod Specifications

For more complex pod configurations, you can use the Kubernetes Java client directly:

```java
import io.kubernetes.client.openapi.models.*;
import io.kubernetes.client.custom.Quantity;

// Create a custom pod with multiple containers
V1Pod pod = new V1Pod()
    .metadata(new V1ObjectMeta().name("complex-pod"))
    .spec(new V1PodSpec()
        .containers(Arrays.asList(
            new V1Container()
                .name("training")
                .image("my-training-image")
                .resources(new V1ResourceRequirements()
                    .putLimitsItem("nvidia.com/gpu", new Quantity("2"))
                    .putLimitsItem("nvidia.com/gpumem", new Quantity("8000"))
                    .putLimitsItem("nvidia.com/gpucores", new Quantity("100"))
                ),
            new V1Container()
                .name("sidecar")
                .image("my-sidecar-image")
        ))
    );

coreV1Api.createNamespacedPod("default", pod, null, null, null, null);
```

### Monitoring Multiple Pods

```java
import io.kubernetes.client.openapi.models.V1PodList;

// List all pods in a namespace
V1PodList pods = coreV1Api.listNamespacedPod(
    "default", null, null, null, null, 
    null, null, null, null, null, null
);

for (V1Pod pod : pods.getItems()) {
    HAMiClient.SchedulingInfo schedulingInfo = 
        hami.getPodSchedulingInfo(
            pod.getMetadata().getName(),
            pod.getMetadata().getNamespace()
        );
    System.out.println("Pod " + pod.getMetadata().getName() + ": " + schedulingInfo);
}
```

## Running the Example

Compile and run the example:

```bash
# Using javac (ensure kubernetes client jar is in classpath)
javac -cp kubernetes-client.jar HAMiClient.java
java -cp .:kubernetes-client.jar io.hami.client.HAMiClient

# Or using Maven
mvn exec:java -Dexec.mainClass="io.hami.client.HAMiClient"

# Or using Gradle
gradle run
```

This will:
1. List all GPU nodes and their devices
2. Create an example GPU pod
3. Query the scheduling information
4. Clean up the created resources

## Contributing

This is an example client implementation. For production use, consider:
- Adding comprehensive error handling
- Implementing retry logic
- Adding logging and monitoring
- Supporting additional HAMi features
- Creating a proper Maven/Gradle library

## References

- [HAMi Documentation](../../docs/)
- [HAMi API Reference](../../docs/develop/api-reference.md)
- [Kubernetes Java Client](https://github.com/kubernetes-client/java)
- [HAMi GitHub Repository](https://github.com/Project-HAMi/HAMi)
