package cmd

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	lxd "github.com/lxc/lxd/client"
	"github.com/lxc/lxd/shared/api"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	slm "github.com/whywaita/shoes-lxd-multi/server/pkg/api"

	"github.com/whywaita/shoes-lxd-multi/pool-agent/pkg/featureflag"
)

// Runner status constants
const (
	RunnerStatusCreating  = "Creating"
	RunnerStatusListening = "Listening"
	RunnerStatusRunning   = "Running"
	RunnerStatusFinished  = "Finished"
)

// Agent is an agent for pool mode.
type Agent struct {
	Image             map[string]*Image
	CheckInterval     time.Duration
	WaitIdleTime      time.Duration
	ZombieAllowTime   time.Duration
	LxdDir            string
	registry          *prometheus.Registry
	ResourceTypesMap  ResourceTypesMap
	Client            lxd.InstanceServer
	deletingInstances instances
}

type Image struct {
	Config ConfigPerImage
	Status imageStatus

	InstanceSource api.InstanceSource
}

type imageStatus struct {
	CreatingInstances map[string]instances
	CurrentImage      struct {
		Hash      string
		CreatedAt time.Time
	}
}

var (
	errAlreadyStopped = "The instance is already stopped"
)

type instances map[string]struct{}

func newImage(conf ConfigPerImage) (*Image, error) {
	s, err := slm.ParseAlias(conf.ImageAlias)
	if err != nil {
		return nil, fmt.Errorf("failed to slm.ParseAlias(%s): %w", conf.ImageAlias, err)
	}
	// Server is image server in alias, so it should be empty.
	s.Server = ""

	creatingInstances := make(map[string]instances)
	for k := range conf.ResourceTypesCounts {
		creatingInstances[k] = make(instances)
	}
	return &Image{
		Config: conf,

		Status: imageStatus{
			CreatingInstances: creatingInstances,
			CurrentImage: struct {
				Hash      string
				CreatedAt time.Time
			}{
				Hash:      "",
				CreatedAt: time.Time{},
			},
		},

		InstanceSource: *s,
	}, nil
}

func newAgent(c lxd.InstanceServer) (*Agent, error) {
	f, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed read config file: %w", err)
	}
	conf, err := LoadConfig(f)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	// register instance per image
	ac := make(map[string]*Image, len(conf.ConfigPerImage))
	for imageName, confPerImage := range conf.ConfigPerImage {
		_imageInstance, err := newImage(confPerImage)
		if err != nil {
			return nil, fmt.Errorf("failed to generate agent config: %w", err)
		}
		ac[imageName] = _imageInstance
	}

	checkInterval, waitIdleTime, zombieAllowTime, lxdDir, err := LoadParams()
	if err != nil {
		return nil, fmt.Errorf("load params: %w", err)
	}

	registry := prometheus.NewRegistry()
	registry.Register(configuredInstancesCount)
	registry.Register(lxdInstances)

	for _, im := range ac {
		for k, v := range im.Config.ResourceTypesCounts {
			configuredInstancesCount.WithLabelValues(k, im.Config.ImageAlias).Set(float64(v))
		}
	}

	agent := &Agent{
		Image:             ac,
		Client:            c,
		CheckInterval:     checkInterval,
		WaitIdleTime:      waitIdleTime,
		ZombieAllowTime:   zombieAllowTime,
		LxdDir:            lxdDir,
		registry:          registry,
		ResourceTypesMap:  conf.ResourceTypesMap,
		deletingInstances: make(instances),
	}

	return agent, nil
}

func (a *Agent) reloadConfig() error {
	f, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed read config file: %w", err)
	}
	conf, err := LoadConfig(f)
	if err != nil {
		return fmt.Errorf("reload config: %w", err)
	}

	for imageName, confImage := range conf.ConfigPerImage {
		if _, ok := a.Image[imageName]; !ok {
			// update image config
			a.Image[imageName].Config = confImage
			continue
		} else {
			// create new image instance
			i, err := newImage(confImage)
			if err != nil {
				return fmt.Errorf("failed to create new image instance: %w", err)
			}
			a.Image[imageName] = i
		}

		for k, v := range confImage.ResourceTypesCounts {
			configuredInstancesCount.WithLabelValues(k, confImage.ImageAlias).Set(float64(v))
		}
	}
	a.ResourceTypesMap = conf.ResourceTypesMap
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
				slog.Error("Failed to reload config", slog.String("err", err.Error()))
			}
		case <-ctx.Done():
			slog.Info("Stopping agent...")
			return nil
		case <-ticker.C:
			if err := a.adjustInstancePool(); err != nil {
				slog.Error("Failed to adjust instances", slog.String("err", err.Error()))
			}
		}
	}
}

func countPooledInstances(instances []api.Instance, resourceTypeName, imageAlias string) int {
	count := 0
	for _, i := range instances {
		if isPooledInstance(i, resourceTypeName, imageAlias) {
			count++
		}
	}
	return count
}

func isPooledInstance(i api.Instance, resourceTypeName, imageAlias string) bool {
	// Check if the feature flag is enabled to exclude Running instances
	if featureflag.IsEnabled(featureflag.CountWithoutRunning) {
		if i.StatusCode != api.Frozen {
			return false
		}
	} else {
		// Default behavior: count both Frozen and Running instances
		if !(i.StatusCode == api.Frozen || i.StatusCode == api.Running) {
			return false
		}
	}

	if i.Config[configKeyResourceType] != resourceTypeName {
		return false
	}
	if i.Config[configKeyImageAlias] != imageAlias {
		return false
	}
	return true
}

func generateInstanceName() (string, error) {
	var b [4]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return "", fmt.Errorf("generate random id: %w", err)
	}
	return fmt.Sprintf("myshoes-runner-%x", b), nil
}

func (a *Agent) CalculateCreateCount(s []api.Instance, rtName string, imageKey string) (int, bool) {
	creating := len(a.Image[imageKey].Status.CreatingInstances[rtName])
	current := countPooledInstances(s, rtName, a.Image[imageKey].Config.ImageAlias)
	rtCount, ok := a.Image[imageKey].Config.ResourceTypesCounts[rtName]
	if !ok || rtCount == 0 {
		// resource type is not configured
		return 0, false
	}

	return rtCount - current - creating, true
}

func (a *Agent) CalculateToDeleteInstances(s []api.Instance, disabledResourceTypes []string, imageKey string) []api.Instance {
	toDelete := []api.Instance{}
	for _, i := range s {
		if i.Config[configKeyResourceType] == "" || i.Config[configKeyImageAlias] != a.Image[imageKey].Config.ImageAlias {
			continue
		}
		l := slog.With(slog.String("instance", i.Name), slog.String("imageKey", imageKey))
		if a.isZombieInstance(i, imageKey) {
			toDelete = append(toDelete, i)
			continue
		}

		if isOld, err := a.isOldImageInstance(i, imageKey); err != nil {
			l.Error("failed to check old image instance", slog.String("err", err.Error()))
		} else if isOld {
			toDelete = append(toDelete, i)
			continue
		}

		for _, rtName := range disabledResourceTypes {
			if i.Config[configKeyResourceType] == rtName {
				toDelete = append(toDelete, i)
				continue
			}
		}
	}
	return toDelete
}

// adjustInstancePool adjusts the instance pool.
// It creates or deletes instances according to the configuration.
func (a *Agent) adjustInstancePool() error {
	s, err := a.Client.GetInstances(api.InstanceTypeAny)
	if err != nil {
		return fmt.Errorf("get instances: %w", err)
	}

	resourceTypes := a.CollectResourceTypes(s)

	createMap := make(map[string]map[string]int)
	toDelete := []api.Instance{}
	for imageKey := range a.Image {
		disabledResourceTypes := []string{}
		createMap[imageKey] = make(map[string]int)
		for _, rtName := range resourceTypes {
			count, ok := a.CalculateCreateCount(s, rtName, imageKey)
			if !ok {
				disabledResourceTypes = append(disabledResourceTypes, rtName)
				continue
			}
			createMap[imageKey][rtName] = count
		}
		toDelete = append(toDelete, a.CalculateToDeleteInstances(s, disabledResourceTypes, imageKey)...)
	}

	a.deleteInstances(toDelete)

	a.createInstances(createMap)

	return nil
}

func (a *Agent) CollectResourceTypes(s []api.Instance) []string {
	resourceTypes := []string{}
	for k := range a.Image {
		resourceTypes = append(resourceTypes, slices.Collect(maps.Keys(a.Image[k].Config.ResourceTypesCounts))...)
	}
	for _, i := range s {
		if !slices.Contains(resourceTypes, i.Config[configKeyResourceType]) {
			resourceTypes = append(resourceTypes, i.Config[configKeyResourceType])
		}
	}
	return resourceTypes
}

func (a *Agent) CollectMetrics(ctx context.Context) error {
	logger := slog.Default().With(slog.String("method", "CollectMetrics"))

	ticker := time.NewTicker(a.CheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("Stopping metrics collection...")
			return nil
		case <-ticker.C:
			logger.Debug("Collecting metrics...")
			if err := a.collectMetrics(logger); err != nil {
				logger.Error("Failed to collect metrics", slog.String("err", err.Error()))
				continue
			}
			if err := prometheus.WriteToTextfile(metricsPath, a.registry); err != nil {
				logger.Error("Failed to write metrics", slog.String("err", err.Error()))
			}
		}
	}
}

func (a *Agent) collectMetrics(logger *slog.Logger) error {
	logger = logger.With(slog.String("method", "collectMetrics"))

	s, err := a.Client.GetInstances(api.InstanceTypeAny)
	if err != nil {
		return fmt.Errorf("get instances: %w", err)
	}
	lxdInstances.Reset()
	for _, i := range s {
		runnerStatus := a.getJobStatus(logger, i)
		lxdInstances.WithLabelValues(i.Status, i.Config[configKeyResourceType], i.Config[configKeyImageAlias], i.Name, i.Config[configKeyRunnerName], runnerStatus).Inc()
	}
	return nil
}

func (a *Agent) isZombieInstance(i api.Instance, imageKey string) bool {
	if i.StatusCode == api.Frozen {
		return false
	}
	if name, ok := i.Config[configKeyRunnerName]; ok && name != "" {
		return false
	}
	if i.Config[configKeyImageAlias] != a.Image[imageKey].Config.ImageAlias {
		return false
	}
	if i.CreatedAt.Add(a.ZombieAllowTime).After(time.Now()) {
		return false
	}
	if rt, ok := i.Config[configKeyResourceType]; !ok {
		return false
	} else if _, ok := a.Image[imageKey].Status.CreatingInstances[rt][i.Name]; ok {
		return false
	}
	return true
}

func (a *Agent) getJobStatus(logger *slog.Logger, instance api.Instance) string {
	logger = logger.With(slog.String("method", "getJobStatus"))

	if instance.StatusCode != api.Running {
		return RunnerStatusCreating
	}

	runnerName := instance.Config[configKeyRunnerName]
	if runnerName == "" {
		return RunnerStatusCreating
	}

	consoleLogPath := filepath.Join(a.LxdDir, instance.Name, "console.log")

	file, err := os.Open(consoleLogPath)
	if err != nil {
		logger.Debug("Failed to open console.log file", slog.String("instance", instance.Name), slog.String("path", consoleLogPath), slog.String("err", err.Error()))
		return RunnerStatusCreating
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		logger.Debug("Failed to read console.log contents", slog.String("instance", instance.Name), slog.String("path", consoleLogPath), slog.String("err", err.Error()))
		return RunnerStatusCreating
	}

	output := string(data)
	lines := strings.Split(strings.TrimSpace(output), "\n")

	maxLines := 100
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	if len(lines) == 0 {
		return RunnerStatusCreating
	}

	hasListening := false
	hasRunning := false
	lastLine := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lastLine = line
		if strings.Contains(line, "Listening for Jobs") {
			hasListening = true
		}
		if strings.Contains(line, "Running job:") {
			hasRunning = true
		}
	}

	if strings.Contains(lastLine, "Listening for Jobs") {
		return RunnerStatusListening
	}
	if strings.Contains(lastLine, "Running job:") {
		return RunnerStatusRunning
	}
	if hasListening || hasRunning {
		return RunnerStatusFinished
	}

	return RunnerStatusCreating
}

func (a *Agent) isOldImageInstance(i api.Instance, imageKey string) (bool, error) {
	baseImage, ok := i.Config["volatile.base_image"]
	if !ok {
		return false, errors.New("Failed to get volatile.base_image")
	}
	if i.Config[configKeyImageAlias] != a.Image[imageKey].Config.ImageAlias {
		return false, nil
	}
	if baseImage != a.Image[imageKey].Status.CurrentImage.Hash {
		if i.CreatedAt.Before(a.Image[imageKey].Status.CurrentImage.CreatedAt) {
			if i.StatusCode == api.Frozen {
				return true, nil
			}
			return false, nil
		}
		a.Image[imageKey].Status.CurrentImage.Hash = baseImage
		a.Image[imageKey].Status.CurrentImage.CreatedAt = i.CreatedAt
		return false, nil
	}
	return false, nil
}

func (a *Agent) deleteInstances(toDelete []api.Instance) {
	for _, i := range toDelete {
		func(i api.Instance) {
			l := slog.With(slog.String("instance", i.Name), slog.String("instance", i.Name))
			l.Info("Deleting instance")
			if _, ok := a.deletingInstances[i.Name]; ok {
				l.Debug("Instance is already deleting")
				return
			}
			a.deletingInstances[i.Name] = struct{}{}
			defer delete(a.deletingInstances, i.Name)
			_, etag, err := a.Client.GetInstance(i.Name)
			if err != nil {
				l.Error("failed to get instance", slog.String("err", err.Error()))
				return
			}
			stopOp, err := a.Client.UpdateInstanceState(i.Name, api.InstanceStatePut{Action: "stop", Timeout: -1, Force: true}, etag)
			if err != nil && err.Error() != errAlreadyStopped {
				l.Error("failed to stop instance", slog.String("err", err.Error()))
				return
			}
			if err := stopOp.Wait(); err != nil && err.Error() != errAlreadyStopped {
				l.Error("failed to stop instance operation", slog.String("err", err.Error()))
				return
			}
			deleteOp, err := a.Client.DeleteInstance(i.Name)
			if err != nil {
				l.Error("failed to delete instance", slog.String("err", err.Error()))
				return
			}
			if err := deleteOp.Wait(); err != nil {
				l.Error("failed to delete instance operation", slog.String("err", err.Error()))
				return
			}
			l.Info("Deleted instance")
		}(i)
	}
}
