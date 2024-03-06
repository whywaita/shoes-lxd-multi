package cmd

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"time"

	lxd "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
	"github.com/pkg/errors"
	slm "github.com/whywaita/shoes-lxd-multi/server/pkg/api"
	"github.com/prometheus/client_golang/prometheus"
)

// Agent is an agent for pool mode.
type Agent struct {
	ImageAlias     string
	InstanceSource api.InstanceSource

	ResourceTypesMap    []ResourceTypesMap
	ResourceTypesCounts ResourceTypesCounts
	Client              lxd.InstanceServer

	CheckInterval   time.Duration
	WaitIdleTime    time.Duration
	ZombieAllowTime time.Duration

	creatingInstances map[string]instances
	deletingInstances instances
	currentImage      struct {
		Hash      string
		CreatedAt time.Time
	}
	registry *prometheus.Registry
}

var (
	ErrAlreadyStopped = errors.New("The instance is already stopped")
)

type instances map[string]struct{}

func newAgent(ctx context.Context) (*Agent, error) {
	conf, err := LoadConfig()
	if err != nil {
		return nil, errors.Wrap(err, "failed to load config")
	}
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

	registry := prometheus.NewRegistry()
	registry.Register(configuredInstancesCount)
	registry.Register(lxdInstances)
	for k, v := range conf.ResourceTypesCounts {
		configuredInstancesCount.WithLabelValues(k).Set(float64(v))
	}
	agent := &Agent{
		ImageAlias:     conf.ImageAlias,
		InstanceSource: *source,

		ResourceTypesMap:    conf.ResourceTypesMap,
		ResourceTypesCounts: conf.ResourceTypesCounts,
		Client:              c,

		CheckInterval:   checkInterval,
		WaitIdleTime:    waitIdleTime,
		ZombieAllowTime: zombieAllowTime,
		currentImage: struct {
			Hash      string
			CreatedAt time.Time
		}{Hash: "", CreatedAt: time.Time{}},

		creatingInstances: creatingInstances,
		deletingInstances: make(instances),
		registry:          registry,
	}
	return agent, nil
}

func (a *Agent) reloadConfig() {
	conf, err := LoadConfig()
	if err != nil {
		slog.Error("failed to reloadconfig", "err", err.Error())
		return
	}

	for k, v := range conf.ResourceTypesCounts {
		configuredInstancesCount.WithLabelValues(k).Set(float64(v))
	}

	if conf.ImageAlias != a.ImageAlias {
		source, err := slm.ParseAlias(conf.ImageAlias)
		if err != nil {
			slog.Error("parse image alias: %+v", "err", err.Error())
			return
		}
		a.InstanceSource = *source
		a.ImageAlias = conf.ImageAlias
	}
	a.ResourceTypesMap = conf.ResourceTypesMap
	a.ResourceTypesCounts = conf.ResourceTypesCounts
}

// Run runs the agent.
func (a *Agent) Run(ctx context.Context, sigHupCh chan os.Signal) error {
	ticker := time.NewTicker(a.CheckInterval)
	defer ticker.Stop()

	slog.Info("Started agent")

	for {
		select {
		case <-sigHupCh:
			slog.Info("Received SIGHUP. Reloading config...")
			a.reloadConfig()
		case <-ticker.C:
			if err := a.checkInstances(); err != nil {
				slog.Error("failed to check instances", "err", err.Error())
			}
			if err := prometheus.WriteToTextfile(metricsPath, a.registry); err != nil {
				slog.Error("failed to write metrics: %+v", "err", err.Error())
			}
		case <-ctx.Done():
			slog.Info("Stopping agent...")
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

func generateInstanceName() (string, error) {
	var b [4]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return "", fmt.Errorf("generate random id: %w", err)
	}
	return fmt.Sprintf("myshoes-runner-%x", b), nil
}

func (a *Agent) checkInstances() error {
	s, err := a.Client.GetInstances(api.InstanceTypeAny)
	if err != nil {
		return fmt.Errorf("get instances: %w", err)
	}

	toDelete := []string{}

	for _, rt := range a.ResourceTypesMap {
		current := a.countPooledInstances(s, rt.Name)
		creating := len(a.creatingInstances[rt.Name])
		rtCount, ok := a.ResourceTypesCounts[rt.Name]
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
		slog.Info("Create instances", "count", createCount, "flavor", rt.Name)
		for i := 0; i < createCount; i++ {
			name, err := generateInstanceName()
			if err != nil {
				return fmt.Errorf("generate instance name: %w", err)
			}
			l := slog.With("instance", name, "flavor", rt.Name)
			a.creatingInstances[rt.Name][name] = struct{}{}

			defer delete(a.creatingInstances[rt.Name], name)

			if err := a.createInstance(name, rt, l); err != nil {
				l.Error("failed to create instance", "err", err.Error())
			}
		}
	}

	for _, i := range s {
		l := slog.With("instance", i.Name)
		lxdInstances.WithLabelValues(i.Name, i.Status, i.Config[configKeyResourceType]).Set(1)
		if _, ok := a.ResourceTypesCounts[i.Config[configKeyResourceType]]; !ok {
			toDelete = append(toDelete, i.Config[configKeyResourceType])
		}
		for _, rt := range toDelete {
			if i.Config[configKeyResourceType] == rt {
				l := l.With("flavor", rt)
				l.Info("Deleting disabled flavor instances")
				if err := a.deleteInstance(i); err != nil {
					l.Error("failed to delete instance", "err", err.Error())
				}
				l.Info("Deleted disabled flavor instance")
			}
		}
		if a.isZombieInstance(i) {
			l.Info("Deleting zombie instance")
			if err := a.deleteInstance(i); err != nil {
				l.Error("failed to delete zombie instance", "err", err.Error())
			}
			l.Info("Deleted zombie instance")
		}
		if isOld, err := a.isOldImageInstance(i); err != nil {
			l.Error("failed to check old image instance", "err", err.Error())
		} else if isOld {
			l.Info("Deleting old image instance")
			if err := a.deleteInstance(i); err != nil {
				l.Error("failed to delete old image instance", "err", err.Error())
			}
			l.Info("Deleted old image instance")
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
