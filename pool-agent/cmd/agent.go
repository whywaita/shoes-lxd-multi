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
	"github.com/prometheus/client_golang/prometheus"
	slm "github.com/whywaita/shoes-lxd-multi/server/pkg/api"
)

type AgentConfig struct {
	ImageAlias     string
	InstanceSource api.InstanceSource

	ResourceTypesCounts ResourceTypesCounts

	creatingInstances map[string]instances
	deletingInstances instances
	currentImage      struct {
		Hash      string
		CreatedAt time.Time
	}
}

// Agent is an agent for pool mode.
type Agent struct {
	Config           map[string]*AgentConfig
	CheckInterval    time.Duration
	WaitIdleTime     time.Duration
	ZombieAllowTime  time.Duration
	registry         *prometheus.Registry
	ResourceTypesMap ResourceTypesMap
	Client           lxd.InstanceServer
}

var (
	errAlreadyStopped = "The instance is already stopped"
)

type instances map[string]struct{}

func newAgent(ctx context.Context) (*Agent, error) {
	confmap, err := LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	ac := make(map[string]*AgentConfig, len(confmap.Config))
	for version, conf := range confmap.Config {
		s, err := slm.ParseAlias(conf.ImageAlias)
		if err != nil {
			return nil, err
		}
		s.Server = ""
		creatingInstances := make(map[string]instances)
		for k, v := range conf.ResourceTypesCounts {
			configuredInstancesCount.WithLabelValues(k, conf.ImageAlias).Set(float64(v))
		}
		for name := range confmap.ResourceTypesMap {
			creatingInstances[name] = make(instances)
		}
		ac[version] = &AgentConfig{
			ImageAlias:     conf.ImageAlias,
			InstanceSource: *s,

			ResourceTypesCounts: conf.ResourceTypesCounts,

			currentImage: struct {
				Hash      string
				CreatedAt time.Time
			}{Hash: "", CreatedAt: time.Time{}},

			creatingInstances: creatingInstances,
			deletingInstances: make(instances),
		}
	}
	checkInterval, waitIdleTime, zombieAllowTime, err := LoadParams()
	if err != nil {
		return nil, fmt.Errorf("load params: %w", err)
	}
	registry := prometheus.NewRegistry()
	registry.Register(configuredInstancesCount)
	registry.Register(lxdInstances)
	c, err := lxd.ConnectLXDUnixWithContext(ctx, "", &lxd.ConnectionArgs{})
	if err != nil {
		return nil, fmt.Errorf("connect lxd: %w", err)
	}

	agent := &Agent{
		Config:           ac,
		Client:           c,
		CheckInterval:    checkInterval,
		WaitIdleTime:     waitIdleTime,
		ZombieAllowTime:  zombieAllowTime,
		registry:         registry,
		ResourceTypesMap: confmap.ResourceTypesMap,
	}

	return agent, nil
}

func (a *Agent) reloadConfig() error {
	confmap, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("reload config: %w", err)
	}

	for version, conf := range confmap.Config {
		for k, v := range conf.ResourceTypesCounts {
			configuredInstancesCount.WithLabelValues(k, conf.ImageAlias).Set(float64(v))
		}
		a.Config[version].ImageAlias = conf.ImageAlias
		a.Config[version].ImageAlias = conf.ImageAlias
		a.Config[version].ResourceTypesCounts = conf.ResourceTypesCounts
	}
	a.ResourceTypesMap = confmap.ResourceTypesMap
	return nil
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
			if err := a.reloadConfig(); err != nil {
				slog.Error("Failed to reload config", "err", err.Error())
			}
		case <-ctx.Done():
			slog.Info("Stopping agent...")
			return nil
		case <-ticker.C:
			if err := a.adjustInstancePool(); err != nil {
				slog.Error("Failed to adjust instances", "err", err)
			}
		}
	}
}

func (a *Agent) countPooledInstances(instances []api.Instance, resourceTypeName, version string) int {
	count := 0
	for _, i := range instances {
		if i.StatusCode != api.Frozen {
			continue
		}
		if i.Config[configKeyResourceType] != resourceTypeName {
			continue
		}
		if i.Config[configKeyImageAlias] != a.Config[version].ImageAlias {
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

// adjustInstancePool adjusts the instance pool.
// It creates or deletes instances according to the configuration.
func (a *Agent) adjustInstancePool() error {
	s, err := a.Client.GetInstances(api.InstanceTypeAny)
	if err != nil {
		return fmt.Errorf("get instances: %w", err)
	}

	for version, config := range a.Config {
		toDelete := []string{}
		for rtName, rt := range a.ResourceTypesMap {
			current := a.countPooledInstances(s, rtName, version)
			creating := len(config.creatingInstances[rtName])
			rtCount, ok := config.ResourceTypesCounts[rtName]
			if !ok {
				toDelete = append(toDelete, rtName)
				continue
			} else if rtCount == 0 {
				toDelete = append(toDelete, rtName)
				continue
			}
			createCount := rtCount - current - creating
			if createCount < 1 {
				continue
			}
			slog.Info("Create instances", "count", createCount, "flavor", rtName)
			for i := 0; i < createCount; i++ {
				iname, err := generateInstanceName()
				if err != nil {
					return fmt.Errorf("generate instance name: %w", err)
				}
				l := slog.With("instance", iname, "flavor", rtName, "version", version)
				config.creatingInstances[rtName][iname] = struct{}{}

				defer delete(config.creatingInstances[rtName], iname)

				if err := a.createInstance(iname, rtName, rt, version, l); err != nil {
					l.Error("failed to create instance", "err", err.Error())
				}
			}
		}
		for _, i := range s {
			if i.Config[configKeyResourceType] == "" || i.Config[configKeyImageAlias] != config.ImageAlias {
				continue
			}
			l := slog.With("instance", i.Name, "version", version)
			if _, ok := config.ResourceTypesCounts[i.Config[configKeyResourceType]]; !ok {
				if i.Config[configKeyImageAlias] == config.ImageAlias {
					toDelete = append(toDelete, i.Config[configKeyResourceType])
				}
			}
			for _, rt := range toDelete {
				if i.Config[configKeyResourceType] == rt {
					l := l.With("flavor", rt)
					l.Info("Deleting disabled flavor instance")
					if err := a.deleteInstance(i, version); err != nil {
						l.Error("failed to delete instance", "err", err.Error())
						continue
					}
					l.Info("Deleted disabled flavor instance")
				}
			}
			if a.isZombieInstance(i, version) {
				l.Info("Deleting zombie instance")
				if err := a.deleteInstance(i, version); err != nil {
					l.Error("failed to delete zombie instance", "err", err.Error())
				}
				l.Info("Deleted zombie instance")
			}
			if isOld, err := a.isOldImageInstance(i, version); err != nil {
				l.Error("failed to check old image instance", "err", err.Error())
			} else if isOld {
				l.Info("Deleting old image instance")
				if err := a.deleteInstance(i, version); err != nil {
					l.Error("failed to delete old image instance", "err", err.Error())
				}
				l.Info("Deleted old image instance")
			}
		}
	}

	return nil
}

func (a *Agent) CollectMetrics(ctx context.Context) error {
	ticker := time.NewTicker(a.CheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping metrics collection...")
			return nil
		case <-ticker.C:
			slog.Debug("Collecting metrics...")
			if err := a.collectMetrics(); err != nil {
				slog.Error("Failed to collect metrics", "err", err)
				continue
			}
			if err := prometheus.WriteToTextfile(metricsPath, a.registry); err != nil {
				slog.Error("Failed to write metrics", "err", err)
			}
		}
	}
}

func (a *Agent) collectMetrics() error {
	s, err := a.Client.GetInstances(api.InstanceTypeAny)
	if err != nil {
		return fmt.Errorf("get instances: %w", err)
	}
	lxdInstances.Reset()
	for _, i := range s {
		lxdInstances.WithLabelValues(i.Status, i.Config[configKeyResourceType], i.Config[configKeyImageAlias]).Inc()
	}
	return nil
}

func (a *Agent) isZombieInstance(i api.Instance, version string) bool {
	if i.StatusCode == api.Frozen {
		return false
	}
	if _, ok := i.Config[configKeyRunnerName]; ok {
		return false
	}
	if i.Config[configKeyImageAlias] != a.Config[version].ImageAlias {
		return false
	}
	if i.CreatedAt.Add(a.ZombieAllowTime).After(time.Now()) {
		return false
	}
	if rt, ok := i.Config[configKeyResourceType]; !ok {
		return false
	} else if _, ok := a.Config[version].creatingInstances[rt][i.Name]; ok {
		return false
	}
	return true
}

func (a *Agent) isOldImageInstance(i api.Instance, version string) (bool, error) {
	baseImage, ok := i.Config["volatile.base_image"]
	if !ok {
		return false, errors.New("Failed to get volatile.base_image")
	}
	if i.Config[configKeyImageAlias] != a.Config[version].ImageAlias {
		return false, nil
	}
	if baseImage != a.Config[version].currentImage.Hash {
		if i.CreatedAt.Before(a.Config[version].currentImage.CreatedAt) {
			if i.StatusCode == api.Frozen {
				return true, nil
			}
			return false, nil
		}
		a.Config[version].currentImage.Hash = baseImage
		a.Config[version].currentImage.CreatedAt = i.CreatedAt
		return false, nil
	}
	return false, nil
}

func (a *Agent) deleteInstance(i api.Instance, version string) error {
	if _, ok := a.Config[version].deletingInstances[i.Name]; ok {
		return nil
	}
	a.Config[version].deletingInstances[i.Name] = struct{}{}
	defer delete(a.Config[version].deletingInstances, i.Name)
	_, etag, err := a.Client.GetInstance(i.Name)
	if err != nil {
		return fmt.Errorf("get instance: %w", err)
	}
	stopOp, err := a.Client.UpdateInstanceState(i.Name, api.InstanceStatePut{Action: "stop", Timeout: -1, Force: true}, etag)
	if err != nil && err.Error() != errAlreadyStopped {
		return fmt.Errorf("stop instance: %w", err)
	}
	if err := stopOp.Wait(); err != nil && err.Error() != errAlreadyStopped {
		return fmt.Errorf("stop instance operation: %w", err)
	}
	deleteOp, err := a.Client.DeleteInstance(i.Name)
	if err != nil {
		return fmt.Errorf("delete instance: %w", err)
	}
	if err := deleteOp.Wait(); err != nil {
		return fmt.Errorf("delete instance operation: %w", err)
	}
	return nil
}
