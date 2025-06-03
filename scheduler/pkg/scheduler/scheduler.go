package scheduler

import (
	"math/rand"
	"sort"
)

type Scheduler struct {
	ResourceManager *LXDResourceManager
}

func (s *Scheduler) Schedule(req ScheduleRequest) (string, bool) {
	return Schedule(s.ResourceManager.resources, req)
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
