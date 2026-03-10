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

// MutexWaitObserver is a function type for observing mutex wait duration
type MutexWaitObserver func(host, caller, instance string, duration time.Duration)

var mutexWaitObserver atomic.Value

// SetMutexWaitObserver sets the observer function for mutex wait metrics
func SetMutexWaitObserver(observer MutexWaitObserver) {
	mutexWaitObserver.Store(observer)
}

// observeMutexWait records mutex wait duration if an observer is set
func observeMutexWait(host, caller, instance string, waitDuration time.Duration) {
	if observer := mutexWaitObserver.Load(); observer != nil {
		observer.(MutexWaitObserver)(host, caller, instance, waitDuration)
	}
}
