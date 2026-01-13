# HAMi API Reference

This document describes the REST API endpoints exposed by the HAMi scheduler. These APIs can be used to interact with HAMi programmatically using any HTTP client library in languages like Python, Java, Go, etc.

## Base URL

The HAMi scheduler exposes its API on the configured HTTP bind address (default: `127.0.0.1:8080`). In production deployments, this is typically exposed as a Kubernetes service.

## Authentication

Currently, the HAMi scheduler API endpoints are designed to be called by the Kubernetes scheduler and webhook admission controller. For external access, ensure proper network policies and authentication mechanisms are in place.

## API Endpoints

### 1. Health Check

**Endpoint:** `GET /healthz`

**Description:** Check the health status of the HAMi scheduler.

**Request:** No request body required.

**Response:**
- **Status Code:** 200 OK
- **Body:** Empty

**Example:**
```bash
curl http://127.0.0.1:8080/healthz
```

### 2. Filter (Predicate)

**Endpoint:** `POST /filter`

**Description:** Filter nodes based on device availability and scheduling constraints. This endpoint is called by the Kubernetes scheduler extender.

**Request Body:** JSON object conforming to `ExtenderArgs` format from Kubernetes scheduler extender API.

```json
{
  "Pod": {
    "metadata": {
      "name": "example-pod",
      "namespace": "default"
    },
    "spec": {
      "containers": [...],
      "schedulerName": "hami-scheduler"
    }
  },
  "Nodes": {
    "items": [...]
  },
  "NodeNames": ["node1", "node2"]
}
```

**Response:** JSON object conforming to `ExtenderFilterResult` format.

```json
{
  "Nodes": {
    "items": [...]
  },
  "NodeNames": ["node1"],
  "FailedNodes": {
    "node2": "insufficient GPU memory"
  },
  "Error": ""
}
```

**Example:**
```bash
curl -X POST http://127.0.0.1:8080/filter \
  -H "Content-Type: application/json" \
  -d @filter-request.json
```

### 3. Bind

**Endpoint:** `POST /bind`

**Description:** Bind a pod to a node with device allocation. This endpoint is called by the Kubernetes scheduler extender.

**Request Body:** JSON object conforming to `ExtenderBindingArgs` format.

```json
{
  "PodName": "example-pod",
  "PodNamespace": "default",
  "PodUID": "12345678-1234-1234-1234-123456789012",
  "Node": "node1"
}
```

**Response:** JSON object conforming to `ExtenderBindingResult` format.

```json
{
  "Error": ""
}
```

**Example:**
```bash
curl -X POST http://127.0.0.1:8080/bind \
  -H "Content-Type: application/json" \
  -d @bind-request.json
```

### 4. Webhook

**Endpoint:** `POST /webhook`

**Description:** Mutating webhook endpoint for pod admission control. This endpoint is called by the Kubernetes API server.

**Request Body:** Kubernetes AdmissionReview object.

**Response:** Kubernetes AdmissionReview object with admission response.

## Using HAMi API with Kubernetes Clients

Since HAMi integrates with Kubernetes, the recommended approach is to use Kubernetes client libraries to interact with pods and nodes, which will automatically trigger HAMi's scheduling logic through the scheduler extender and webhook mechanisms.

### Kubernetes Resources

HAMi uses Kubernetes node annotations to store device information and pod annotations to store scheduling decisions. You can query these using standard Kubernetes API calls.

#### Node Annotations

Nodes with HAMi devices have the following annotations:

- `hami.io/node-handshake-{device-type}`: Handshake timestamp
- `hami.io/node-{device-type}-register`: Device registration information

Example:
```
hami.io/node-handshake-nvidia: Reported 2024-01-23 04:30:04
hami.io/node-nvidia-register: GPU-00552014-5c87-89ac-b1a6-7b53aa24b0ec,10,32768,100,NVIDIA-Tesla V100-PCIE-32GB,0,true:...
```

#### Pod Annotations

Pods scheduled by HAMi have the following annotations:

- `hami.io/devices-to-allocate`: Device allocation information
- `hami.io/vgpu-node`: Scheduled node name
- `hami.io/vgpu-time`: Scheduling timestamp

Example:
```
hami.io/devices-to-allocate: GPU-0fc3eda5-e98b-a25b-5b0d-cf5c855d1448,NVIDIA,3000,0:
hami.io/vgpu-node: node67-4v100
hami.io/vgpu-time: 1705054796
```

## Device Resource Limits

When creating pods, specify device requirements using resource limits:

```yaml
resources:
  limits:
    nvidia.com/gpu: 1           # Number of physical GPUs
    nvidia.com/gpumem: 3000     # GPU memory in MB
    nvidia.com/gpucores: 50     # GPU core percentage
```

## Error Handling

All API endpoints return appropriate HTTP status codes:

- `200 OK`: Request successful
- `400 Bad Request`: Invalid request body
- `500 Internal Server Error`: Server error

Error responses include an `Error` field in the response body with details about the failure.

## Client Examples

For example implementations of HAMi API clients, see:

- [Python Client Example](../../examples/client/python/README.md)
- [Java Client Example](../../examples/client/java/README.md)

## Further Reading

- [Protocol Documentation](protocol.md) - Detailed protocol specifications
- [Scheduler Policy](scheduler-policy.md) - Scheduling algorithms and policies
- [Configuration Guide](../config.md) - HAMi configuration options
