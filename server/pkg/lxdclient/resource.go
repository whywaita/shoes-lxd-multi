package lxdclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"github.com/whywaita/shoes-lxd-multi/server/pkg/config"

	"github.com/docker/go-units"
	lxd "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
	"github.com/whywaita/xsemaphore"
)

// Resource is resource of lxd host
type Resource struct {
	CPUTotal    uint64
	MemoryTotal uint64
	CPUUsed     uint64
	MemoryUsed  uint64
}

const (
	// ConfigKeyResourceType is key of resource type
	ConfigKeyResourceType = "user.myshoes_resource_type"
	// ConfigKeyImageAlias is key of image alias
	ConfigKeyImageAlias = "user.myshoes_image_alias"
	// ConfigKeyRunnerName is key of runner name
	ConfigKeyRunnerName = "user.myshoes_runner_name"
	// ConfigKeyAllocatedAt is key of allocated at
	ConfigKeyAllocatedAt = "user.myshoes_allocated_at"
)

// GetCPUOverCommitPercent calculate percent of over commit
func GetCPUOverCommitPercent(in Resource) uint64 {
	return uint64(float64(in.CPUUsed) / float64(in.CPUTotal) * 100.0)
}

// GetResource get Resource
func GetResource(ctx context.Context, hostConfig config.HostConfig, logger *slog.Logger) (*Resource, error) {
	status, err := GetStatusCache(hostConfig.LxdHost)
	if err == nil {
		// found from cache
		return &status.Resource, nil
	}
	if err != nil && !errors.Is(err, ErrCacheNotFound) {
		return nil, fmt.Errorf("failed to get status from cache: %w", err)
	}

	logger.Warn("failed to get status from cache, so scrape from lxd")

	r, _, _, err := GetResourceFromLXD(ctx, hostConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource from lxd: %w", err)
	}
	s := LXDStatus{
		Resource:   *r,
		HostConfig: hostConfig,
	}
	if err := SetStatusCache(hostConfig.LxdHost, s); err != nil {
		return nil, fmt.Errorf("failed to set status to cache: %w", err)
	}

	return r, nil
}

// GetResourceFromLXD get resources from LXD API
func GetResourceFromLXD(ctx context.Context, hostConfig config.HostConfig, logger *slog.Logger) (*Resource, []api.Instance, string, error) {
	client, err := ConnectLXDWithTimeout(hostConfig.LxdHost, hostConfig.LxdClientCert, hostConfig.LxdClientKey)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to connect lxd: %w", err)
	}

	return GetResourceFromLXDWithClient(ctx, *client, hostConfig.LxdHost, logger)
}

// GetResourceFromLXDWithClient get resources from LXD API with client
func GetResourceFromLXDWithClient(ctx context.Context, client lxd.InstanceServer, host string, logger *slog.Logger) (*Resource, []api.Instance, string, error) {
	sem := xsemaphore.Get(host, 1)
	if err := sem.Acquire(ctx, 1); err != nil {
		return nil, nil, "", fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer sem.Release(1)

	cpuTotal, memoryTotal, hostname, err := ScrapeLXDHostResources(client, host, logger)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to scrape total resource: %w", err)
	}
	instances, err := GetAnyInstances(client)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to retrieve list of instance: %w", err)
	}
	cpuUsed, memoryUsed, err := ScrapeLXDHostAllocatedResources(instances)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to scrape allocated resource: %w", err)
	}

	r := Resource{
		CPUTotal:    cpuTotal,
		MemoryTotal: memoryTotal,
		CPUUsed:     cpuUsed,
		MemoryUsed:  memoryUsed,
	}

	return &r, instances, hostname, nil
}

var (
	// LXDHostResourceCache is cache of LXD resource
	LXDHostResourceCache sync.Map
)

// LXDHostResource is resource of LXD host
type LXDHostResource struct {
	CPUTotal    uint64
	MemoryTotal uint64
	Hostname    string
}

// ScrapeLXDHostResources scrape all resources
func ScrapeLXDHostResources(client lxd.InstanceServer, host string, logger *slog.Logger) (uint64, uint64, string, error) {
	v, ok := LXDHostResourceCache.Load(host)
	if ok {
		r := v.(LXDHostResource)
		return r.CPUTotal, r.MemoryTotal, r.Hostname, nil
	}

	logger.Warn("failed to get host resource from cache, so scrape from lxd")

	cpuTotal, memoryTotal, hostname, err := ScrapeLXDHostResourcesFromLXD(client)
	if err != nil {
		return 0, 0, "", fmt.Errorf("failed to scrape total resource: %w", err)
	}

	LXDHostResourceCache.Store(host, LXDHostResource{
		CPUTotal:    cpuTotal,
		MemoryTotal: memoryTotal,
		Hostname:    hostname,
	})

	return cpuTotal, memoryTotal, hostname, nil
}

// ScrapeLXDHostResourcesFromLXD scrape all resources
func ScrapeLXDHostResourcesFromLXD(client lxd.InstanceServer) (uint64, uint64, string, error) {
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
		if instance.StatusCode == api.Frozen {
			continue
		}
		instanceCPU := instance.Config["limits.cpu"]
		if instanceCPU != "" {
			cpu, err := strconv.Atoi(instance.Config["limits.cpu"])
			if err != nil {
				return 0, 0, fmt.Errorf("failed to convert limits.cpu: %w", err)
			}
			allocatedCPU += uint64(cpu)
		} else {
			slog.Warn("instance hasn't limits.cpu", "instance", instance.Name)
		}

		instanceMemory := instance.Config["limits.memory"]
		if instanceMemory != "" {
			memory, err := units.FromHumanSize(instance.Config["limits.memory"])
			if err != nil {
				return 0, 0, fmt.Errorf("failed to convert limits.memory: %w", err)
			}
			allocatedMemory += uint64(memory)
		} else {
			slog.Warn("instance hasn't limits.memory", "instance", instance.Name)
		}
	}

	return allocatedCPU, allocatedMemory, nil
}
