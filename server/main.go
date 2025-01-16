package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"

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

	go serveMetrics(context.Background(), &c.LxdHost)

	// lxd resource cache
	var hcs []config.HostConfig
	c.LxdHost.Range(func(key string, value config.HostConfig) bool {
		hcs = append(hcs, value)
		return true
	})
	go resourcecache.RunLXDResourceCacheTicker(ctx, hcs, c.ResourceCachePeriodSec)

	server, err := api.New(&c.LxdHost, c.ResourceTypeMapping, c.OverCommitPercent, c.IsPoolMode)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	if err := server.Run(c.Port); err != nil {
		return fmt.Errorf("faied to run server: %w", err)
	}

	return nil
}

func serveMetrics(ctx context.Context, hostConfigs *config.HostConfigMap) {
	var hcs []config.HostConfig
	hostConfigs.Range(func(key string, value config.HostConfig) bool {
		hcs = append(hcs, value)
		return true
	})

	registry := prometheus.NewRegistry()
	registry.MustRegister(metric.NewCollector(ctx, hcs))
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
