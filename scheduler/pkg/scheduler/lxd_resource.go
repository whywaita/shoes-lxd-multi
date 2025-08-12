package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/whywaita/shoes-lxd-multi/scheduler/pkg/storage"
	serverconfig "github.com/whywaita/shoes-lxd-multi/server/pkg/config"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"
)

const (
	// ScheduledResourceCleanupThreshold is the duration for cleanup threshold of scheduled resources
	ScheduledResourceCleanupThreshold = 2 * time.Minute
)

// LXDResourceManager manages LXD resources and their updates
type LXDResourceManager struct {
	hosts     *serverconfig.HostConfigMap
	errorConn map[string]error // key: lxdAPIAddress, value: error of connect
	mu        sync.RWMutex
	interval  time.Duration
	fetchFunc func(ctx context.Context, host serverconfig.HostConfig, logger *slog.Logger) (*LXDResource, error)
	storage   storage.Storage

	Logger *slog.Logger
}

// LXDResource represents a single LXD host resource
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

	// Use wait group to parallelize resource fetching
	var wg sync.WaitGroup
	var mu sync.Mutex // To protect errorConn map

	m.hosts.Range(func(lxdAPIAddress string, hostConfig serverconfig.HostConfig) bool {
		wg.Add(1)
		go func(addr string, config serverconfig.HostConfig) {
			defer wg.Done()
			
			res, err := m.fetchFunc(ctx, config, m.Logger)
			if err != nil {
				m.Logger.Warn("failed to fetch resource", "lxd_api_address", addr, "err", err.Error())
				mu.Lock()
				m.errorConn[addr] = err
				mu.Unlock()
				return
			}

			// Save the resource to Redis
			resourceBytes, err := json.Marshal(res)
			if err != nil {
				m.Logger.Error("failed to marshal resource", "err", err)
				return
			}
			err = m.storage.SetResource(ctx, &storage.Resource{
				ID:        addr,
				Status:    string(resourceBytes), // Store the resource as a JSON string
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}, storage.ResourceTTL)
			if err != nil {
				m.Logger.Error("failed to save resource to redis", "err", err)
			}
		}(lxdAPIAddress, hostConfig)
		return true
	})

	wg.Wait()
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
		if m.isScheduledResourceID(id) {
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

// isScheduledResourceID checks if the given ID is for a scheduled resource
func (m *LXDResourceManager) isScheduledResourceID(id string) bool {
	return len(id) > 10 && id[:10] == "scheduled:"
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
		if !m.isScheduledResourceID(id) {
			continue
		}

		if len(resourceList) == 0 {
			continue
		}

		updated, cleaned, err := m.processScheduledResourceEntry(ctx, id, *resourceList[0], now)
		if err != nil {
			m.Logger.Error("failed to process scheduled resource during cleanup", "id", id, "err", err)
			continue
		}

		if updated || cleaned {
			cleanupCount++
		}
	}

	if cleanupCount > 0 {
		m.Logger.Info("completed scheduled resources cleanup", "cleaned", cleanupCount)
	} else {
		m.Logger.Debug("no stale scheduled resources found")
	}
}

// processScheduledResourceEntry processes a single scheduled resource entry
func (m *LXDResourceManager) processScheduledResourceEntry(ctx context.Context, id string, resource storage.Resource, now time.Time) (updated bool, cleaned bool, err error) {
	var schedList []ScheduledResource
	if err := json.Unmarshal([]byte(resource.Status), &schedList); err != nil {
		return false, false, fmt.Errorf("failed to unmarshal scheduled resources: %w", err)
	}

	// Filter out stale resources
	validResources := m.filterValidScheduledResources(schedList, now)

	if len(validResources) == 0 {
		// If no valid resources, delete the entire scheduled resource entry
		if err := m.storage.DeleteResource(ctx, id); err != nil {
			return false, false, fmt.Errorf("failed to delete stale scheduled resource: %w", err)
		}
		return false, true, nil
	} else if len(validResources) < len(schedList) {
		// If some resources are stale, update the entry with only valid ones
		data, err := json.Marshal(validResources)
		if err != nil {
			return false, false, fmt.Errorf("failed to marshal valid scheduled resources: %w", err)
		}

		err = m.storage.SetResource(ctx, &storage.Resource{
			ID:        id,
			Status:    string(data),
			CreatedAt: resource.CreatedAt,
			UpdatedAt: now,
		}, ScheduledResourceTTL)

		if err != nil {
			return false, false, fmt.Errorf("failed to update scheduled resource: %w", err)
		}
		return true, false, nil
	}

	return false, false, nil
}

// filterValidScheduledResources filters out stale scheduled resources
func (m *LXDResourceManager) filterValidScheduledResources(schedList []ScheduledResource, now time.Time) []ScheduledResource {
	validResources := make([]ScheduledResource, 0, len(schedList))
	for _, sr := range schedList {
		if now.Sub(sr.Time) < ScheduledResourceCleanupThreshold {
			validResources = append(validResources, sr)
		}
	}
	return validResources
}
