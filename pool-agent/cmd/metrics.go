package cmd

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	configuredInstancesCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "instances_count",
			Help:      "Number of configured instances",
			Subsystem: "configured",
			Namespace: "pool_agent",
		},
		[]string{"flavor", "image_alias"},
	)
	lxdInstances = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "instances",
			Help:      "LXD instances",
			Subsystem: "lxd",
			Namespace: "pool_agent",
		},
		[]string{"status", "flavor", "image_alias"},
	)
)
