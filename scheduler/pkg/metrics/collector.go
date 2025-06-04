package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/whywaita/shoes-lxd-multi/scheduler/pkg/scheduler"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	namespace = "shoes_lxd_multi_scheduler"
)

var (
	scrapeDurationDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "collector_duration_seconds"),
		"Collector time duration.",
		[]string{"collector"}, nil,
	)
)

type Collector struct {
	ctx      context.Context
	metrics  Metrics
	scrapers []Scraper

	rm *scheduler.LXDResourceManager
}

// NewCollector create a collector
func NewCollector(ctx context.Context, rm *scheduler.LXDResourceManager) *Collector {
	return &Collector{
		ctx:      ctx,
		metrics:  NewMetrics(),
		scrapers: NewScrapers(),

		rm: rm,
	}
}

// Describe describe metrics
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.metrics.TotalScrapes.Desc()
	ch <- c.metrics.Error.Desc()
	c.metrics.ScrapeErrors.Describe(ch)
}

// Collect metrics
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	c.scrape(c.ctx, ch)

	ch <- c.metrics.TotalScrapes
	ch <- c.metrics.Error
	c.metrics.ScrapeErrors.Collect(ch)
}

func (c *Collector) scrape(ctx context.Context, ch chan<- prometheus.Metric) {
	c.metrics.TotalScrapes.Inc()
	c.metrics.Error.Set(0)

	var wg sync.WaitGroup
	for _, scraper := range c.scrapers {
		wg.Add(1)
		go func(scraper Scraper) {
			defer wg.Done()
			label := fmt.Sprintf("collect.%s", scraper.Name())
			scrapeStartTime := time.Now()
			if err := scraper.Scrape(ctx, c.rm, ch); err != nil {
				slog.Warn("failed to scrape metrics", "name", scraper.Name(), "err", err.Error())
				c.metrics.ScrapeErrors.WithLabelValues(label).Inc()
				c.metrics.Error.Set(1)
			}
			ch <- prometheus.MustNewConstMetric(scrapeDurationDesc, prometheus.GaugeValue, time.Since(scrapeStartTime).Seconds(), label)
		}(scraper)
	}
	wg.Wait()
}

// Scraper is interface for scraping
type Scraper interface {
	Name() string
	Help() string
	Scrape(ctx context.Context, rm *scheduler.LXDResourceManager, ch chan<- prometheus.Metric) error
}

// NewScrapers return list of scraper
func NewScrapers() []Scraper {
	return []Scraper{
		ScraperLXD{},
		ScraperScheduler{},
	}
}

// Metrics is data in scraper
type Metrics struct {
	TotalScrapes prometheus.Counter
	ScrapeErrors *prometheus.CounterVec
	Error        prometheus.Gauge
}

// NewMetrics create a metrics
func NewMetrics() Metrics {
	return Metrics{
		TotalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "",
			Name:      "scrapes_total",
			Help:      "Total number of times shoes-lxd-multi scheduler was scraped for metrics.",
		}),
		ScrapeErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "",
			Name:      "scrape_errors_total",
			Help:      "Total number of times an error occurred scraping a shoes-lxd-multi scheduler.",
		}, []string{"collector"}),
		Error: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "",
			Name:      "last_scrape_error",
			Help:      "Whether the last scrape of metrics from shoes-lxd-multi scheduler resulted in an error (1 for error, 0 for success).",
		}),
	}
}
