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

	hostConfigs, mapping, imageAliasMap, periodSec, listenPort, overCommitPercent, logLevel, schedulerAddress, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: true,
		Level:     *logLevel,
	})))

	go serveMetrics(context.Background(), hostConfigs)

	// lxd resource cache
	var hcs []config.HostConfig
	hostConfigs.Range(func(key string, value config.HostConfig) bool {
		hcs = append(hcs, value)
		return true
	})
	go resourcecache.RunLXDResourceCacheTicker(ctx, hcs, periodSec)

	server, err := api.New(hostConfigs, mapping, imageAliasMap, overCommitPercent, schedulerAddress)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	if err := server.Run(listenPort); err != nil {
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
