package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/docker/go-units"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/whywaita/shoes-lxd-multi/scheduler/pkg/scheduler"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"
)

const lxdName = "lxd"

var (
	lxdHostMaxCPU = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, lxdName, "host_max_cpu"),
		"host of cpu",
		[]string{"hostname"}, nil,
	)
	lxdHostMaxMemory = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, lxdName, "host_max_memory"),
		"host of memory",
		[]string{"hostname"}, nil,
	)
	lxdUsageCPU = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, lxdName, "host_usage_cpu"),
		"usage of cpu",
		[]string{"hostname"}, nil,
	)
	lxdUsageMemory = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, lxdName, "host_usage_memory"),
		"usage of memory",
		[]string{"hostname"}, nil,
	)
	lxdInstance = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, lxdName, "instance"),
		"LXD instances",
		[]string{"instance_name", "stadium_name", "status", "flavor", "cpu", "memory"}, nil,
	)
	lxdConnectErrHost = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, lxdName, "host_connect_error"),
		"error of connect LXD host",
		[]string{"hostname", "error_reason"}, nil,
	)
)

// ScraperLXD is scraper implement for LXD
type ScraperLXD struct{}

// Name return name
func (ScraperLXD) Name() string {
	return lxdName
}

// Help return help
func (ScraperLXD) Help() string {
	return "Collect from LXD host"
}

// Scrape scrape metrics
func (ScraperLXD) Scrape(ctx context.Context, rm *scheduler.LXDResourceManager, ch chan<- prometheus.Metric) error {
	if err := scrapeLXDHosts(ctx, rm, ch); err != nil {
		return fmt.Errorf("failed to scrape LXD host: %w", err)
	}

	return nil
}

func scrapeLXDHosts(ctx context.Context, rm *scheduler.LXDResourceManager, ch chan<- prometheus.Metric) error {
	l := slog.With("method", "scrapeLXDHosts")

	errConn := rm.GetErrorConn()
	if len(errConn) > 0 {
		for lxdAPIAddress, err := range errConn {
			ch <- prometheus.MustNewConstMetric(
				lxdConnectErrHost, prometheus.GaugeValue, 1,
				lxdAPIAddress, err.Error(),
			)
		}
		return nil
	}

	resources := rm.GetResources()
	if len(resources) == 0 {
		l.Warn("no resources found")
		return nil
	}

	for _, resource := range resources {
		l.Debug("scraping resource", "resource", resource)
		if err := scrapeLXDHost(ctx, resource, ch); err != nil {
			l.Error("failed to scrape LXD host", "err", err)
		}
	}

	return nil
}

func scrapeLXDHost(ctx context.Context, resource scheduler.LXDResource, ch chan<- prometheus.Metric) error {
	l := slog.With("method", "scrapeLXDHost")

	ch <- prometheus.MustNewConstMetric(
		lxdHostMaxCPU, prometheus.GaugeValue, float64(resource.Resource.CPUTotal), resource.Hostname)
	ch <- prometheus.MustNewConstMetric(
		lxdHostMaxMemory, prometheus.GaugeValue, float64(resource.Resource.MemoryTotal), resource.Hostname)

	for _, instance := range resource.Resource.Instances {
		memory, err := units.FromHumanSize(instance.Config["limits.memory"])
		if err != nil {
			l.Warn("failed to convert limits.memory", "err", err.Error(), "instance", instance.Name)
			continue
		}

		ch <- prometheus.MustNewConstMetric(
			lxdInstance, prometheus.GaugeValue, 1,
			instance.Name, resource.Hostname, instance.Status, instance.Config[lxdclient.ConfigKeyResourceType], instance.Config["limits.cpu"], strconv.FormatInt(memory, 10),
		)
	}
	ch <- prometheus.MustNewConstMetric(
		lxdUsageCPU, prometheus.GaugeValue, float64(resource.Resource.CPUUsed), resource.Hostname)
	ch <- prometheus.MustNewConstMetric(
		lxdUsageMemory, prometheus.GaugeValue, float64(resource.Resource.MemoryUsed), resource.Hostname)

	return nil
}
