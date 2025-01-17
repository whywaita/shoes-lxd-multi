package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"slices"
	"time"

	"github.com/whywaita/shoes-lxd-multi/server/pkg/resourcecache/inmemory"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/resourcecache/redis"

	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/resourcecache"

	"github.com/whywaita/shoes-lxd-multi/server/pkg/api"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/config"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/metric"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()

	c, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: true,
		Level:     c.LogLevel,
	})))

	// lxd resource cache
	var hcs []config.HostConfig
	c.LxdHost.Range(func(key string, value config.HostConfig) bool {
		hcs = append(hcs, value)
		return true
	})

	var rc resourcecache.ResourceCache
	if c.ClusterModeIsEnable {
		slog.Info("cluster mode is enabled")
		conn, err := redis.NewRedis(c.ClusterRedisHosts)
		if err != nil {
			return fmt.Errorf("failed to create redis: %w", err)
		}
		rc = conn
	} else {
		rc = inmemory.NewMemory()
	}

	go serveMetrics(context.Background(), c.LxdHost, rc)
	go RunLXDResourceCacheTicker(ctx, rc, hcs, c.ResourceCachePeriodSec)

	server, err := api.New(c.LxdHost, c.ResourceTypeMapping, c.OverCommitPercent, c.IsPoolMode)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	if err := server.Run(c.Port); err != nil {
		return fmt.Errorf("faied to run server: %w", err)
	}

	return nil
}

func serveMetrics(ctx context.Context, hostConfigs *config.HostConfigMap, rc resourcecache.ResourceCache) {
	var hcs []config.HostConfig
	hostConfigs.Range(func(key string, value config.HostConfig) bool {
		hcs = append(hcs, value)
		return true
	})

	registry := prometheus.NewRegistry()
	registry.MustRegister(metric.NewCollector(ctx, hcs, rc))
	registry.MustRegister(metric.FailedLxdAllocate)
	gatherers := prometheus.Gatherers{
		prometheus.DefaultGatherer,
		registry,
	}

	http.Handle("/metrics", promhttp.HandlerFor(
		gatherers,
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		},
	))

	if err := http.ListenAndServe(":9090", nil); err != nil {
		log.Fatal("failed to serve metrics (port 9090)", "err", err.Error())
	}
}

// RunLXDResourceCacheTicker is run ticker for set lxd resource cache
func RunLXDResourceCacheTicker(ctx context.Context, rc resourcecache.ResourceCache, hcs []config.HostConfig, periodSec int64) {
	ticker := time.NewTicker(time.Duration(periodSec) * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C
		if err := reloadLXDHostResourceCache(ctx, rc, hcs); err != nil {
			log.Fatal("failed to set lxd resource cache", "err", err.Error())
		}
	}
}

func reloadLXDHostResourceCache(ctx context.Context, rc resourcecache.ResourceCache, hcs []config.HostConfig) error {
	l := slog.With("method", "reloadLXDHostResourceCache")
	_, hostnames, _, err := rc.ListResourceCache(ctx)
	if err != nil {
		return fmt.Errorf("failed to list resource cache: %s", err)
	}

	var uncachedHostConfigs []config.HostConfig
	for _, hc := range hcs {
		if !slices.Contains(hostnames, hc.LxdHost) {
			uncachedHostConfigs = append(uncachedHostConfigs, hc)
		}
	}

	hosts, _, err := lxdclient.ConnectLXDs(uncachedHostConfigs)
	if err != nil {
		return fmt.Errorf("failed to connect LXD hosts: %s", err)
	}

	for _, host := range hosts {
		_l := l.With("host", host.HostConfig.LxdHost)
		if err := setLXDHostResourceCache(ctx, rc, &host, _l); err != nil {
			_l.Warn("failed to set lxd host resource cache", "err", err.Error())
			continue
		}
	}
	return nil
}

func setLXDHostResourceCache(ctx context.Context, rc resourcecache.ResourceCache, host *lxdclient.LXDHost, logger *slog.Logger) error {
	if err := rc.Lock(ctx, host.HostConfig.LxdHost); err != nil {
		return fmt.Errorf("failed to lock: %s", err)
	}
	defer rc.Unlock(ctx, host.HostConfig.LxdHost)

	resources, _, err := lxdclient.GetResourceFromLXDWithClient(ctx, host.Client, host.HostConfig.LxdHost, logger)
	if err != nil {
		return fmt.Errorf("failed to get resource from lxd: %s", err)
	}

	cacheExpireDuration := time.Duration((1 + rand.Float64()) * 1000) // 1-2 seconds, for avoid cache stampede
	if err := rc.SetResourceCache(ctx, host.HostConfig.LxdHost, *resources, cacheExpireDuration*time.Millisecond); err != nil {
		return fmt.Errorf("failed to set status cache: %s", err)
	}
	return nil
}
