package cmd

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"os"
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
	currentImage      struct {
		Hash      string
		CreatedAt time.Time
	}
}

var (
	ErrAlreadyStopped = errors.New("The instance is already stopped")
)

type instances map[string]struct{}

func newAgent(ctx context.Context, conf Config) (*Agent, error) {
	source, err := slm.ParseAlias(conf.ImageAlias)
	if err != nil {
		return nil, err
	}
	c, err := lxd.ConnectLXDUnixWithContext(ctx, "", &lxd.ConnectionArgs{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect lxd")
	}
	checkInterval, waitIdleTime, zombieAllowTime, err := LoadParams()
	if err != nil {
		return nil, errors.Wrap(err, "failed to load params")
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
		currentImage: struct {
			Hash      string
			CreatedAt time.Time
		}{Hash: "", CreatedAt: time.Time{}},

		creatingInstances: creatingInstances,
		deletingInstances: make(instances),
	}
	return agent, nil
}

func (a *Agent) reloadConfig(ctx context.Context) {
	conf, err := LoadConfig()
	if err != nil {
		log.Printf("failed to load config: %+v", err)
		return
	}
	if conf.ImageAlias != a.ImageAlias {
		source, err := slm.ParseAlias(conf.ImageAlias)
		if err != nil {
			log.Printf("failed to parse image alias: %+v", err)
			return
		}
		a.InstanceSource = *source
		a.ImageAlias = conf.ImageAlias
	}
	a.ResourceTypesMap = conf.ResourceTypesMap
	a.ResouceTypesCounts = conf.ResourceTypesCounts
}

// Run runs the agent.
func (a *Agent) Run(ctx context.Context, sigHupCh chan os.Signal) error {
	ticker := time.NewTicker(a.CheckInterval)
	defer ticker.Stop()

	log.Println("Started agent")

	for {
		select {
		case <-sigHupCh:
			log.Println("Received SIGHUP. Reloading config...")
			a.reloadConfig(ctx)
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

	toDelete := []string{}

	for _, rt := range a.ResourceTypesMap {
		current := a.countPooledInstances(s, rt.Name)
		creating := len(a.creatingInstances[rt.Name])
		rtCount, ok := a.ResouceTypesCounts[rt.Name]
		if !ok {
			toDelete = append(toDelete, rt.Name)
			continue
		} else if rtCount == 0 {
			toDelete = append(toDelete, rt.Name)
			continue
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
		if _, ok := a.ResouceTypesCounts[i.Config[configKeyResourceType]]; !ok {
			toDelete = append(toDelete, i.Config[configKeyResourceType])
		}
		for _, rt := range toDelete {
			if i.Config[configKeyResourceType] == rt {
				log.Printf("Deleting disabled resource type instance %q...", i.Name)
				if err := a.deleteInstance(i); err != nil {
					log.Printf("failed to delete instance %q: %+v", i.Name, err)
				}
				log.Printf("Deleted disabled resource type instance %q", i.Name)
			}
		}
		if a.isZombieInstance(i) {
			log.Printf("Deleting zombie instance %q...", i.Name)
			if err := a.deleteInstance(i); err != nil {
				log.Printf("failed to delete zombie instance %q: %+v", i.Name, err)
			}
			log.Printf("Deleted zombie instance %q", i.Name)
		}
		if isOld, err := a.isOldImageInstance(i); err != nil {
			log.Printf("failed to check old image instance %q: %+v", i.Name, err)
		} else if isOld {
			log.Printf("Deleting old image instance %q...", i.Name)
			if err := a.deleteInstance(i); err != nil {
				log.Printf("failed to delete old image instance %q: %+v", i.Name, err)
			}
			log.Printf("Deleted old image instance %q", i.Name)
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

func (a *Agent) isOldImageInstance(i api.Instance) (bool, error) {
	baseImage, ok := i.Config["volatile.base_image"]
	if !ok {
		return false, fmt.Errorf("Failed to get volatile.base_image")
	}
	if baseImage != a.currentImage.Hash {
		if i.CreatedAt.Before(a.currentImage.CreatedAt) {
			if i.StatusCode == api.Frozen {
				return true, nil
			}
			return false, nil
		} else {
			a.currentImage.Hash = baseImage
			a.currentImage.CreatedAt = i.CreatedAt
			return false, nil
		}
	}
	return false, nil
}

func (a *Agent) deleteInstance(i api.Instance) error {
	if _, ok := a.deletingInstances[i.Name]; ok {
		return nil
	}
	a.deletingInstances[i.Name] = struct{}{}
	defer delete(a.deletingInstances, i.Name)
	_, etag, err := a.Client.GetInstance(i.Name)
	if err != nil {
		return fmt.Errorf("get instance %q: %w", i.Name, err)
	}
	stopOp, err := a.Client.UpdateInstanceState(i.Name, api.InstanceStatePut{Action: "stop", Timeout: -1, Force: true}, etag)
	if err != nil && !errors.Is(err, ErrAlreadyStopped) {
		return fmt.Errorf("failed to stop instance %q: %+v", i.Name, err)
	}
	if err := stopOp.Wait(); err != nil && !errors.Is(err, ErrAlreadyStopped) {
		return fmt.Errorf("failed to stop instance %q: %+v", i.Name, err)
	}
	deleteOp, err := a.Client.DeleteInstance(i.Name)
	if err != nil {
		return fmt.Errorf("delete instance %q: %w", i.Name, err)
	}
	if err := deleteOp.Wait(); err != nil {
		return fmt.Errorf("delete instance %q operation: %w", i.Name, err)
	}
	return nil
}
