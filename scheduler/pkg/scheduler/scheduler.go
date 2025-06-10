package scheduler

import (
	"context"
	"encoding/json"
	"math/rand"
	"sort"
	"time"

	"github.com/whywaita/shoes-lxd-multi/scheduler/pkg/storage"
)

const (
	// ScheduledResourceTTL is the TTL for scheduled resources in Redis
	ScheduledResourceTTL = 2 * time.Minute
	// ScheduledResourceFilterTTL is the duration used to filter out old scheduled resources
	ScheduledResourceFilterTTL = 1 * time.Minute
	// MaxSchedulingRetries is the maximum number of retries for resource scheduling
	MaxSchedulingRetries = 3
	// LockRetryDelay is the delay between lock acquisition retries
	LockRetryDelay = 10 * time.Millisecond
)

// ScheduledResource represents a resource allocation created by scheduling
type ScheduledResource struct {
	CPU    int       `json:"cpu"`    // CPU cores requested
	Memory int       `json:"memory"` // Memory in MB requested
	Time   time.Time `json:"time"`   // When it was scheduled
}

// ScheduledResourceStats contains aggregated metrics for scheduled resources
type ScheduledResourceStats struct {
	HostName      string    `json:"host_name"`
	TotalCPU      int       `json:"total_cpu"`
	TotalMemory   int       `json:"total_memory"`
	Count         int       `json:"count"`
	OldestRequest time.Time `json:"oldest_request"`
}

type Scheduler struct {
	ResourceManager *LXDResourceManager
}

func (s *Scheduler) Schedule(ctx context.Context, req ScheduleRequest) (string, bool) {
	resources := s.ResourceManager.GetResources(ctx)
	storageBackend := s.ResourceManager.storage

	// Get scheduled resources to account for the planned usage
	scheduledResources, err := s.GetScheduledResources(ctx)
	if err != nil {
		s.ResourceManager.Logger.Error("failed to get scheduled resources", "error", err)
		// Continue with empty scheduled resources if there's an error
		scheduledResources = make(map[string][]ScheduledResource)
	}

	// Create a copy of resources adjusted with scheduled resources
	adjustedResources := make(map[string]LXDResource)
	for lxdAPIAddress, res := range resources {
		// Create a deep copy of the resource
		adjustedRes := res

		// Account for scheduled resources
		schRes, exists := scheduledResources[lxdAPIAddress]
		if exists {
			for _, sr := range schRes {
				adjustedRes.Resource.CPUUsed += uint64(sr.CPU)
				adjustedRes.Resource.MemoryUsed += uint64(sr.Memory)
			}
		}

		adjustedResources[lxdAPIAddress] = adjustedRes
	}

	// Try to schedule and acquire lock with retry logic
	var selected string
	var locked bool
	for attempt := 0; attempt < MaxSchedulingRetries; attempt++ {
		// Try to get a lock for each resource to avoid race conditions
		// but do not filter out resources that are already locked
		// as they may still have capacity for additional workloads
		candidate, ok := Schedule(adjustedResources, req)
		if !ok {
			return "", false
		}

		// Lock the resource to prevent race conditions during allocation
		var err error
		locked, err = storageBackend.TryLock(ctx, candidate)
		if err != nil {
			s.ResourceManager.Logger.Error("failed to acquire lock", "lxdAPIAddress", candidate, "error", err, "attempt", attempt+1)
			if attempt < MaxSchedulingRetries-1 {
				time.Sleep(LockRetryDelay)
				continue
			}
			return "", false
		}

		if locked {
			selected = candidate
			break
		}

		s.ResourceManager.Logger.Info("resource locked during selection, retrying", "lxdAPIAddress", candidate, "attempt", attempt+1)
		if attempt < MaxSchedulingRetries-1 {
			time.Sleep(LockRetryDelay)
		}
	}

	if !locked {
		s.ResourceManager.Logger.Warn("failed to acquire lock after all retries", "maxRetries", MaxSchedulingRetries)
		return "", false
	}

	// Store the scheduled resource information
	schedKey := "scheduled:" + selected
	var schedList []ScheduledResource

	// Get existing scheduled resources
	schedResource, err := storageBackend.GetResource(ctx, schedKey)
	if err == nil && schedResource != nil {
		// Resource exists, unmarshal the scheduled resources
		err = json.Unmarshal([]byte(schedResource.Status), &schedList)
		if err != nil {
			s.ResourceManager.Logger.Error("failed to unmarshal scheduled resources", "error", err)
			schedList = []ScheduledResource{}
		}
	}

	// Add the new scheduled resource
	newSched := ScheduledResource{
		CPU:    req.CPU,
		Memory: req.Memory,
		Time:   time.Now(),
	}
	schedList = append(schedList, newSched)

	// Serialize and save
	schedData, err := json.Marshal(schedList)
	if err != nil {
		s.ResourceManager.Logger.Error("failed to marshal scheduled resources", "error", err)
		// Continue anyway, as we've already locked the resource
	} else {
		err = storageBackend.SetResource(ctx, &storage.Resource{
			ID:        schedKey,
			Status:    string(schedData),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, ScheduledResourceTTL) // TTL for scheduled resource information

		if err != nil {
			s.ResourceManager.Logger.Error("failed to save scheduled resources", "error", err)
		}
	}

	// Release the lock after saving the scheduled resources
	err = storageBackend.Unlock(ctx, selected)
	if err != nil {
		s.ResourceManager.Logger.Error("failed to release lock", "lxdAPIAddress", selected, "error", err)
	}

	return selected, true
}

// GetScheduledResources retrieves all scheduled resources from storage
func (s *Scheduler) GetScheduledResources(ctx context.Context) (map[string][]ScheduledResource, error) {
	storageBackend := s.ResourceManager.storage

	result := make(map[string][]ScheduledResource)
	resources, err := storageBackend.ListResources(ctx)
	if err != nil {
		return result, err
	}

	// Find all scheduled resources
	for key, resource := range resources {
		if len(resource) == 0 {
			continue
		}

		// Only process scheduled resources
		if len(key) <= 10 || key[:10] != "scheduled:" {
			continue
		}

		lxdAPIAddress := key[10:] // Remove "scheduled:" prefix
		var schedList []ScheduledResource

		err := json.Unmarshal([]byte(resource[0].Status), &schedList)
		if err != nil {
			s.ResourceManager.Logger.Error("failed to unmarshal scheduled resources", "key", key, "error", err)
			continue
		}

		// Filter out old scheduled resources
		validSched := []ScheduledResource{}
		for _, sr := range schedList {
			if time.Since(sr.Time) < ScheduledResourceFilterTTL {
				validSched = append(validSched, sr)
			}
		}

		if len(validSched) > 0 {
			result[lxdAPIAddress] = validSched
		}
	}

	return result, nil
}

// GetScheduledResourceStats returns statistics about scheduled resources by host
func (s *Scheduler) GetScheduledResourceStats(ctx context.Context) (map[string]ScheduledResourceStats, error) {
	scheduledResources, err := s.GetScheduledResources(ctx)
	if err != nil {
		return nil, err
	}

	stats := make(map[string]ScheduledResourceStats)

	for host, resources := range scheduledResources {
		if _, exists := stats[host]; !exists {
			stats[host] = ScheduledResourceStats{
				HostName:      host,
				TotalCPU:      0,
				TotalMemory:   0,
				Count:         0,
				OldestRequest: time.Time{},
			}
		}

		hostStats := stats[host]

		for _, res := range resources {
			hostStats.TotalCPU += res.CPU
			hostStats.TotalMemory += res.Memory
			hostStats.Count++

			if hostStats.OldestRequest.IsZero() || res.Time.Before(hostStats.OldestRequest) {
				hostStats.OldestRequest = res.Time
			}
		}

		stats[host] = hostStats
	}

	return stats, nil
}

func Schedule(resources map[string]LXDResource, req ScheduleRequest) (string, bool) {
	type resource struct {
		lxdAPIAddress string
		res           LXDResource
	}
	var input []resource
	for lxdAPIAddress, res := range resources {
		input = append(input, resource{
			lxdAPIAddress: lxdAPIAddress,
			res:           res,
		})
	}

	// 1. Filter host of fewest used resources
	var filterMinUsedCore []resource
	var minUsedCore uint64 = 1024 * 1024 * 1024 * 1024 // a very large number
	for _, r := range input {
		switch {
		case r.res.Resource.CPUUsed < minUsedCore:
			// found new minimum used core
			// reset filtered
			filterMinUsedCore = []resource{r}
			minUsedCore = r.res.Resource.CPUUsed
		case r.res.Resource.CPUUsed == minUsedCore:
			// found same minimum used core
			// append to filtered
			filterMinUsedCore = append(filterMinUsedCore, r)
		}
	}

	// 2. Check if any host has enough resources
	var filterEnoughResource []resource
	for _, r := range filterMinUsedCore {
		if r.res.Resource.CPUTotal-r.res.Resource.CPUUsed >= uint64(req.CPU) && r.res.Resource.MemoryTotal-r.res.Resource.MemoryUsed >= uint64(req.Memory) {
			// found host with enough resources
			filterEnoughResource = append(filterEnoughResource, r)
		}
	}

	// 3. Sort is finished. If no host has enough resources, return empty
	if len(filterEnoughResource) == 0 {
		return "", false
	}

	// 4. Sort by least created instances
	sort.SliceStable(filterEnoughResource, func(i, j int) bool {
		return len(filterEnoughResource[i].res.Resource.Instances) < len(filterEnoughResource[j].res.Resource.Instances)
	})
	var minCreatedInstances int = len(filterEnoughResource[0].res.Resource.Instances)

	// 5. Sort by least used resources
	sort.SliceStable(filterEnoughResource, func(i, j int) bool {
		return filterEnoughResource[i].res.Resource.CPUUsed < filterEnoughResource[j].res.Resource.CPUUsed
	})

	// 6. Filter by minimum created instances
	var filterMinCreatedInstances []resource
	for _, r := range filterEnoughResource {
		if len(r.res.Resource.Instances) == minCreatedInstances {
			// found host with minimum created instances
			filterMinCreatedInstances = append(filterMinCreatedInstances, r)
		}
	}

	// 7. Finish scheduling. Return the random host in the filtered list
	if len(filterMinCreatedInstances) == 0 {
		return "", false
	}
	// Randomly select a host from the filtered list
	randomIndex := rand.Intn(len(filterMinCreatedInstances))
	return filterMinCreatedInstances[randomIndex].lxdAPIAddress, true
}
