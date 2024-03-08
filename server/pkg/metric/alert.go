package metric

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	FailedLxdAllocate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "",
			Name:      "failed_lxd_allocate",
		},
		[]string{"stadium_name", "runner_name"},
	)
)
