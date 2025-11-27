package metric

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"
)

var (
	// LXDAPIRequestsTotal counts the total number of LXD API requests by host, method, and status
	LXDAPIRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "lxd_api",
			Name:      "request_total",
			Help:      "Total number of LXD API requests by host, method, and status.",
		},
		[]string{"host", "method", "status"},
	)

	// LXDAPIRequestDuration measures the duration of LXD API requests in seconds
	LXDAPIRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "lxd_api",
			Name:      "request_duration_seconds",
			Help:      "Duration of LXD API requests in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"host", "method", "status"},
	)
)

func init() {
	// Set up the metrics observer for LXD API calls
	lxdclient.SetAPIMetricsObserver(observeLXDAPICall)
}

// observeLXDAPICall records metrics for an LXD API call
func observeLXDAPICall(host, method string, duration time.Duration, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}

	LXDAPIRequestsTotal.WithLabelValues(host, method, status).Inc()
	LXDAPIRequestDuration.WithLabelValues(host, method, status).Observe(duration.Seconds())
}

// LXDAPITimer is a helper to measure LXD API call duration
type LXDAPITimer struct {
	host      string
	method    string
	startTime time.Time
}

// NewLXDAPITimer creates a new timer for LXD API calls
func NewLXDAPITimer(host, method string) *LXDAPITimer {
	return &LXDAPITimer{
		host:      host,
		method:    method,
		startTime: time.Now(),
	}
}

// ObserveDuration records the duration and increments the request counter
func (t *LXDAPITimer) ObserveDuration(err error) {
	observeLXDAPICall(t.host, t.method, time.Since(t.startTime), err)
}
