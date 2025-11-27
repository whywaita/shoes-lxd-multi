package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
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

// Scheduler implements the resource scheduling logic for LXD instances
type Scheduler struct {
	ResourceManager *LXDResourceManager
}

func (s *Scheduler) Schedule(ctx context.Context, req ScheduleRequest) (string, bool) {
	resources := s.ResourceManager.GetResources(ctx)
	storageBackend := s.ResourceManager.storage

	// Filter resources by target hosts if specified
	if len(req.TargetHosts) > 0 {
		filteredResources := make(map[string]LXDResource)
		for _, targetHost := range req.TargetHosts {
			if res, exists := resources[targetHost]; exists {
				filteredResources[targetHost] = res
			}
		}
		resources = filteredResources

		// If no target hosts are available, return early
		if len(resources) == 0 {
			s.ResourceManager.Logger.Warn("no target hosts available",
				"target_hosts", req.TargetHosts,
			)
			return "", false
		}
	}

	// Get scheduled resources to account for the planned usage
	scheduledResources, err := s.GetScheduledResources(ctx)
	if err != nil {
		s.ResourceManager.Logger.Error("failed to get scheduled resources", "error", err)
		// Continue with empty scheduled resources if there's an error
		scheduledResources = make(map[string][]ScheduledResource)
	}

	// Create a copy of resources adjusted with scheduled resources
	adjustedResources := s.adjustResourcesWithScheduled(resources, scheduledResources)

	// Try to schedule and acquire lock with retry logic
	selected, err := s.scheduleWithLockRetry(ctx, adjustedResources, req, storageBackend)
	if err != nil || selected == "" {
		return "", false
	}

	// Store the scheduled resource information
	err = s.storeScheduledResource(ctx, storageBackend, selected, req)
	if err != nil {
		s.ResourceManager.Logger.Error("failed to store scheduled resource", "error", err)
		// Continue anyway, as we've already locked the resource
	}

	// Release the lock after saving the scheduled resources
	unlockErr := storageBackend.Unlock(ctx, selected)
	if unlockErr != nil {
		s.ResourceManager.Logger.Error("failed to release lock", "lxdAPIAddress", selected, "error", unlockErr)
	}

	return selected, true
}

// adjustResourcesWithScheduled creates a copy of resources adjusted with scheduled resources
func (s *Scheduler) adjustResourcesWithScheduled(resources map[string]LXDResource, scheduledResources map[string][]ScheduledResource) map[string]LXDResource {
	adjustedResources := make(map[string]LXDResource)
	for lxdAPIAddress, res := range resources {
		// Create a deep copy of the resource
		adjustedRes := res

		// Account for scheduled resources
		if schRes, exists := scheduledResources[lxdAPIAddress]; exists {
			for _, sr := range schRes {
				adjustedRes.Resource.CPUUsed += uint64(sr.CPU)
				adjustedRes.Resource.MemoryUsed += uint64(sr.Memory)
			}
		}

		adjustedResources[lxdAPIAddress] = adjustedRes
	}
	return adjustedResources
}

// scheduleWithLockRetry tries to schedule and acquire lock with retry logic
func (s *Scheduler) scheduleWithLockRetry(ctx context.Context, adjustedResources map[string]LXDResource, req ScheduleRequest, storageBackend storage.Storage) (string, error) {
	for attempt := 0; attempt < MaxSchedulingRetries; attempt++ {
		// Try to get a lock for each resource to avoid race conditions
		// but do not filter out resources that are already locked
		// as they may still have capacity for additional workloads
		candidate, ok := Schedule(adjustedResources, req)
		if !ok {
			return "", nil // No suitable candidate found
		}

		// Lock the resource to prevent race conditions during allocation
		locked, err := storageBackend.TryLock(ctx, candidate)
		if err != nil {
			s.ResourceManager.Logger.Error("failed to acquire lock", "lxdAPIAddress", candidate, "error", err, "attempt", attempt+1)
			if attempt < MaxSchedulingRetries-1 {
				time.Sleep(LockRetryDelay)
				continue
			}
			return "", err
		}

		if locked {
			return candidate, nil
		}

		s.ResourceManager.Logger.Info("resource locked during selection, retrying", "lxdAPIAddress", candidate, "attempt", attempt+1)
		if attempt < MaxSchedulingRetries-1 {
			time.Sleep(LockRetryDelay)
		}
	}

	s.ResourceManager.Logger.Warn("failed to acquire lock after all retries", "maxRetries", MaxSchedulingRetries)
	return "", nil
}

// storeScheduledResource stores the scheduled resource information
func (s *Scheduler) storeScheduledResource(ctx context.Context, storageBackend storage.Storage, selected string, req ScheduleRequest) error {
	schedKey := "scheduled:" + selected
	var schedList []ScheduledResource

	// Get existing scheduled resources
	schedResource, err := storageBackend.GetResource(ctx, schedKey)
	if err == nil && schedResource != nil {
		// Resource exists, unmarshal the scheduled resources
		if err := json.Unmarshal([]byte(schedResource.Status), &schedList); err != nil {
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
		return fmt.Errorf("failed to marshal scheduled resources: %w", err)
	}

	err = storageBackend.SetResource(ctx, &storage.Resource{
		ID:        schedKey,
		Status:    string(schedData),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, ScheduledResourceTTL) // TTL for scheduled resource information

	if err != nil {
		return fmt.Errorf("failed to save scheduled resources: %w", err)
	}

	return nil
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

// ScheduleAlgorithmResult represents the result of the scheduling algorithm
type ScheduleAlgorithmResult struct {
	LxdAPIAddress string
	Score         int
}

// Schedule selects the best host based on resource availability and load balancing criteria
func Schedule(resources map[string]LXDResource, req ScheduleRequest) (string, bool) {
	type resource struct {
		lxdAPIAddress string
		res           LXDResource
	}

	// Convert map to slice for easier processing
	var input []resource
	for lxdAPIAddress, res := range resources {
		input = append(input, resource{
			lxdAPIAddress: lxdAPIAddress,
			res:           res,
		})
	}

	// Filter hosts that have enough resources
	var candidates []resource
	for _, r := range input {
		availableCPU := r.res.Resource.CPUTotal - r.res.Resource.CPUUsed
		availableMemory := r.res.Resource.MemoryTotal - r.res.Resource.MemoryUsed

		if availableCPU >= uint64(req.CPU) && availableMemory >= uint64(req.Memory) {
			candidates = append(candidates, r)
		}
	}

	// If no host has enough resources, return empty
	if len(candidates) == 0 {
		return "", false
	}

	// Score each candidate based on multiple factors
	type scoredResource struct {
		resource resource
		score    int
	}

	var scored []scoredResource
	for _, r := range candidates {
		score := calculateScore(r.res, req)
		scored = append(scored, scoredResource{
			resource: r,
			score:    score,
		})
	}

	// Sort by score (higher is better)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Select the best candidate
	// If multiple candidates have the same highest score, randomly select one for better load distribution
	bestScore := scored[0].score
	var bestCandidates []resource
	for _, sr := range scored {
		if sr.score == bestScore {
			bestCandidates = append(bestCandidates, sr.resource)
		} else {
			break // Since sorted, no more candidates with the same score
		}
	}

	// Randomly select from the best candidates
	randomIndex := rand.Intn(len(bestCandidates))
	return bestCandidates[randomIndex].lxdAPIAddress, true
}

// calculateScore calculates a score for a resource based on multiple factors
func calculateScore(res LXDResource, req ScheduleRequest) int {
	// Factor 1: Available resources (higher is better)
	availableCPU := res.Resource.CPUTotal - res.Resource.CPUUsed
	availableMemory := res.Resource.MemoryTotal - res.Resource.MemoryUsed

	// Normalize available resources to a score (0-100)
	cpuScore := int((float64(availableCPU) / float64(res.Resource.CPUTotal)) * 50)
	memoryScore := int((float64(availableMemory) / float64(res.Resource.MemoryTotal)) * 50)

	// Factor 2: Number of instances (lower is better)
	instanceCount := len(res.Resource.Instances)
	instanceScore := 100 - instanceCount // Assuming max 100 instances, adjust as needed

	// Combine scores with weights
	totalScore := cpuScore + memoryScore + instanceScore

	// Ensure score is non-negative
	if totalScore < 0 {
		return 0
	}

	return totalScore
}
