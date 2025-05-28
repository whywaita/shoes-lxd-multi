package scheduler

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/whywaita/shoes-lxd-multi/scheduler/pkg/storage"
	serverconfig "github.com/whywaita/shoes-lxd-multi/server/pkg/config"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"
)

type LXDResourceManager struct {
	hosts     *serverconfig.HostConfigMap
	errorConn map[string]error // key: lxdAPIAddress, value: error of connect
	mu        sync.RWMutex
	interval  time.Duration
	fetchFunc func(ctx context.Context, host serverconfig.HostConfig, logger *slog.Logger) (*LXDResource, error)
	storage   storage.Storage

	Logger *slog.Logger
}

type LXDResource struct {
	Hostname string
	Resource lxdclient.Resource
}

func NewLXDResourceManager(
	hosts *serverconfig.HostConfigMap,
	interval time.Duration,
	fetchFunc func(ctx context.Context, host serverconfig.HostConfig, logger *slog.Logger) (*LXDResource, error),
	logger *slog.Logger,
	storage storage.Storage,
) *LXDResourceManager {
	l := logger.With("component", "LXDResourceManager")

	return &LXDResourceManager{
		hosts:     hosts,
		errorConn: make(map[string]error),
		interval:  interval,
		fetchFunc: fetchFunc,
		Logger:    l,
		storage:   storage,
	}
}

func (m *LXDResourceManager) Start(ctx context.Context) {
	m.updateAll(ctx)
	go func() {
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.updateAll(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Start the scheduled resources cleanup routine
	go func() {
		// Run cleanup every 30 seconds
		cleanupTicker := time.NewTicker(30 * time.Second)
		defer cleanupTicker.Stop()
		for {
			select {
			case <-cleanupTicker.C:
				m.cleanupScheduledResources(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (m *LXDResourceManager) updateAll(ctx context.Context) {
	m.Logger.Info("updating all resources")

	m.hosts.Range(func(lxdAPIAddress string, hostConfig serverconfig.HostConfig) bool {
		res, err := m.fetchFunc(ctx, hostConfig, m.Logger)
		if err != nil {
			m.Logger.Warn("failed to fetch resource", "lxd_api_address", lxdAPIAddress, "err", err.Error())
			m.mu.Lock()
			m.errorConn[lxdAPIAddress] = err
			m.mu.Unlock()
			return true
		}

		// Save the resource to Redis
		resourceBytes, err := json.Marshal(res)
		if err != nil {
			m.Logger.Error("failed to marshal resource", "err", err)
			return true
		}
		err = m.storage.SetResource(ctx, &storage.Resource{
			ID:        lxdAPIAddress,
			Status:    string(resourceBytes), // Store the resource as a JSON string
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, storage.ResourceTTL)
		if err != nil {
			m.Logger.Error("failed to save resource to redis", "err", err)
		}
		return true
	})
	m.Logger.Info("finished updating all resources")
}

func (m *LXDResourceManager) GetResources(ctx context.Context) map[string]LXDResource {
	resources := make(map[string]LXDResource)
	list, err := m.storage.ListResources(ctx)
	if err != nil {
		m.Logger.Error("failed to list resources from redis", "err", err)
		return resources
	}

	for id, resourceList := range list {
		// Skip scheduled resources
		if len(id) > 10 && id[:10] == "scheduled:" {
			continue
		}

		if len(resourceList) > 0 {
			var lxdRes LXDResource
			if err := json.Unmarshal([]byte(resourceList[0].Status), &lxdRes); err == nil {
				resources[id] = lxdRes
			}
		}
	}
	return resources
}

func (m *LXDResourceManager) GetErrorConn() map[string]error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	copied := make(map[string]error, len(m.errorConn))
	for k, v := range m.errorConn {
		copied[k] = v
	}
	return copied
}

// cleanupScheduledResources removes stale scheduled resources
func (m *LXDResourceManager) cleanupScheduledResources(ctx context.Context) {
	m.Logger.Debug("cleaning up scheduled resources")

	resources, err := m.storage.ListResources(ctx)
	if err != nil {
		m.Logger.Error("failed to list resources for cleanup", "err", err)
		return
	}

	now := time.Now()
	cleanupCount := 0

	for id, resourceList := range resources {
		// Only process scheduled resources
		if len(id) <= 10 || id[:10] != "scheduled:" {
			continue
		}

		if len(resourceList) == 0 {
			continue
		}

		var schedList []ScheduledResource
		if err := json.Unmarshal([]byte(resourceList[0].Status), &schedList); err != nil {
			m.Logger.Error("failed to unmarshal scheduled resources during cleanup", "id", id, "err", err)
			continue
		}

		// Check if there are any valid scheduled resources
		validResources := []ScheduledResource{}
		for _, sr := range schedList {
			if now.Sub(sr.Time) < 2*time.Minute {
				validResources = append(validResources, sr)
			}
		}

		if len(validResources) == 0 {
			// If no valid resources, delete the entire scheduled resource entry
			if err := m.storage.DeleteResource(ctx, id); err != nil {
				m.Logger.Error("failed to delete stale scheduled resource", "id", id, "err", err)
			} else {
				cleanupCount++
			}
		} else if len(validResources) < len(schedList) {
			// If some resources are stale, update the entry with only valid ones
			data, err := json.Marshal(validResources)
			if err != nil {
				m.Logger.Error("failed to marshal valid scheduled resources", "id", id, "err", err)
				continue
			}

			err = m.storage.SetResource(ctx, &storage.Resource{
				ID:        id,
				Status:    string(data),
				CreatedAt: resourceList[0].CreatedAt,
				UpdatedAt: now,
			}, storage.ResourceTTL)

			if err != nil {
				m.Logger.Error("failed to update scheduled resource during cleanup", "id", id, "err", err)
			} else {
				cleanupCount++
			}
		}
	}

	if cleanupCount > 0 {
		m.Logger.Info("completed scheduled resources cleanup", "cleaned", cleanupCount)
	} else {
		m.Logger.Debug("no stale scheduled resources found")
	}
}
