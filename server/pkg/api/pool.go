package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/docker/go-units"
	lxd "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
	"github.com/whywaita/myshoes/pkg/datastore"

	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/metric"
)

type gotInstances struct {
	Instances         []api.Instance
	OverCommitPercent uint64
	Error             error
}

func getInstancesWithTimeout(_ctx context.Context, h *lxdclient.LXDHost, d time.Duration, l *slog.Logger) ([]api.Instance, uint64, error) {
	ret := make(chan *gotInstances)
	ctx, cancel := context.WithTimeout(_ctx, d)
	defer cancel()
	go func() {
		defer close(ret)
		r, err := lxdclient.GetResource(ctx, h.HostConfig, l)
		if err != nil {
			ret <- &gotInstances{
				Instances:         nil,
				OverCommitPercent: 0,
				Error:             fmt.Errorf("failed to get resource: %w", err),
			}
			return
		}
		var used uint64
		for _, i := range r.Instances {
			if i.StatusCode != api.Running {
				continue
			}
			instanceCPU := i.Config["limits.cpu"]
			if instanceCPU == "" {
				continue
			}
			cpu, err := strconv.Atoi(i.Config["limits.cpu"])
			if err != nil {
				ret <- &gotInstances{
					Instances:         nil,
					OverCommitPercent: 0,
					Error:             fmt.Errorf("failed to parse limits.cpu: %w", err),
				}
				return
			}
			used += uint64(cpu)
		}
		overCommitPercent := uint64(float64(used) / float64(r.CPUTotal) * 100)
		ret <- &gotInstances{
			Instances:         r.Instances,
			OverCommitPercent: overCommitPercent,
			Error:             nil,
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil, 0, errors.New("timed out")
		case r := <-ret:
			return r.Instances, r.OverCommitPercent, r.Error
		}
	}
}

type instance struct {
	Host         *lxdclient.LXDHost
	InstanceName string
}

func findInstances(ctx context.Context, targets []*lxdclient.LXDHost, match func(api.Instance) bool, limitOverCommit uint64, l *slog.Logger) []instance {
	type result struct {
		host              *lxdclient.LXDHost
		overCommitPercent uint64
		instances         []string
	}
	rs := make([]result, len(targets))

	wg := new(sync.WaitGroup)
	for i, target := range targets {
		wg.Add(1)
		l := l.With("host", target.HostConfig.LxdHost)
		go func(i int, target *lxdclient.LXDHost) {
			defer wg.Done()

			s, overCommitPercent, err := getInstancesWithTimeout(ctx, target, 10*time.Second, l)
			if err != nil {
				l.Info("failed to find instance", "err", err)
				return
			}
			if limitOverCommit > 0 && overCommitPercent >= limitOverCommit {
				l.Info("host reached over commit limit", "current", overCommitPercent, "limit", limitOverCommit)
				return
			}

			var instances []string
			for _, i := range s {
				if match(i) {
					instances = append(instances, i.Name)
				}
			}

			// Shuffle instances to reduce conflicting
			rand.Shuffle(len(instances), func(i, j int) {
				instances[i], instances[j] = instances[j], instances[i]
			})

			rs[i] = result{
				host:              target,
				overCommitPercent: overCommitPercent,
				instances:         instances,
			}
		}(i, target)
	}
	wg.Wait()

	sort.Slice(rs, func(i, j int) bool {
		return rs[i].overCommitPercent < rs[j].overCommitPercent
	})

	var instances []instance
	for _, r := range rs {
		for _, i := range r.instances {
			instances = append(instances, instance{
				Host:         r.host,
				InstanceName: i,
			})
		}
	}

	return instances
}

func findInstanceByJob(ctx context.Context, targets []*lxdclient.LXDHost, runnerName string, l *slog.Logger) (*lxdclient.LXDHost, string, bool) {
	s := findInstances(ctx, targets, func(i api.Instance) bool {
		return i.Config[lxdclient.ConfigKeyRunnerName] == runnerName && i.StatusCode == api.Frozen
	}, 0, l)
	if len(s) < 1 {
		return nil, "", false
	}
	return s[0].Host, s[0].InstanceName, true
}

func (s *ShoesLXDMultiServer) allocatePooledInstance(ctx context.Context, targets []*lxdclient.LXDHost, resourceType, imageAlias string, limitOverCommit uint64, runnerName string, l *slog.Logger) (*lxdclient.LXDHost, string, error) {
	// Use scheduler if configured
	if s.schedulerClient != nil {
		if selectedHost, err := s.selectHostUsingScheduler(ctx, targets, resourceType, l); err == nil && selectedHost != nil {
			// Filter targets to only include the selected host
			targets = []*lxdclient.LXDHost{selectedHost}
		} else {
			l.Warn("scheduler failed, falling back to default algorithm", "err", err)
		}
	}

	instances := findInstances(ctx, targets, func(i api.Instance) bool {
		if i.StatusCode != api.Frozen {
			return false
		}
		if i.Config[lxdclient.ConfigKeyResourceType] != resourceType {
			return false
		}
		if i.Config[lxdclient.ConfigKeyImageAlias] != imageAlias {
			return false
		}
		if _, ok := i.Config[lxdclient.ConfigKeyRunnerName]; ok {
			return false
		}
		return true
	}, limitOverCommit, l)

	for _, i := range instances {
		l := l.With("host", i.Host.HostConfig.LxdHost, "instance", i.InstanceName)
		if err := allocateInstance(i.Host, i.InstanceName, runnerName, l); err != nil {
			l.Info("failed to allocate instance (trying another instance)", "err", err)
			metric.FailedLxdAllocate.WithLabelValues(i.Host.HostConfig.LxdHost, runnerName).Set(1)
			continue
		}
		metric.FailedLxdAllocate.DeleteLabelValues(i.Host.HostConfig.LxdHost, runnerName)
		return i.Host, i.InstanceName, nil
	}

	return nil, "", fmt.Errorf("no available instance for resource_type=%q image_alias=%q", resourceType, imageAlias)
}

// selectHostUsingScheduler uses the scheduler to select the best host
func (s *ShoesLXDMultiServer) selectHostUsingScheduler(ctx context.Context, targets []*lxdclient.LXDHost, resourceType string, l *slog.Logger) (*lxdclient.LXDHost, error) {
	// Get resource requirements for the given resource type
	cpu, memory, err := s.getResourceRequirements(resourceType)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource requirements: %w", err)
	}

	// Extract host names from targets
	var targetHosts []string
	for _, target := range targets {
		targetHosts = append(targetHosts, target.HostConfig.LxdHost)
	}

	// Call scheduler API
	schedReq := ScheduleRequest{
		CPU:         cpu,
		Memory:      memory,
		TargetHosts: targetHosts,
	}

	schedResp, err := s.schedulerClient.Schedule(ctx, schedReq)
	if err != nil {
		return nil, fmt.Errorf("scheduler API call failed: %w", err)
	}

	// Find the selected host in our targets
	for _, target := range targets {
		l.Info("checking target host", "host", target.HostConfig.LxdHost, "scheduler_host", schedResp.Host)
		if target.HostConfig.LxdHost == schedResp.Host {
			l.Info("scheduler selected host", "host", schedResp.Host, "cpu", cpu, "memory", memory)
			return target, nil
		}
	}

	return nil, fmt.Errorf("scheduler selected host %q not found in targets", schedResp.Host)
}

// getResourceRequirements converts resource type to CPU and memory requirements
func (s *ShoesLXDMultiServer) getResourceRequirements(resourceType string) (int, int, error) {
	rt := datastore.UnmarshalResourceType(resourceType)
	if rt == datastore.ResourceTypeUnknown {
		return 0, 0, fmt.Errorf("unknown resource type: %s", resourceType)
	}

	mapping, ok := s.resourceMapping[rt.ToPb()]
	if !ok {
		// Default values if no mapping is configured
		return 1, 1024 * 1024 * 1024, nil // 1 CPU, 1GB memory
	}

	// Parse memory string (e.g., "2GB", "1024MB")
	memoryBytes, err := units.FromHumanSize(mapping.Memory)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse memory size %q: %w", mapping.Memory, err)
	}

	return mapping.CPUCore, int(memoryBytes), nil
}

func allocateInstance(host *lxdclient.LXDHost, instanceName, runnerName string, l *slog.Logger) error {
	host.APICallMutex.Lock()
	defer host.APICallMutex.Unlock()
	timer := metric.NewLXDAPITimer(host.HostConfig.LxdHost, "GetInstance")
	i, etag, err := host.Client.GetInstance(instanceName)
	timer.ObserveDuration(err)
	if err != nil {
		return fmt.Errorf("get instance: %w", err)
	}

	if _, ok := i.Config[lxdclient.ConfigKeyRunnerName]; ok {
		return fmt.Errorf("already allocated instance %q in host %q", instanceName, host.HostConfig.LxdHost)
	}

	l.Info("Allocating instance to runner")

	i.InstancePut.Config[lxdclient.ConfigKeyRunnerName] = runnerName
	i.InstancePut.Config[lxdclient.ConfigKeyAllocatedAt] = time.Now().UTC().Format(time.RFC3339Nano)

	timer = metric.NewLXDAPITimer(host.HostConfig.LxdHost, "UpdateInstance")
	op, err := host.Client.UpdateInstance(instanceName, i.InstancePut, etag)
	timer.ObserveDuration(err)
	if err != nil {
		return fmt.Errorf("update instance: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("waiting operation: %w", err)
	}

	// Workaround for https://github.com/canonical/lxd/issues/12189
	timer = metric.NewLXDAPITimer(host.HostConfig.LxdHost, "GetInstance")
	i, _, err = host.Client.GetInstance(instanceName)
	timer.ObserveDuration(err)
	if err != nil {
		return fmt.Errorf("get instance: %w", err)
	}
	if i.Config[lxdclient.ConfigKeyRunnerName] != runnerName {
		return fmt.Errorf("updated instance config mismatch: got=%q expected=%q", i.Config[lxdclient.ConfigKeyRunnerName], runnerName)
	}

	return nil
}

func recoverInvalidInstance(c lxd.InstanceServer, instanceName, host string) error {
	timer := metric.NewLXDAPITimer(host, "DeleteInstance")
	op, err := c.DeleteInstance(instanceName)
	timer.ObserveDuration(err)
	if err != nil {
		return fmt.Errorf("delete instance: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("waiting operation: %w", err)
	}
	return nil
}

func unfreezeInstance(c lxd.InstanceServer, instanceName, host string) error {
	timer := metric.NewLXDAPITimer(host, "GetInstanceState")
	state, etag, err := c.GetInstanceState(instanceName)
	timer.ObserveDuration(err)
	if err != nil {
		return fmt.Errorf("get instance state: %w", err)
	}
	switch state.StatusCode {
	case api.Running:
		// do nothing
	case api.Frozen:
		timer = metric.NewLXDAPITimer(host, "UpdateInstanceState")
		op, err := c.UpdateInstanceState(instanceName, api.InstanceStatePut{
			Action:  "unfreeze",
			Timeout: -1,
		}, etag)
		timer.ObserveDuration(err)
		if err != nil {
			return fmt.Errorf("update instance state: %w", err)
		}
		if err := op.Wait(); err != nil {
			return fmt.Errorf("waiting operation: %w", err)
		}
	default:
		return fmt.Errorf("unexpected instance state: %s", state.StatusCode.String())
	}
	return nil
}
