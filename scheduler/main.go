package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/whywaita/shoes-lxd-multi/scheduler/pkg/metrics"
	"github.com/whywaita/shoes-lxd-multi/scheduler/pkg/scheduler"
	serverconfig "github.com/whywaita/shoes-lxd-multi/server/pkg/config"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"
)

func fetchResource(ctx context.Context, host serverconfig.HostConfig, logger *slog.Logger) (*scheduler.LXDResource, error) {
	client, err := lxdclient.ConnectLXDWithTimeout(ctx, host.LxdHost, host.LxdClientCert, host.LxdClientKey)
	if err != nil {
		return nil, fmt.Errorf("failed to connect LXD: %w", err)
	}
	resources, hostname, err := lxdclient.GetResourceFromLXDWithClient(ctx, client.Client, host.LxdHost, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource from lxd: %w", err)
	}

	return &scheduler.LXDResource{
		Hostname: hostname,
		Resource: *resources,
	}, nil
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("failed to run: %s", err)
	}
}

func run() error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	})))

	hostConfig, err := serverconfig.LoadHostConfigs()
	if err != nil {
		return fmt.Errorf("failed to load host configs: %w", err)
	}

	interval := 30 * time.Second
	if intervalEnv := os.Getenv("LXD_MULTI_SCHEDULER_FETCH_INTERVAL_SECOND"); intervalEnv != "" {
		if sec, err := time.ParseDuration(intervalEnv + "s"); err == nil {
			interval = sec
		}
	}

	logger := slog.Default()
	rm := scheduler.NewLXDResourceManager(hostConfig, interval, fetchResource, logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rm.Start(ctx)

	registry := prometheus.NewRegistry()
	registry.MustRegister(metrics.NewCollector(ctx, rm))
	//registry.MustRegister(metrics.FailureCollec)
	gatherers := prometheus.Gatherers{
		prometheus.DefaultGatherer,
		registry,
	}

	s := &scheduler.Scheduler{ResourceManager: rm}
	http.Handle("/schedule", s)
	http.Handle("/metrics", promhttp.HandlerFor(
		gatherers,
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		},
	))
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	slog.Info("Starting server", "port", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	return nil
}
