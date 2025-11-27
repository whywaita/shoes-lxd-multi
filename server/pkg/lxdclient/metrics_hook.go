package lxdclient

import (
	"sync/atomic"
	"time"
)

// APIMetricsObserver is a function type for observing LXD API call metrics
type APIMetricsObserver func(host, method string, duration time.Duration, err error)

var apiMetricsObserver atomic.Value

// SetAPIMetricsObserver sets the observer function for LXD API metrics
func SetAPIMetricsObserver(observer APIMetricsObserver) {
	apiMetricsObserver.Store(observer)
}

// observeAPICall records metrics for an LXD API call if an observer is set
func observeAPICall(host, method string, startTime time.Time, err error) {
	if observer := apiMetricsObserver.Load(); observer != nil {
		observer.(APIMetricsObserver)(host, method, time.Since(startTime), err)
	}
}
