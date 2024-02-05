package lxdclient

import (
	"errors"
	"fmt"
	"log"
	"log/slog"
	"strconv"

	"github.com/whywaita/shoes-lxd-multi/server/pkg/config"

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
	return uint64(float64(in.CPUUsed) / float64(in.CPUTotal) * 100.0)
}

// GetResource get Resource
func GetResource(hostConfig config.HostConfig, logger *slog.Logger) (*Resource, error) {
	status, err := GetStatusCache(hostConfig.LxdHost)
	if err == nil {
		// found from cache
		return &status.Resource, nil
	}
	if err != nil && !errors.Is(err, ErrCacheNotFound) {
		return nil, fmt.Errorf("failed to get status from cache: %w", err)
	}

	logger.Warn("failed to get status from cache, so scrape from lxd")

	client, err := ConnectLXDWithTimeout(hostConfig.LxdHost, hostConfig.LxdClientCert, hostConfig.LxdClientKey)
	if err != nil {
		return nil, fmt.Errorf("failed to connect lxd: %w", err)
	}

	cpuTotal, memoryTotal, _, err := ScrapeLXDHostResources(*client)
	if err != nil {
		return nil, fmt.Errorf("failed to scrape total resource: %w", err)
	}
	instances, err := GetAnyInstances(*client)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve list of instance: %w", err)
	}
	cpuUsed, memoryUsed, err := ScrapeLXDHostAllocatedResources(instances)
	if err != nil {
		return nil, fmt.Errorf("failed to scrape allocated resource: %w", err)
	}

	r := Resource{
		CPUTotal:    cpuTotal,
		MemoryTotal: memoryTotal,
		CPUUsed:     cpuUsed,
		MemoryUsed:  memoryUsed,
	}
	s := LXDStatus{
		Resource:   r,
		HostConfig: hostConfig,
	}
	if err := SetStatusCache(hostConfig.LxdHost, s); err != nil {
		return nil, fmt.Errorf("failed to set status to cache: %w", err)
	}

	return &r, nil
}

// ScrapeLXDHostResources scrape all resources
func ScrapeLXDHostResources(client lxd.InstanceServer) (uint64, uint64, string, error) {
	resources, err := client.GetServerResources()
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to get server resource: %w", err)
	}

	server, _, err := client.GetServer()
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to get server: %w", err)
	}

	return resources.CPU.Total, resources.Memory.Total, server.Environment.ServerName, nil
}

// GetAnyInstances get any instances from lxd
func GetAnyInstances(client lxd.InstanceServer) ([]api.Instance, error) {
	instances, err := client.GetInstances(api.InstanceTypeAny)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve instances: %w", err)
	}

	return instances, nil
}

// ScrapeLXDHostAllocatedResources scrape allocated resources
func ScrapeLXDHostAllocatedResources(instances []api.Instance) (uint64, uint64, error) {
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
