package cmd

import (
	"github.com/prometheus/client_golang/prometheus"
)

type containerState float64

const (
	containerStateStopped containerState = iota
	containerStateRunning
	containerStateFreezing
	containerStateFrozen
)

func (s containerState) String() string {
	switch s {
	case containerStateStopped:
		return "Stopped"
	case containerStateRunning:
		return "Running"
	case containerStateFreezing:
		return "Freezing"
	case containerStateFrozen:
		return "Frozen"
	default:
		return "Unknown"
	}
}

func containerStateFromString(s string) containerState {
	switch s {
	case "Stopped":
		return containerStateStopped
	case "Running":
		return containerStateRunning
	case "Freezing":
		return containerStateFreezing
	case "Frozen":
		return containerStateFrozen
	default:
		return containerStateStopped
	}
}

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
			Help:      "LXD instances. values are enumarated as 0:Stopped, 1:Running, 2:Freezing, 3:Frozen",
			Subsystem: "lxd",
			Namespace: "pool_agent",
		},
		[]string{"flavor", "image_alias", "container_name", "runner_name"},
	)
)
