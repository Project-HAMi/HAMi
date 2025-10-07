#!/usr/bin/env python3
"""
HAMi Python Client

This is an example client library for interacting with HAMi (Heterogeneous AI Computing Virtualization Middleware).
HAMi integrates with Kubernetes to manage heterogeneous devices like GPUs, NPUs, etc.

The recommended way to use HAMi is through Kubernetes client libraries to manage pods and nodes.
This example shows how to:
1. Query HAMi device information from node annotations
2. Create pods with HAMi device requests
3. Monitor HAMi scheduling decisions

Requirements:
    pip install kubernetes

Usage:
    python hami_client.py
"""

from kubernetes import client, config
from typing import Dict, List, Optional
import json
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class HAMiClient:
    """Client for interacting with HAMi through Kubernetes API."""
    
    def __init__(self, kubeconfig: Optional[str] = None):
        """
        Initialize HAMi client.
        
        Args:
            kubeconfig: Path to kubeconfig file. If None, uses default config.
        """
        try:
            if kubeconfig:
                config.load_kube_config(config_file=kubeconfig)
            else:
                # Try in-cluster config first, then fall back to default kubeconfig
                try:
                    config.load_incluster_config()
                except config.ConfigException:
                    config.load_kube_config()
            
            self.core_v1 = client.CoreV1Api()
            logger.info("HAMi client initialized successfully")
        except Exception as e:
            logger.error(f"Failed to initialize Kubernetes client: {e}")
            raise
    
    def list_gpu_nodes(self, label_selector: str = "gpu=on") -> List[Dict]:
        """
        List all nodes with HAMi GPU support.
        
        Args:
            label_selector: Label selector to filter nodes (default: "gpu=on")
            
        Returns:
            List of node information dictionaries
        """
        try:
            nodes = self.core_v1.list_node(label_selector=label_selector)
            result = []
            
            for node in nodes.items:
                node_info = {
                    "name": node.metadata.name,
                    "labels": node.metadata.labels,
                    "annotations": node.metadata.annotations or {},
                    "devices": self._parse_device_annotations(node.metadata.annotations or {})
                }
                result.append(node_info)
            
            logger.info(f"Found {len(result)} GPU nodes")
            return result
        except Exception as e:
            logger.error(f"Failed to list GPU nodes: {e}")
            raise
    
    def _parse_device_annotations(self, annotations: Dict[str, str]) -> Dict:
        """
        Parse HAMi device annotations from node.
        
        Args:
            annotations: Node annotations dictionary
            
        Returns:
            Dictionary of device information by device type
        """
        devices = {}
        
        for key, value in annotations.items():
            # Parse device registration annotations
            if key.startswith("hami.io/node-") and key.endswith("-register"):
                device_type = key.replace("hami.io/node-", "").replace("-register", "")
                devices[device_type] = self._parse_device_string(value)
            
            # Include handshake information
            if key.startswith("hami.io/node-handshake-"):
                device_type = key.replace("hami.io/node-handshake-", "")
                if device_type not in devices:
                    devices[device_type] = {}
                devices[device_type]["handshake"] = value
        
        return devices
    
    def _parse_device_string(self, device_str: str) -> List[Dict]:
        """
        Parse device registration string.
        
        Format: {Device UUID},{split count},{memory},{cores},{type},{numa},{healthy}:...
        
        Args:
            device_str: Device registration string
            
        Returns:
            List of device dictionaries
        """
        devices = []
        
        if not device_str or device_str.endswith(":"):
            device_str = device_str.rstrip(":")
        
        for device_info in device_str.split(":"):
            if not device_info:
                continue
            
            parts = device_info.split(",")
            if len(parts) >= 7:
                devices.append({
                    "uuid": parts[0],
                    "split_count": int(parts[1]),
                    "memory_mb": int(parts[2]),
                    "cores": int(parts[3]),
                    "type": parts[4],
                    "numa": int(parts[5]),
                    "healthy": parts[6].lower() == "true"
                })
        
        return devices
    
    def get_node_devices(self, node_name: str) -> Dict:
        """
        Get device information for a specific node.
        
        Args:
            node_name: Name of the node
            
        Returns:
            Dictionary of device information
        """
        try:
            node = self.core_v1.read_node(name=node_name)
            annotations = node.metadata.annotations or {}
            return self._parse_device_annotations(annotations)
        except Exception as e:
            logger.error(f"Failed to get devices for node {node_name}: {e}")
            raise
    
    def create_gpu_pod(self, 
                       pod_name: str,
                       namespace: str,
                       image: str,
                       gpu_count: int = 1,
                       gpu_memory_mb: Optional[int] = None,
                       gpu_cores: Optional[int] = None,
                       command: Optional[List[str]] = None) -> Dict:
        """
        Create a pod with GPU resources.
        
        Args:
            pod_name: Name of the pod
            namespace: Kubernetes namespace
            image: Container image
            gpu_count: Number of GPUs to request
            gpu_memory_mb: GPU memory in MB (optional)
            gpu_cores: GPU core percentage (optional)
            command: Container command (optional)
            
        Returns:
            Created pod object as dictionary
        """
        # Build resource limits
        limits = {
            "nvidia.com/gpu": str(gpu_count)
        }
        
        if gpu_memory_mb is not None:
            limits["nvidia.com/gpumem"] = str(gpu_memory_mb)
        
        if gpu_cores is not None:
            limits["nvidia.com/gpucores"] = str(gpu_cores)
        
        # Create pod specification
        container = client.V1Container(
            name=pod_name,
            image=image,
            command=command,
            resources=client.V1ResourceRequirements(
                limits=limits
            )
        )
        
        pod_spec = client.V1PodSpec(
            containers=[container],
            restart_policy="Never"
        )
        
        pod = client.V1Pod(
            api_version="v1",
            kind="Pod",
            metadata=client.V1ObjectMeta(name=pod_name),
            spec=pod_spec
        )
        
        try:
            created_pod = self.core_v1.create_namespaced_pod(
                namespace=namespace,
                body=pod
            )
            logger.info(f"Created pod {pod_name} in namespace {namespace}")
            return self._pod_to_dict(created_pod)
        except Exception as e:
            logger.error(f"Failed to create pod: {e}")
            raise
    
    def get_pod_scheduling_info(self, pod_name: str, namespace: str) -> Dict:
        """
        Get HAMi scheduling information for a pod.
        
        Args:
            pod_name: Name of the pod
            namespace: Kubernetes namespace
            
        Returns:
            Dictionary with scheduling information
        """
        try:
            pod = self.core_v1.read_namespaced_pod(name=pod_name, namespace=namespace)
            annotations = pod.metadata.annotations or {}
            
            scheduling_info = {
                "devices_to_allocate": annotations.get("hami.io/devices-to-allocate", ""),
                "node": annotations.get("hami.io/vgpu-node", ""),
                "schedule_time": annotations.get("hami.io/vgpu-time", ""),
                "pod_phase": pod.status.phase,
                "node_name": pod.spec.node_name
            }
            
            # Parse device allocation
            if scheduling_info["devices_to_allocate"]:
                scheduling_info["allocated_devices"] = self._parse_device_allocation(
                    scheduling_info["devices_to_allocate"]
                )
            
            return scheduling_info
        except Exception as e:
            logger.error(f"Failed to get pod scheduling info: {e}")
            raise
    
    def _parse_device_allocation(self, allocation_str: str) -> List[Dict]:
        """
        Parse device allocation string.
        
        Format: {device UUID},{type},{memory},{cores}:...
        
        Args:
            allocation_str: Device allocation string
            
        Returns:
            List of allocated device dictionaries
        """
        devices = []
        
        if not allocation_str or allocation_str.endswith(":"):
            allocation_str = allocation_str.rstrip(":")
        
        for device_info in allocation_str.split(":"):
            if not device_info:
                continue
            
            parts = device_info.split(",")
            if len(parts) >= 4:
                devices.append({
                    "uuid": parts[0],
                    "type": parts[1],
                    "memory_mb": int(parts[2]),
                    "cores": int(parts[3])
                })
        
        return devices
    
    def _pod_to_dict(self, pod) -> Dict:
        """Convert pod object to dictionary."""
        return {
            "name": pod.metadata.name,
            "namespace": pod.metadata.namespace,
            "uid": pod.metadata.uid,
            "phase": pod.status.phase,
            "node_name": pod.spec.node_name
        }
    
    def delete_pod(self, pod_name: str, namespace: str) -> None:
        """
        Delete a pod.
        
        Args:
            pod_name: Name of the pod
            namespace: Kubernetes namespace
        """
        try:
            self.core_v1.delete_namespaced_pod(
                name=pod_name,
                namespace=namespace
            )
            logger.info(f"Deleted pod {pod_name} from namespace {namespace}")
        except Exception as e:
            logger.error(f"Failed to delete pod: {e}")
            raise


def main():
    """Example usage of HAMi client."""
    # Initialize client
    hami = HAMiClient()
    
    # List GPU nodes
    print("\n=== GPU Nodes ===")
    nodes = hami.list_gpu_nodes()
    for node in nodes:
        print(f"\nNode: {node['name']}")
        print(f"Devices: {json.dumps(node['devices'], indent=2)}")
    
    # Example: Create a GPU pod
    print("\n=== Creating GPU Pod ===")
    try:
        pod_info = hami.create_gpu_pod(
            pod_name="hami-example-pod",
            namespace="default",
            image="nvidia/cuda:11.0-base",
            gpu_count=1,
            gpu_memory_mb=3000,
            gpu_cores=50,
            command=["nvidia-smi"]
        )
        print(f"Created pod: {json.dumps(pod_info, indent=2)}")
        
        # Get scheduling info
        import time
        time.sleep(2)  # Wait for scheduling
        
        print("\n=== Pod Scheduling Info ===")
        scheduling_info = hami.get_pod_scheduling_info("hami-example-pod", "default")
        print(json.dumps(scheduling_info, indent=2))
        
        # Clean up
        print("\n=== Cleaning Up ===")
        hami.delete_pod("hami-example-pod", "default")
        
    except Exception as e:
        logger.error(f"Example failed: {e}")


if __name__ == "__main__":
    main()
