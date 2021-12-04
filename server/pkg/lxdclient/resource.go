package lxdclient

import (
	"fmt"
	"log"
	"strconv"

	"github.com/docker/go-units"
	lxd "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
)

// Resource is resource of lxd host
type Resource struct {
	CPUTotal    uint64
	MemoryTotal uint64
	CPUUsed     uint64
	MemoryUsed  uint64
}

// GetCPUOverCommitPercent calculate percent of over commit
func GetCPUOverCommitPercent(in Resource) uint64 {
	return (in.CPUUsed / in.CPUTotal) * 100
}

// GetResource get Resource
func GetResource(client lxd.InstanceServer) (*Resource, error) {
	cpuTotal, memoryTotal, err := ScrapeLXDHostResources(client)
	if err != nil {
		return nil, fmt.Errorf("failed to scrape total resource: %w", err)
	}
	cpuUsed, memoryUsed, err := ScrapeLXDHostAllocatedResources(client)
	if err != nil {
		return nil, fmt.Errorf("failed to scrape allocated resource: %w", err)
	}

	return &Resource{
		CPUTotal:    cpuTotal,
		MemoryTotal: memoryTotal,
		CPUUsed:     cpuUsed,
		MemoryUsed:  memoryUsed,
	}, nil
}

// ScrapeLXDHostResources scrape all resources
func ScrapeLXDHostResources(client lxd.InstanceServer) (uint64, uint64, error) {
	resources, err := client.GetServerResources()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get server resource: %w", err)
	}

	return resources.CPU.Total, resources.Memory.Total, nil
}

// ScrapeLXDHostAllocatedResources scrape allocated resources
func ScrapeLXDHostAllocatedResources(client lxd.InstanceServer) (uint64, uint64, error) {
	instances, err := client.GetInstances(api.InstanceTypeAny)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to retrieve instances: %w", err)
	}

	var allocatedCPU uint64
	var allocatedMemory uint64
	for _, instance := range instances {
		instanceCPU := instance.Config["limits.cpu"]
		if instanceCPU != "" {
			cpu, err := strconv.Atoi(instance.Config["limits.cpu"])
			if err != nil {
				return 0, 0, fmt.Errorf("failed to convert limits.cpu: %w", err)
			}
			allocatedCPU += uint64(cpu)
		} else {
			log.Printf("%s hasn't limits.cpu\n", instance.Name)
		}

		instanceMemory := instance.Config["limits.memory"]
		if instanceMemory != "" {
			memory, err := units.FromHumanSize(instance.Config["limits.memory"])
			if err != nil {
				return 0, 0, fmt.Errorf("failed to convert limits.memory: %w", err)
			}
			allocatedMemory += uint64(memory)
		} else {
			log.Printf("%s hasn't limits.memory\n", instance.Name)
		}
	}

	return allocatedCPU, allocatedMemory, nil
}
