package api

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sort"
	"strconv"
	"sync"
	"time"

	lxd "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"

	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"
)

const (
	ConfigKeyResourceType = "user.myshoes_resource_type"
	ConfigKeyImageAlias   = "user.myshoes_image_alias"
	ConfigKeyRunnerName   = "user.myshoes_runner_name"
	ConfigKeyAllocatedAt  = "user.myshoes_allocated_at"
)

func getInstancesWithTimeout(h lxdclient.LXDHost, d time.Duration) (s []api.Instance, overCommitPercent uint64, err error) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		s, err = lxdclient.GetAnyInstances(h.Client)
		if err != nil {
			return
		}
		r, err := lxdclient.GetResource(h.HostConfig)
		if err != nil {
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
				err = fmt.Errorf("failed to parse limits.cpu: %w", err)
				return
			}
			used += uint64(cpu)
		}
		overCommitPercent = uint64(float64(used) / float64(r.CPUTotal) * 100)
	}()
	select {
	case <-done:
		return
	case <-time.After(d):
		return nil, 0, errors.New("timed out")
	}
}

type instance struct {
	Host         *lxdclient.LXDHost
	InstanceName string
}

func findInstances(targets []lxdclient.LXDHost, match func(api.Instance) bool, limitOverCommit uint64) []instance {
	type result struct {
		host              *lxdclient.LXDHost
		overCommitPercent uint64
		instances         []string
	}
	rs := make([]result, len(targets))

	wg := new(sync.WaitGroup)
	wg.Add(len(targets))
	for i, target := range targets {
		go func(i int, target lxdclient.LXDHost) {
			defer wg.Done()

			s, overCommitPercent, err := getInstancesWithTimeout(target, 10*time.Second)
			if err != nil {
				log.Printf("failed to find instance in host %q: %+v", target.HostConfig.LxdHost, err)
				return
			}
			if limitOverCommit > 0 && overCommitPercent >= limitOverCommit {
				log.Printf("host %q reached over commit limit: current=%d limit=%d", target.HostConfig.LxdHost, overCommitPercent, limitOverCommit)
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

func findInstanceByJob(targets []lxdclient.LXDHost, runnerName string) (*lxdclient.LXDHost, string, bool) {
	s := findInstances(targets, func(i api.Instance) bool {
		return i.Config[ConfigKeyRunnerName] == runnerName
	}, 0)
	if len(s) < 1 {
		return nil, "", false
	}
	return s[0].Host, s[0].InstanceName, true
}

func allocatePooledInstance(targets []lxdclient.LXDHost, resourceType, imageAlias string, limitOverCommit uint64, runnerName string) (*lxdclient.LXDHost, string, error) {
	s := findInstances(targets, func(i api.Instance) bool {
		if i.StatusCode != api.Frozen {
			return false
		}
		if i.Config[ConfigKeyResourceType] != resourceType {
			return false
		}
		if i.Config[ConfigKeyImageAlias] != imageAlias {
			return false
		}
		if _, ok := i.Config[ConfigKeyRunnerName]; ok {
			return false
		}
		return true
	}, limitOverCommit)

	for _, i := range s {
		if err := allocateInstance(*i.Host, i.InstanceName, runnerName); err != nil {
			log.Printf("failed to allocate instance %q in host %q (trying another instance): %+v", i.InstanceName, i.Host.HostConfig.LxdHost, err)
			continue
		}
		return i.Host, i.InstanceName, nil
	}

	return nil, "", fmt.Errorf("no available instance for resource_type=%q image_alias=%q", resourceType, imageAlias)
}

func allocateInstance(host lxdclient.LXDHost, instanceName, runnerName string) error {
	i, etag, err := host.Client.GetInstance(instanceName)
	if err != nil {
		return fmt.Errorf("get instance: %w", err)
	}

	if _, ok := i.Config[ConfigKeyRunnerName]; ok {
		return fmt.Errorf("already allocated instance %q in host %q", instanceName, host.HostConfig.LxdHost)
	}

	log.Printf("Allocating %q to %q", instanceName, runnerName)

	i.InstancePut.Config[ConfigKeyRunnerName] = runnerName
	i.InstancePut.Config[ConfigKeyAllocatedAt] = time.Now().UTC().Format(time.RFC3339Nano)

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
	if i.Config[ConfigKeyRunnerName] != runnerName {
		return fmt.Errorf("updated instance config mismatch: got=%q expected=%q", i.Config[ConfigKeyRunnerName], runnerName)
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
