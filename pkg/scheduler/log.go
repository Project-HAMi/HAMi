/*
Copyright 2025 The HAMi Authors.

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

package scheduler

import (
	"container/list"
	"fmt"
	"sync"
)

type Status string

const (
	Failed Status = "failed"
	Passed Status = "passed"
)

/*
pod scheduler log like this
{
    "status": "failed",
    "filter": {
        "status": "passed",
        "summary": "",
        "nodes": [
            {
                "name": "node1",
                "status": "failed",
                "score": 0,
                "containers": [
                    {
                        "name": "container1",
                        "status": "failed",
                        "reason": "2/8 NumaNotFit, 3/8 CardInsufficientMemory, 2/8 CardInsufficientCore, 1/8 ExclusiveDeviceAllocateConflict"
                    }
                ]
            },
            {
                "name": "node2",
                "status": "failed",
                "score": 0,
                "containers": [
                    {
                        "name": "container1",
                        "status": "passed"
                    },
                    {
                        "name": "container2",
                        "status": "failed",
                        "reason": "4/8 CardInsufficientMemory, 3/8 CardUuidMismatch, 1/8 CardInsufficientCore"
                    }
                ]
            },
            {
                "name": "node3",
                "status": "passed",
                "score": 2.35,
                "containers": [
                    {
                        "name": "container1",
                        "status": "passed"
                    },
                    {
                        "name": "container1",
                        "status": "passed"
                    }
                ]
            },
            {
                "name": "node4",
                "status": "passed",
                "score": 2.95,
                "containers": [
                    {
                        "name": "container1",
                        "status": "passed"
                    },
                    {
                        "name": "container1",
                        "status": "passed"
                    }
                ]
            },
            {
                "name": "node5",
                "status": "failed",
                "score": 0,
                "containers": [
                    {
                        "name": "container1",
                        "status": "failed",
                        "reason": "4/8 CardUuidMismatch, 3/8 CardInsufficientMemory, 1/8 ExclusiveDeviceAllocateConflict"
                    }
                ]
            }
        ]
    },
    "bind": {
        "status": "failed",
        "summary": "Failed to lock node, node aio-node15 has been locked within 5min"
    }
}
*/

// PodSchedulerLogResponse represents the entire response structure for the pod scheduler logs API.
type PodSchedulerLogResponse struct {
	Status Status       `json:"status"` // Overall scheduling status: "succeeded" or "failed"
	Filter FilterResult `json:"filter"` // Result of the filtering phase
	Bind   BindResult   `json:"bind"`   // Result of the binding phase
}

// FilterResult represents the result of the filter (predicates) phase of scheduling.
type FilterResult struct {
	Status  Status       `json:"status"`  // Status of the filter phase: "success" or "failed"
	Summary string       `json:"summary"` // A brief summary of the filter phase result
	Nodes   []NodeResult `json:"nodes"`   // Results for each node in the pod
}

// NodeResult represents the scheduling result for a single node within the pod.
type NodeResult struct {
	Name       string            `json:"name"`       // Name of the node
	Status     Status            `json:"status"`     // Overall status for this node: "passed" or "failed"
	Score      float32           `json:"score"`      // Overall score for the filter phase
	Containers []ContainerResult `json:"containers"` // Results for each container evaluated for this node
}

// ContainerResult represents the evaluation result of a single node for a container.
type ContainerResult struct {
	Name   string `json:"name"`             // Name of the node
	Status Status `json:"status"`           // Evaluation status on this node: "passed" or "failed"
	Reason string `json:"reason,omitempty"` // Reason for failure, present only if status is "failed"
}

// BindResult represents the result of the bind (binding) phase of scheduling.
type BindResult struct {
	Status  Status `json:"status"`  // Status of the bind phase: "success" or "failed"
	Summary string `json:"summary"` // Summary of the bind phase result
}

// schedulerLogEntry represents an entry in the cache.
type schedulerLogEntry struct {
	key   string
	value PodSchedulerLogResponse
}

// SchedulerLogCache is a thread-safe LRU cache for pod scheduler logs.
type SchedulerLogCache struct {
	items    map[string]*list.Element // map for O(1) lookups
	oldest   *list.List               // list to track insertion order
	capacity int                      // maximum number of items to cache
	mu       sync.Mutex               // mutex for concurrent access
}

// NewSchedulerLogCache creates a new SchedulerLogCache with the specified capacity.
func NewSchedulerLogCache(capacity int) *SchedulerLogCache {
	return &SchedulerLogCache{
		items:    make(map[string]*list.Element),
		oldest:   list.New(),
		capacity: capacity,
	}
}

// getKey generates a unique key for a pod.
func getKey(namespace, podName string) string {
	return fmt.Sprintf("%s/%s", namespace, podName)
}

// Get retrieves the scheduler log for a pod.
func (c *SchedulerLogCache) Get(namespace, podName string) (PodSchedulerLogResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := getKey(namespace, podName)
	if elem, ok := c.items[key]; ok {
		c.oldest.MoveToFront(elem)
		entry, ok := elem.Value.(schedulerLogEntry)
		if !ok {
			return PodSchedulerLogResponse{}, false
		}
		return entry.value, true
	}
	return PodSchedulerLogResponse{}, false
}

// SetStatus sets the overall status of a pod's scheduling.
func (c *SchedulerLogCache) SetStatus(namespace, podName string, status Status) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := getKey(namespace, podName)

	// If the pod doesn't exist, create a new entry
	if _, exists := c.items[key]; !exists {
		c.items[key] = c.oldest.PushFront(schedulerLogEntry{
			key: key,
			value: PodSchedulerLogResponse{
				Status: status,
				Filter: FilterResult{
					Nodes: []NodeResult{},
				},
				Bind: BindResult{},
			},
		})
		c.evictIfNecessary()
		return
	}

	// Update the existing entry
	entry, ok := c.items[key].Value.(schedulerLogEntry)
	if !ok {
		return
	}
	entry.value.Status = status
	c.items[key].Value = entry
}

// SetFilterStatusAndSummary sets the filter phase status and summary.
func (c *SchedulerLogCache) SetFilterStatusAndSummary(namespace, podName string, status Status, summary string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := getKey(namespace, podName)

	// If the pod doesn't exist, create a new entry
	if _, exists := c.items[key]; !exists {
		c.items[key] = c.oldest.PushFront(schedulerLogEntry{
			key: key,
			value: PodSchedulerLogResponse{
				Filter: FilterResult{
					Status:  status,
					Summary: summary,
					Nodes:   []NodeResult{},
				},
				Bind: BindResult{},
			},
		})
		c.evictIfNecessary()
		return
	}

	// Update the existing entry
	entry, ok := c.items[key].Value.(schedulerLogEntry)
	if !ok {
		return
	}
	entry.value.Filter.Status = status
	entry.value.Status = status // same as Filter.Status
	entry.value.Filter.Summary = summary
	c.items[key].Value = entry
}

// SetBindStatusAndSummary sets the bind phase status and summary.
func (c *SchedulerLogCache) SetBindStatusAndSummary(namespace, podName string, status Status, summary string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := getKey(namespace, podName)

	// If the pod doesn't exist, create a new entry
	if _, exists := c.items[key]; !exists {
		c.items[key] = c.oldest.PushFront(schedulerLogEntry{
			key: key,
			value: PodSchedulerLogResponse{
				Bind: BindResult{
					Status:  status,
					Summary: summary,
				},
				Filter: FilterResult{
					Nodes: []NodeResult{},
				},
			},
		})
		c.evictIfNecessary()
		return
	}

	// Update the existing entry
	entry, ok := c.items[key].Value.(schedulerLogEntry)
	if !ok {
		return
	}
	entry.value.Bind.Status = status
	entry.value.Status = status // same as Bind.Status
	entry.value.Bind.Summary = summary
	c.items[key].Value = entry
}

// AddNodeResult adds a node result for a specific container in a pod.
func (c *SchedulerLogCache) AddNodeResult(namespace, podName, nodeName string, status Status, score float32, containers []ContainerResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := getKey(namespace, podName)

	// If the pod doesn't exist, create a new entry
	if _, exists := c.items[key]; !exists {
		// Create a new node with the node result
		node := NodeResult{
			Name:       nodeName,
			Status:     status,
			Score:      score,
			Containers: containers,
		}

		// Create a new pod scheduler log response
		c.items[key] = c.oldest.PushFront(schedulerLogEntry{
			key: key,
			value: PodSchedulerLogResponse{
				Filter: FilterResult{
					Nodes: []NodeResult{node},
				},
				Bind: BindResult{},
			},
		})
		c.evictIfNecessary()
		return
	}

	// Update the existing entry
	entry, ok := c.items[key].Value.(schedulerLogEntry)
	if !ok {
		return
	}
	logResponse := entry.value

	// Find or create the node
	nodeIndex := -1
	for i, node := range logResponse.Filter.Nodes {
		if node.Name == nodeName {
			nodeIndex = i
			break
		}
	}

	var node NodeResult
	if nodeIndex == -1 {
		// Node doesn't exist, create it
		node = NodeResult{
			Name:       nodeName,
			Status:     status,
			Score:      score,
			Containers: containers,
		}
		logResponse.Filter.Nodes = append(logResponse.Filter.Nodes, node)
		nodeIndex = len(logResponse.Filter.Nodes) - 1
	} else {
		node = logResponse.Filter.Nodes[nodeIndex]
	}

	// Add the node result
	node.Containers = containers
	// Update the container in the log response
	logResponse.Filter.Nodes[nodeIndex] = node

	// Update the entry
	entry.value = logResponse
	c.items[key].Value = entry
}

// Remove deletes a pod's scheduler log from the cache.
func (c *SchedulerLogCache) Remove(namespace, podName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := getKey(namespace, podName)
	if elem, ok := c.items[key]; ok {
		delete(c.items, key)
		c.oldest.Remove(elem)
	}
}

// evictIfNecessary removes the oldest item if the cache is full.
func (c *SchedulerLogCache) evictIfNecessary() {
	if c.oldest.Len() > c.capacity {
		// Remove the oldest item (from the back of the list)
		elem := c.oldest.Back()
		if elem != nil {
			entry, ok := elem.Value.(schedulerLogEntry)
			if !ok {
				return
			}
			delete(c.items, entry.key)
			c.oldest.Remove(elem)
		}
	}
}
