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

	lxd "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"

	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/metric"
)

type gotInstances struct {
	Instances         []api.Instance
	OverCommitPercent uint64
	Error             error
}

func getInstancesWithTimeout(_ctx context.Context, h lxdclient.LXDHost, d time.Duration, l *slog.Logger) ([]api.Instance, uint64, error) {
	ret := make(chan *gotInstances)
	ctx, cancel := context.WithTimeout(_ctx, d)
	defer cancel()
	go func() {
		defer close(ret)
		s, err := lxdclient.GetAnyInstances(h.Client)
		if err != nil {
			ret <- &gotInstances{
				Instances:         nil,
				OverCommitPercent: 0,
				Error:             fmt.Errorf("failed to get instances: %w", err),
			}
			return
		}
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
		for _, i := range s {
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
			Instances:         s,
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

func findInstances(ctx context.Context, targets []lxdclient.LXDHost, match func(api.Instance) bool, limitOverCommit uint64, l *slog.Logger) []instance {
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
		go func(i int, target lxdclient.LXDHost) {
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
				host:              &target,
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

func findInstanceByJob(ctx context.Context, targets []lxdclient.LXDHost, runnerName string, l *slog.Logger) (*lxdclient.LXDHost, string, bool) {
	s := findInstances(ctx, targets, func(i api.Instance) bool {
		return i.Config[lxdclient.ConfigKeyRunnerName] == runnerName
	}, 0, l)
	if len(s) < 1 {
		return nil, "", false
	}
	return s[0].Host, s[0].InstanceName, true
}

func allocatePooledInstance(ctx context.Context, targets []lxdclient.LXDHost, resourceType, imageAlias string, limitOverCommit uint64, runnerName string, l *slog.Logger) (*lxdclient.LXDHost, string, error) {
	s := findInstances(ctx, targets, func(i api.Instance) bool {
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

	for _, i := range s {
		l := l.With("stadium", i.Host.HostConfig.LxdHost, "instance", i.InstanceName)
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

func allocateInstance(host *lxdclient.LXDHost, instanceName, runnerName string, l *slog.Logger) error {
	i, etag, err := host.Client.GetInstance(instanceName)
	if err != nil {
		return fmt.Errorf("get instance: %w", err)
	}

	if _, ok := i.Config[lxdclient.ConfigKeyRunnerName]; ok {
		return fmt.Errorf("already allocated instance %q in host %q", instanceName, host.HostConfig.LxdHost)
	}

	l.Info("Allocating instance to runner")

	i.InstancePut.Config[lxdclient.ConfigKeyRunnerName] = runnerName
	i.InstancePut.Config[lxdclient.ConfigKeyAllocatedAt] = time.Now().UTC().Format(time.RFC3339Nano)

	op, err := host.Client.UpdateInstance(instanceName, i.InstancePut, etag)
	if err != nil {
		return fmt.Errorf("update instance: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("waiting operation: %w", err)
	}

	// Workaround for https://github.com/canonical/lxd/issues/12189
	i, _, err = host.Client.GetInstance(instanceName)
	if err != nil {
		return fmt.Errorf("get instance: %w", err)
	}
	if i.Config[lxdclient.ConfigKeyRunnerName] != runnerName {
		return fmt.Errorf("updated instance config mismatch: got=%q expected=%q", i.Config[lxdclient.ConfigKeyRunnerName], runnerName)
	}

	return nil
}

func unfreezeInstance(c lxd.InstanceServer, name string) error {
	state, etag, err := c.GetInstanceState(name)
	if err != nil {
		return fmt.Errorf("get instance state: %w", err)
	}
	switch state.StatusCode {
	case api.Running:
		// do nothing
	case api.Frozen:
		op, err := c.UpdateInstanceState(name, api.InstanceStatePut{
			Action:  "unfreeze",
			Timeout: -1,
		}, etag)
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
