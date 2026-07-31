# HAMi Integration with Kueue (Kubernetes-native Job Queueing)

This directory contains examples for integrating HAMi's virtualized GPU resources (`hami.io/vgpu-memory` and `hami.io/vgpu-core`) with [Kueue](https://kueue.sigs.k8s.io/).

Kueue natively supports tracking extended resources like those provided by HAMi. By defining a `ClusterQueue` with HAMi resources, Kueue will ensure that batch ML workloads (Jobs, PyTorchJobs, RayJobs) are only admitted to the cluster if there is sufficient fragmented vGPU capacity available.

## Usage Guide

1. **Install Kueue:** Follow the [official Kueue installation guide](https://kueue.sigs.k8s.io/docs/installation/).
2. **Apply the ResourceFlavor:** Defines the labels for your GPU nodes.
   ```bash
   kubectl apply -f 01-resource-flavor.yaml
   ```
3. **Configure the ClusterQueue:** Defines the total vGPU capacity available for Kueue to manage. *Adjust the `nominalQuota` to match your cluster's total physical capacity.*
   ```bash
   kubectl apply -f 02-cluster-queue.yaml
   ```
4. **Create a LocalQueue:** Grants a specific namespace access to the ClusterQueue.
   ```bash
   kubectl apply -f 03-local-queue.yaml
   ```
5. **Submit a Workload:** Submit a Job that requests `hami.io/vgpu-memory` and specifies the Kueue queue.
   ```bash
   kubectl apply -f 04-sample-job.yaml
   ```

Kueue will suspend the job if the `hami.io/vgpu-memory` requested exceeds the available quota in the `ClusterQueue`, and automatically resume it once vGPU memory frees up.
