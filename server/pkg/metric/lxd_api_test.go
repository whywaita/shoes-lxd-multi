package metric

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestObserveMutexWait(t *testing.T) {
	// Reset the metric to avoid interference from other tests
	LXDAPIMutexWaitDuration.Reset()

	ObserveMutexWait("https://lxd1:8443", "allocateInstance", "pool-runner-01", 100*time.Millisecond)

	m := &dto.Metric{}
	err := LXDAPIMutexWaitDuration.WithLabelValues("https://lxd1:8443", "allocateInstance", "pool-runner-01").(prometheus.Histogram).Write(m)
	if err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}

	if got := m.GetHistogram().GetSampleCount(); got != 1 {
		t.Errorf("expected sample count 1, got %d", got)
	}
	if got := m.GetHistogram().GetSampleSum(); got < 0.09 || got > 0.11 {
		t.Errorf("expected sample sum ~0.1, got %f", got)
	}
}

func TestObserveMutexWait_EmptyInstance(t *testing.T) {
	LXDAPIMutexWaitDuration.Reset()

	ObserveMutexWait("https://lxd1:8443", "GetResourceFromLXD", "", 50*time.Millisecond)

	m := &dto.Metric{}
	err := LXDAPIMutexWaitDuration.WithLabelValues("https://lxd1:8443", "GetResourceFromLXD", "").(prometheus.Histogram).Write(m)
	if err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}

	if got := m.GetHistogram().GetSampleCount(); got != 1 {
		t.Errorf("expected sample count 1, got %d", got)
	}
}

func TestLXDAPIMutexSkippedTotal(t *testing.T) {
	LXDAPIMutexSkippedTotal.Reset()

	LXDAPIMutexSkippedTotal.WithLabelValues("https://lxd1:8443").Inc()
	LXDAPIMutexSkippedTotal.WithLabelValues("https://lxd1:8443").Inc()

	m := &dto.Metric{}
	err := LXDAPIMutexSkippedTotal.WithLabelValues("https://lxd1:8443").Write(m)
	if err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}

	if got := m.GetCounter().GetValue(); got != 2 {
		t.Errorf("expected counter value 2, got %f", got)
	}
}
