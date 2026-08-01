# Proposal: Kueue Integration for Batch AI Workload Queuing

## What would you like to be added

We propose adding a native integration or topology-aware extension between HAMi and Kueue (the Kubernetes-native job queueing system). Specifically, this would involve exposing HAMi's cluster-wide vGPU capacity in a way that Kueue's `ClusterQueue` and `ResourceFlavor` CRDs can interpret. This ensures Kueue only admits a Job if HAMi actually has the contiguous vGPU memory required available in the cluster.

## Why is this needed

As AI workloads scale, users are increasingly relying on Kueue to manage batch ML jobs like `PyTorchJob` or `RayJob`. Currently, Kueue lacks native visibility into HAMi's virtualized GPU resources (e.g., `nvidia.com/gpumem` and `nvidia.com/gpucores`). 

If multiple teams submit large ML jobs requesting vGPUs, Kueue cannot accurately queue these jobs based on HAMi's actual fragmented capacity. This leads to scheduling deadlocks or pods getting stuck in the `Pending` state indefinitely, defeating the entire purpose of a batch queue.

## Anything else we need to know?

This integration bridges two major CNCF projects (HAMi and Kueue) and solves a highly requested enterprise use case for batch AI scheduling. This would make an excellent LFX Mentorship Project. By formalizing this architectural direction, we can ensure standard batch ML workloads operate smoothly on top of HAMi virtualized clusters.
