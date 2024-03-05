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
		[]string{"flavor"},
	)
	lxdInstances = prometheus.NewDesc(
		prometheus.BuildFQName("pool_agent", "lxd", "instance"),
		"LXD instances",
		[]string{"instance_name", "status", "flavor"}, nil,
	)
)

func (a *Agent) Describe(ch chan<- *prometheus.Desc) {
	configuredInstancesCount.Describe(ch)
}

func (a *Agent) Collect(ch chan<- prometheus.Metric) {
	for k, v := range a.ResourceTypesCounts {
		configuredInstancesCount.WithLabelValues(k).Set(float64(v))
		ch <- configuredInstancesCount.WithLabelValues(k)
	}
	for _, i := range a.instancesCache {
		ch <- prometheus.MustNewConstMetric(lxdInstances, prometheus.GaugeValue, 1, i.Name, i.Status, i.Config["user.myshoes_resource_type"])
	}
}
