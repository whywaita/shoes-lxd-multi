package lxdclient

import "time"

// APIMetricsObserver is a function type for observing LXD API call metrics
type APIMetricsObserver func(host, method string, duration time.Duration, err error)

var apiMetricsObserver APIMetricsObserver

// SetAPIMetricsObserver sets the observer function for LXD API metrics
func SetAPIMetricsObserver(observer APIMetricsObserver) {
	apiMetricsObserver = observer
}

// observeAPICall records metrics for an LXD API call if an observer is set
func observeAPICall(host, method string, startTime time.Time, err error) {
	if apiMetricsObserver != nil {
		apiMetricsObserver(host, method, time.Since(startTime), err)
	}
}
