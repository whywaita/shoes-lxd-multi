package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/whywaita/shoes-lxd-multi/server/pkg/api"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/config"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/metric"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	hostConfigs, mapping, listenPort, overCommitPercent, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	go serveMetrics(context.Background(), hostConfigs)

	server, err := api.New(hostConfigs, mapping, overCommitPercent)
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
		log.Fatal(err)
	}
}
