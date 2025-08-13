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
	"fmt"
	"testing"
)

func TestSchedulerLogCache_Get(t *testing.T) {
	cache := NewSchedulerLogCache(2)

	_, exists := cache.Get("default", "non-existent-pod")
	if exists {
		t.Errorf("Expected non-existent pod to not be found")
	}

	cache.SetStatus("default", "test-pod", Failed)
	log, exists := cache.Get("default", "test-pod")
	if !exists {
		t.Errorf("Expected pod to be found after setting status")
	}
	if log.Status != Failed {
		t.Errorf("Expected status to be 'failed', got %s", log.Status)
	}

	cache.SetStatus("default", "test-pod", Passed)
	log, exists = cache.Get("default", "test-pod")
	if !exists {
		t.Errorf("Expected pod to be found after setting status")
	}
	if log.Status != Passed {
		t.Errorf("Expected status to be 'failed', got %s", log.Status)
	}
}

func TestSchedulerLogCache_SetStatus(t *testing.T) {
	cache := NewSchedulerLogCache(1)

	cache.SetStatus("default", "test-pod", Failed)

	log, exists := cache.Get("default", "test-pod")
	if !exists {
		t.Errorf("Pod should exist after SetStatus")
	}
	if log.Status != Failed {
		t.Errorf("Expected status 'failed', got %s", log.Status)
	}
}

func TestSchedulerLogCache_SetFilterStatusAndSummary(t *testing.T) {
	cache := NewSchedulerLogCache(1)

	cache.SetFilterStatusAndSummary("default", "test-pod", Failed, "No suitable node")

	log, exists := cache.Get("default", "test-pod")
	if !exists {
		t.Errorf("Pod should exist after SetFilterStatusAndSummary")
	}
	if log.Filter.Status != Failed {
		t.Errorf("Expected filter status 'failed', got %s", log.Filter.Status)
	}
	if log.Filter.Summary != "No suitable node" {
		t.Errorf("Expected filter summary 'No suitable node', got %s", log.Filter.Summary)
	}
}

func TestSchedulerLogCache_SetBindStatusAndSummary(t *testing.T) {
	cache := NewSchedulerLogCache(1)

	cache.SetBindStatusAndSummary("default", "test-pod", Failed, "Failed to lock node")

	log, exists := cache.Get("default", "test-pod")
	if !exists {
		t.Errorf("Pod should exist after SetBindStatusAndSummary")
	}
	if log.Bind.Status != Failed {
		t.Errorf("Expected bind status 'failed', got %s", log.Bind.Status)
	}
	if log.Bind.Summary != "Failed to lock node" {
		t.Errorf("Expected bind summary 'Failed to lock node', got %s", log.Bind.Summary)
	}

	cache.SetBindStatusAndSummary("default", "test-pod", Passed, "")
	log, exists = cache.Get("default", "test-pod")
	if !exists {
		t.Errorf("Pod should exist after SetBindStatusAndSummary")
	}
	if log.Bind.Status != Passed {
		t.Errorf("Expected bind status 'passed', got %s", log.Bind.Status)
	}
	if log.Bind.Summary != "" {
		t.Errorf("Expected bind summary '', got %s", log.Bind.Summary)
	}
}

func TestSchedulerLogCache_AddNodeResult(t *testing.T) {
	cache := NewSchedulerLogCache(1)

	cache.AddNodeResult("default", "test-pod", "node1", Failed, 0,
		[]ContainerResult{
			{Name: "container1", Status: Passed},
			{Name: "container2", Status: Failed, Reason: "Insufficient resources"},
		})
	cache.AddNodeResult("default", "test-pod", "node2", Passed, 2.5,
		[]ContainerResult{
			{Name: "container1", Status: Passed},
			{Name: "container2", Status: Passed},
		})

	log, _ := cache.Get("default", "test-pod")
	if len(log.Filter.Nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(log.Filter.Nodes))
	}
	if len(log.Filter.Nodes[0].Containers) != 2 {
		t.Errorf("Expected 2 containers, got %d", len(log.Filter.Nodes[0].Containers))
	}
	if log.Filter.Nodes[0].Containers[0].Name != "container1" || log.Filter.Nodes[0].Containers[0].Status != Passed {
		t.Errorf("Container1 should be passed")
	}
	if log.Filter.Nodes[0].Containers[1].Name != "container2" || log.Filter.Nodes[0].Containers[1].Status != Failed || log.Filter.Nodes[0].Containers[1].Reason != "Insufficient resources" {
		t.Errorf("Container2 should be failed with reason 'Insufficient resources'")
	}

	cache.AddNodeResult("default", "test-pod", "node3", Failed, 0,
		[]ContainerResult{
			{Name: "container1", Status: Passed},
			{Name: "container2", Status: Failed, Reason: "Insufficient resources"},
		})
	cache.AddNodeResult("default", "test-pod", "node2", Passed, 2.96,
		[]ContainerResult{
			{Name: "container1", Status: Passed},
			{Name: "container2", Status: Passed},
		})

	log, _ = cache.Get("default", "test-pod")
	if len(log.Filter.Nodes) != 3 {
		t.Errorf("Expected 3 nodes, got %d", len(log.Filter.Nodes))
	}
	if len(log.Filter.Nodes[1].Containers) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(log.Filter.Nodes[1].Containers))
	}
	if log.Filter.Nodes[1].Containers[0].Name != "container1" || log.Filter.Nodes[1].Containers[0].Status != Passed {
		t.Errorf("Container1 should be passed")
	}
}

func TestSchedulerLogCache_Remove(t *testing.T) {
	cache := NewSchedulerLogCache(1)

	cache.SetStatus("default", "test-pod", Failed)
	cache.Remove("default", "test-pod")

	_, exists := cache.Get("default", "test-pod")
	if exists {
		t.Errorf("Pod should have been removed")
	}
}

func TestSchedulerLogCache_LRU(t *testing.T) {
	cache := NewSchedulerLogCache(2)

	cache.SetStatus("default", "pod1", Failed)
	cache.SetStatus("default", "pod2", Failed)
	cache.SetStatus("default", "pod3", Failed)

	_, exists1 := cache.Get("default", "pod1")
	_, exists2 := cache.Get("default", "pod2")
	_, exists3 := cache.Get("default", "pod3")

	if exists1 {
		t.Errorf("pod1 should have been evicted by LRU")
	}
	if !exists2 {
		t.Errorf("pod2 should still exist")
	}
	if !exists3 {
		t.Errorf("pod3 should exist")
	}
}

func TestSchedulerLogCache_ConcurrentAccess(t *testing.T) {
	cache := NewSchedulerLogCache(100)
	numGoroutines := 10
	numOperations := 100

	done := make(chan bool)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numOperations; j++ {
				namespace := fmt.Sprintf("ns-%d", id)
				podName := fmt.Sprintf("pod-%d-%d", id, j)
				containerName := fmt.Sprintf("container-%d", j%3)

				cache.SetStatus(namespace, podName, Failed)
				cache.SetFilterStatusAndSummary(namespace, podName, Failed, "No suitable node")
				cache.AddNodeResult(namespace, podName, fmt.Sprintf("node-%d", j%5), Failed, 0.25, []ContainerResult{
					{Name: containerName, Status: Failed, Reason: "Insufficient resources"},
				})

				if j%10 == 0 {
					cache.Get(namespace, podName)
				}
			}
			done <- true
		}(i)
	}

	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	if cache.oldest.Len() > 100 {
		t.Errorf("Cache size exceeded capacity: %d", cache.oldest.Len())
	}
}
