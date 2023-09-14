package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"sync"
	"time"

	lxd "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
	"golang.org/x/sync/semaphore"
)

type Agent struct {
	ImageAlias     string
	InstanceSource api.InstanceSource

	ResourceTypes []ResourceType
	Client        lxd.InstanceServer

	CheckInterval         time.Duration
	ConcurrentCreateLimit int64
	WaitIdleTime          time.Duration
	ZombieAllowTime       time.Duration

	wg                *sync.WaitGroup
	createLimit       *semaphore.Weighted
	creatingInstances map[string]map[string]struct{}
	deletingInstances map[string]struct{}
}

func (a *Agent) Run(ctx context.Context) error {
	ticker := time.NewTicker(a.CheckInterval)

	a.wg = new(sync.WaitGroup)
	a.createLimit = semaphore.NewWeighted(a.ConcurrentCreateLimit)
	a.deletingInstances = make(map[string]struct{})

	a.creatingInstances = make(map[string]map[string]struct{}, len(a.ResourceTypes))
	for _, rt := range a.ResourceTypes {
		a.creatingInstances[rt.Name] = make(map[string]struct{})
	}

	log.Printf("Started agent")

	for {
		select {
		case <-ticker.C:
			if err := a.checkInstances(ctx); err != nil {
				log.Printf("failed to check instances: %+v", err)
			}
		case <-ctx.Done():
			log.Printf("Stopping agent...")
			a.wg.Wait()
			return ctx.Err()
		}
	}
}

func (a *Agent) countPooledInstances(instances []api.Instance, resourceTypeName string) int {
	var count int
	for _, i := range instances {
		if i.StatusCode != api.Frozen {
			continue
		}
		if i.Config[configKeyImageAlias] != a.ImageAlias {
			continue
		}
		if i.Config[configKeyResourceType] != resourceTypeName {
			continue
		}
		if _, ok := i.Config[configKeyRunnerName]; ok {
			continue
		}
		count++
	}
	return count
}

func (a *Agent) generateInstanceName() (string, error) {
	var b [4]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return "", fmt.Errorf("generate random id: %w", err)
	}
	return fmt.Sprintf("myshoes-runner-%x", b), nil
}

func (a *Agent) checkInstances(ctx context.Context) error {
	s, err := a.Client.GetInstances(api.InstanceTypeAny)
	if err != nil {
		return fmt.Errorf("get instances: %w", err)
	}

	for _, rt := range a.ResourceTypes {
		current := a.countPooledInstances(s, rt.Name)
		creating := len(a.creatingInstances[rt.Name])
		createCount := rt.PoolCount - current - creating
		if createCount < 1 {
			continue
		}
		log.Printf("Create %d instances for %q", createCount, rt.Name)
		for i := 0; i < createCount; i++ {
			name, err := a.generateInstanceName()
			if err != nil {
				return fmt.Errorf("generate instance name: %w", err)
			}
			a.creatingInstances[rt.Name][name] = struct{}{}
			go func(name string, rt ResourceType) {
				a.createLimit.Acquire(context.Background(), 1)
				defer a.createLimit.Release(1)

				defer delete(a.creatingInstances[rt.Name], name)

				a.wg.Add(1)
				defer a.wg.Done()
				select {
				case <-ctx.Done():
					// context cancelled, stop creating immediately
					return
				default:
					// context is not cancelled, continue
				}

				if err := a.createInstance(name, rt); err != nil {
					log.Printf("failed to create instance %q: %+v", name, err)
				}
			}(name, rt)
		}
	}

	for _, i := range s {
		if a.isZombieInstance(i) {
			if _, ok := a.deletingInstances[i.Name]; ok {
				continue
			}
			log.Printf("Deleting zombie instance %q...", i.Name)
			a.deletingInstances[i.Name] = struct{}{}
			a.wg.Add(1)
			go func(i api.Instance) {
				defer a.wg.Done()
				defer delete(a.deletingInstances, i.Name)

				if err := a.deleteZombieInstance(i); err != nil {
					log.Printf("failed to delete zombie instance %q: %+v", i.Name, err)
				}
				log.Printf("Deleted zombie instance %q", i.Name)
			}(i)
		}
	}

	return nil
}

func (a *Agent) isZombieInstance(i api.Instance) bool {
	if i.StatusCode == api.Frozen {
		return false
	}
	if _, ok := i.Config[configKeyRunnerName]; ok {
		return false
	}
	if i.Config[configKeyImageAlias] != a.ImageAlias {
		return false
	}
	if i.CreatedAt.Add(a.ZombieAllowTime).After(time.Now()) {
		return false
	}
	if rt, ok := i.Config[configKeyResourceType]; !ok {
		return false
	} else if _, ok := a.creatingInstances[rt][i.Name]; ok {
		return false
	}
	return true
}

func (a *Agent) deleteZombieInstance(i api.Instance) error {
	if i.StatusCode == api.Running {
		op, err := a.Client.UpdateInstanceState(i.Name, api.InstanceStatePut{
			Action:  "stop",
			Timeout: -1,
		}, "")
		if err != nil {
			return fmt.Errorf("stop: %w", err)
		}
		if err := op.Wait(); err != nil {
			return fmt.Errorf("stop operation: %w", err)
		}
	}

	op, err := a.Client.DeleteInstance(i.Name)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("delete operation: %w", err)
	}

	return nil
}
