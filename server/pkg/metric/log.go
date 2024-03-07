package metric

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	FailedAllocateCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{Namespace: namespace, Subsystem: lxdName, Name: "failed_allocate_count", Help: "failed allocate instance count"},
		[]string{"lxd_host", "resource_type", "host_name"},
	)
)
