package cmd

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"time"

	lxd "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
	"github.com/pkg/errors"
	slm "github.com/whywaita/shoes-lxd-multi/server/pkg/api"
)

// Agent is an agent for pool mode.
type Agent struct {
	ImageAlias     string
	InstanceSource api.InstanceSource

	ResourceTypesMap   []ResourceTypesMap
	ResouceTypesCounts ResourceTypesCounts
	Client             lxd.InstanceServer

	CheckInterval   time.Duration
	WaitIdleTime    time.Duration
	ZombieAllowTime time.Duration

	creatingInstances map[string]instances
	deletingInstances instances
}

type instances map[string]struct{}

func newAgent(conf Config) (*Agent, error) {
	source, err := slm.ParseAlias(conf.ImageAlias)
	if err != nil {
		return nil, err
	}
	c, err := lxd.ConnectLXDUnix("", &lxd.ConnectionArgs{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect lxd")
	}
	checkInterval, waitIdleTime, zombieAllowTime, err := LoadParams()
	if err != nil {
		return nil, err
	}
	creatingInstances := make(map[string]instances)
	for _, rt := range conf.ResourceTypesMap {
		creatingInstances[rt.Name] = make(instances)
	}
	agent := &Agent{
		ImageAlias:     conf.ImageAlias,
		InstanceSource: *source,

		ResourceTypesMap:   conf.ResourceTypesMap,
		ResouceTypesCounts: conf.ResourceTypesCounts,
		Client:             c,

		CheckInterval:   checkInterval,
		WaitIdleTime:    waitIdleTime,
		ZombieAllowTime: zombieAllowTime,

		creatingInstances: creatingInstances,
		deletingInstances: make(instances),
	}
	return agent, nil
}

// Run runs the agent.
func (a *Agent) Run(ctx context.Context) error {
	ticker := time.NewTicker(a.CheckInterval)
	defer ticker.Stop()

	log.Println("Started agent")

	for {
		select {
		case <-ticker.C:
			if err := a.checkInstances(ctx); err != nil {
				log.Printf("failed to check instances: %+v", err)
			}
		case <-ctx.Done():
			log.Printf("Stopping agent...")
			return nil
		}
	}
}

func (a *Agent) countPooledInstances(instances []api.Instance, resourceTypeName string) int {
	count := 0
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

	for _, rt := range a.ResourceTypesMap {
		current := a.countPooledInstances(s, rt.Name)
		creating := len(a.creatingInstances[rt.Name])
		rtCount, ok := a.ResouceTypesCounts[rt.Name]
		if !ok {
			return fmt.Errorf("get resource counts: %w", err)
		}
		createCount := rtCount - current - creating
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

			defer delete(a.creatingInstances[rt.Name], name)

			if err := a.createInstance(name, rt); err != nil {
				log.Printf("failed to create instance %q: %+v", name, err)
			}
		}
	}

	for _, i := range s {
		if a.isZombieInstance(i) {
			if _, ok := a.deletingInstances[i.Name]; ok {
				continue
			}
			log.Printf("Deleting zombie instance %q...", i.Name)
			a.deletingInstances[i.Name] = struct{}{}
			defer delete(a.deletingInstances, i.Name)

			if err := a.deleteZombieInstance(i); err != nil {
				log.Printf("failed to delete zombie instance %q: %+v", i.Name, err)
			}
			log.Printf("Deleted zombie instance %q", i.Name)
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
