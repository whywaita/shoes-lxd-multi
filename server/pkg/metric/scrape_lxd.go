package metric

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/docker/go-units"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/whywaita/shoes-lxd-multi/server/pkg/config"
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
func (ScraperLXD) Scrape(ctx context.Context, hostConfigs []config.HostConfig, ch chan<- prometheus.Metric) error {
	if err := scrapeLXDHosts(ctx, hostConfigs, ch); err != nil {
		return fmt.Errorf("failed to scrape LXD host: %w", err)
	}

	return nil
}

func scrapeLXDHosts(ctx context.Context, hostConfigs []config.HostConfig, ch chan<- prometheus.Metric) error {
	l := slog.With("method", "scrapeLXDHosts")
	hosts, errHosts, err := lxdclient.ConnectLXDs(ctx, hostConfigs)
	if err != nil {
		return fmt.Errorf("failed to connect LXD hosts: %w", err)
	}

	for _, eh := range errHosts {
		ch <- prometheus.MustNewConstMetric(
			lxdConnectErrHost, prometheus.GaugeValue, 1,
			eh.HostConfig.LxdHost, eh.Err.Error(),
		)
	}

	wg := sync.WaitGroup{}

	for _, host := range hosts {
		wg.Add(1)
		host := host
		go func(host lxdclient.LXDHost) {
			defer wg.Done()

			_l := l.With("host", host.HostConfig.LxdHost)

			if err := scrapeLXDHost(ctx, host, ch, _l); err != nil {
				_l.Warn("failed to scrape LXD host", "err", err.Error())
			}
		}(host)
	}
	wg.Wait()
	return nil
}

func scrapeLXDHost(ctx context.Context, host lxdclient.LXDHost, ch chan<- prometheus.Metric, logger *slog.Logger) error {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	resources, hostname, err := lxdclient.GetResourceFromLXDWithClient(cctx, host.Client, host.HostConfig.LxdHost, logger)
	if err != nil {
		return fmt.Errorf("failed to get resource from lxd: %w", err)
	}

	ch <- prometheus.MustNewConstMetric(
		lxdHostMaxCPU, prometheus.GaugeValue, float64(resources.CPUTotal), hostname)
	ch <- prometheus.MustNewConstMetric(
		lxdHostMaxMemory, prometheus.GaugeValue, float64(resources.MemoryTotal), hostname)

	for _, instance := range resources.Instances {
		memory, err := units.FromHumanSize(instance.Config["limits.memory"])
		if err != nil {
			logger.Warn("failed to convert limits.memory", "err", err.Error(), "instance", instance.Name)
			continue
		}

		ch <- prometheus.MustNewConstMetric(
			lxdInstance, prometheus.GaugeValue, 1,
			instance.Name, hostname, instance.Status, instance.Config[lxdclient.ConfigKeyResourceType], instance.Config["limits.cpu"], strconv.FormatInt(memory, 10),
		)
	}
	ch <- prometheus.MustNewConstMetric(
		lxdUsageCPU, prometheus.GaugeValue, float64(resources.CPUUsed), hostname)
	ch <- prometheus.MustNewConstMetric(
		lxdUsageMemory, prometheus.GaugeValue, float64(resources.MemoryUsed), hostname)

	s := lxdclient.LXDStatus{
		Resource:   *resources,
		HostConfig: host.HostConfig,
	}
	if err := lxdclient.SetStatusCache(host.HostConfig.LxdHost, s); err != nil {
		return fmt.Errorf("failed to set status cache: %w", err)
	}

	return nil
}
