package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	serverconfig "github.com/whywaita/shoes-lxd-multi/server/pkg/config"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"
)

type LXDResourceManager struct {
	hosts     *serverconfig.HostConfigMap
	resources map[string]LXDResource // key: lxdAPIAddress, value: LXDResource
	errorConn map[string]error       // key: lxdAPIAddress, value: error of connect
	mu        sync.RWMutex
	interval  time.Duration
	fetchFunc func(ctx context.Context, host serverconfig.HostConfig, logger *slog.Logger) (*LXDResource, error)
	logger    *slog.Logger
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
) *LXDResourceManager {
	l := logger.With("component", "LXDResourceManager")

	return &LXDResourceManager{
		hosts:     hosts,
		resources: make(map[string]LXDResource),
		errorConn: make(map[string]error),
		interval:  interval,
		fetchFunc: fetchFunc,
		logger:    l,
	}
}

func (m *LXDResourceManager) Start(ctx context.Context) {
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
	m.updateAll(ctx)
}

func (m *LXDResourceManager) updateAll(ctx context.Context) {
	m.logger.Info("updating all resources", "target", len(m.resources))

	m.hosts.Range(func(lxdAPIAddress string, hostConfig serverconfig.HostConfig) bool {
		res, err := m.fetchFunc(ctx, hostConfig, m.logger)
		if err != nil {
			m.logger.Warn("failed to fetch resource", "lxd_api_address", lxdAPIAddress, "err", err.Error())
			m.mu.Lock()
			m.errorConn[lxdAPIAddress] = err
			m.mu.Unlock()
			return true
		}

		m.mu.Lock()
		m.resources[lxdAPIAddress] = *res
		m.mu.Unlock()
		return true
	})
	m.logger.Info("finished updating all resources")
}

func (m *LXDResourceManager) GetResources() map[string]LXDResource {
	m.mu.RLock()
	defer m.mu.RUnlock()
	copied := make(map[string]LXDResource, len(m.resources))
	for k, v := range m.resources {
		copied[k] = v
	}
	return copied
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
