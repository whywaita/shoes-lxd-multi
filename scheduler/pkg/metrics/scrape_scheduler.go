package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/whywaita/shoes-lxd-multi/scheduler/pkg/scheduler"
)

const schedulerName = "scheduled"

var (
	schedulerTotalCPU = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, schedulerName, "total_cpu"),
		"Total CPU cores scheduled per host",
		[]string{"lxd_api_address"}, nil,
	)

	schedulerTotalMemory = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, schedulerName, "total_memory"),
		"Total memory (MB) scheduled per host",
		[]string{"lxd_api_address"}, nil,
	)

	schedulerRequestCount = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, schedulerName, "request_count"),
		"Number of scheduled requests per host",
		[]string{"lxd_api_address"}, nil,
	)

	schedulerOldestRequest = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, schedulerName, "oldest_seconds"),
		"Age of oldest scheduled request in seconds",
		[]string{"lxd_api_address"}, nil,
	)
)

// ScraperScheduler is scraper implement for scheduler
type ScraperScheduler struct{}

// Name return name
func (ScraperScheduler) Name() string {
	return schedulerName
}

// Help return help
func (ScraperScheduler) Help() string {
	return "Collect from scheduler"
}

// Scrape scrape metrics
func (ScraperScheduler) Scrape(ctx context.Context, rm *scheduler.LXDResourceManager, ch chan<- prometheus.Metric) error {
	if err := scrapeScheduler(ctx, rm, ch); err != nil {
		return fmt.Errorf("failed to scrape scheduler: %w", err)
	}

	return nil
}

func scrapeScheduler(ctx context.Context, rm *scheduler.LXDResourceManager, ch chan<- prometheus.Metric) error {
	l := slog.With("method", "scrapeScheduler")

	// Create a dummy scheduler to access scheduling data
	s := &scheduler.Scheduler{ResourceManager: rm}

	// Get and report aggregated stats
	stats, err := s.GetScheduledResourceStats(ctx)
	if err != nil {
		l.Error("failed to get scheduled resource stats", "error", err)
		return nil
	}

	for host, stat := range stats {
		l.Info("SCRAPE host stats", "host", host, "stat", stat)

		// Report total CPU
		ch <- prometheus.MustNewConstMetric(
			schedulerTotalCPU, prometheus.GaugeValue, float64(stat.TotalCPU),
			host,
		)

		// Report total memory
		ch <- prometheus.MustNewConstMetric(
			schedulerTotalMemory, prometheus.GaugeValue, float64(stat.TotalMemory),
			host,
		)

		// Report request count
		ch <- prometheus.MustNewConstMetric(
			schedulerRequestCount, prometheus.GaugeValue, float64(stat.Count),
			host,
		)

		// Report age of oldest request in seconds
		if !stat.OldestRequest.IsZero() {
			ageSeconds := float64(time.Since(stat.OldestRequest).Seconds())
			ch <- prometheus.MustNewConstMetric(
				schedulerOldestRequest, prometheus.GaugeValue, ageSeconds,
				host,
			)
		}
	}

	return nil
}
