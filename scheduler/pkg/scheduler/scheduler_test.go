package scheduler

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lxc/lxd/shared/api"
	"github.com/whywaita/shoes-lxd-multi/scheduler/pkg/storage"

	serverconfig "github.com/whywaita/shoes-lxd-multi/server/pkg/config"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"
)

// resetTestEnvironment ensures a clean test environment by resetting all shared resources
func resetTestEnvironment() (*mockStorage, *LXDResourceManager, *Scheduler) {
	// Create a new clean mock storage
	storage := newMockStorage()
	// Reset storage to ensure no previous test data is present
	storage.Reset()

	// Create resource manager with clean storage
	logger := slog.Default()
	rm := NewLXDResourceManager(newDummyHostConfigMap(), 10*time.Second, dummyFetch, logger, storage)

	// Initialize resources
	rm.updateAll(context.Background())

	// Create scheduler
	s := &Scheduler{ResourceManager: rm}

	return storage, rm, s
}

func newDummyHostConfigMap() *serverconfig.HostConfigMap {
	hcm := serverconfig.NewHostConfigMap()

	hcm.Store("192.0.2.1", serverconfig.HostConfig{
		Cert:          tls.Certificate{},
		LxdHost:       "192.0.2.1:8443",
		LxdClientCert: "./certs/client1.crt",
		LxdClientKey:  "./certs/client1.key",
	})
	hcm.Store("192.0.2.2", serverconfig.HostConfig{
		Cert:          tls.Certificate{},
		LxdHost:       "192.0.2.2:8443",
		LxdClientCert: "./certs/client2.crt",
		LxdClientKey:  "./certs/client2.key",
	})
	hcm.Store("192.0.2.3", serverconfig.HostConfig{
		Cert:          tls.Certificate{},
		LxdHost:       "192.0.2.3:8443",
		LxdClientCert: "./certs/client3.crt",
		LxdClientKey:  "./certs/client3.key",
	})

	return hcm
}

// mockStorage is an in-memory mock for storage.Storage
// It is not thread-safe and only for testing.
type mockStorage struct {
	data  map[string]*storage.Resource
	locks map[string]bool
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		data:  make(map[string]*storage.Resource),
		locks: make(map[string]bool),
	}
}

// Reset clears all data and locks in the mock storage
func (m *mockStorage) Reset() {
	m.data = make(map[string]*storage.Resource)
	m.locks = make(map[string]bool)
}

func (m *mockStorage) GetResource(ctx context.Context, id string) (*storage.Resource, error) {
	r, ok := m.data[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return r, nil
}
func (m *mockStorage) SetResource(ctx context.Context, resource *storage.Resource, ttl time.Duration) error {
	m.data[resource.ID] = resource
	return nil
}
func (m *mockStorage) DeleteResource(ctx context.Context, id string) error {
	delete(m.data, id)
	return nil
}
func (m *mockStorage) ListResources(ctx context.Context) (map[string][]*storage.Resource, error) {
	result := make(map[string][]*storage.Resource)
	for k, v := range m.data {
		result[k] = []*storage.Resource{v}
	}
	return result, nil
}
func (m *mockStorage) TryLock(ctx context.Context, id string) (bool, error) {
	if m.locks[id] {
		return false, nil
	}
	m.locks[id] = true
	return true, nil
}
func (m *mockStorage) Unlock(ctx context.Context, id string) error {
	m.locks[id] = false
	return nil
}

func Test_Schedule(t *testing.T) {
	// Reset test environment to ensure clean state for this test
	resetTestEnvironment()

	tests := []struct {
		name  string
		input struct {
			req       ScheduleRequest
			resources map[string]LXDResource
		}
		want    string
		wantErr bool
	}{
		{
			name: ".1 only empty",
			input: struct {
				req       ScheduleRequest
				resources map[string]LXDResource
			}{
				req: ScheduleRequest{
					CPU:    2,
					Memory: 1024,
				},
				resources: map[string]LXDResource{
					"host1": {
						Hostname: "host1",
						Resource: lxdclient.Resource{
							Instances:   nil,
							CPUTotal:    4,
							MemoryTotal: 4096,
							CPUUsed:     0,
							MemoryUsed:  0,
						},
					},
					"host2": {
						Hostname: "host2",
						Resource: lxdclient.Resource{
							Instances: []api.Instance{
								{Name: "a"},
								{Name: "b"},
							},
							CPUTotal:    4,
							MemoryTotal: 4096,
							CPUUsed:     2,
							MemoryUsed:  1024,
						},
					},
					"host3": {
						Hostname: "host3",
						Resource: lxdclient.Resource{
							Instances: []api.Instance{
								{Name: "a"},
								{Name: "b"},
							},
							CPUTotal:    4,
							MemoryTotal: 4096,
							CPUUsed:     2,
							MemoryUsed:  1024,
						},
					},
				},
			},
			want:    "host1",
			wantErr: false,
		},
		{
			name: ".2 only empty",
			input: struct {
				req       ScheduleRequest
				resources map[string]LXDResource
			}{
				req: ScheduleRequest{
					CPU:    2,
					Memory: 1024,
				},
				resources: map[string]LXDResource{
					"host1": {
						Hostname: "host1",
						Resource: lxdclient.Resource{
							Instances: []api.Instance{
								{Name: "a"},
								{Name: "b"},
							},
							CPUTotal:    4,
							MemoryTotal: 4096,
							CPUUsed:     2,
							MemoryUsed:  1024,
						},
					},
					"host2": {
						Hostname: "host2",
						Resource: lxdclient.Resource{
							Instances:   nil,
							CPUTotal:    4,
							MemoryTotal: 4096,
							CPUUsed:     0,
							MemoryUsed:  0,
						},
					},
					"host3": {
						Hostname: "host3",
						Resource: lxdclient.Resource{
							Instances: []api.Instance{
								{Name: "a"},
								{Name: "b"},
							},
							CPUTotal:    4,
							MemoryTotal: 4096,
							CPUUsed:     2,
							MemoryUsed:  1024,
						},
					},
				},
			},
			want:    "host2",
			wantErr: false,
		},
		{
			name: "all insufficient resources",
			input: struct {
				req       ScheduleRequest
				resources map[string]LXDResource
			}{
				req: ScheduleRequest{
					CPU:    8,
					Memory: 8192,
				},
				resources: map[string]LXDResource{
					"host1": {
						Hostname: "host1",
						Resource: lxdclient.Resource{
							Instances: []api.Instance{
								{Name: "a"},
								{Name: "b"},
								{Name: "c"},
								{Name: "d"},
							},
							CPUTotal:    4,
							MemoryTotal: 4096,
							CPUUsed:     4,
							MemoryUsed:  4096,
						},
					},
					"host2": {
						Hostname: "host2",
						Resource: lxdclient.Resource{
							Instances: []api.Instance{
								{Name: "a"},
								{Name: "b"},
								{Name: "c"},
								{Name: "d"},
							},
							CPUTotal:    4,
							MemoryTotal: 4096,
							CPUUsed:     4,
							MemoryUsed:  4096,
						},
					},
					"host3": {
						Hostname: "host3",
						Resource: lxdclient.Resource{
							Instances: []api.Instance{
								{Name: "a"},
								{Name: "b"},
								{Name: "c"},
								{Name: "d"},
							},
							CPUTotal:    4,
							MemoryTotal: 4096,
							CPUUsed:     4,
							MemoryUsed:  4096,
						},
					},
				},
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "only host1 can schedule",
			input: struct {
				req       ScheduleRequest
				resources map[string]LXDResource
			}{
				req: ScheduleRequest{
					CPU:    2,
					Memory: 1024,
				},
				resources: map[string]LXDResource{
					"host1": {
						Hostname: "host1",
						Resource: lxdclient.Resource{
							Instances: []api.Instance{
								{Name: "a"},
								{Name: "b"},
							},
							CPUTotal:    4,
							MemoryTotal: 4096,
							CPUUsed:     2,
							MemoryUsed:  1024,
						},
					},
					"host2": {
						Hostname: "host2",
						Resource: lxdclient.Resource{
							Instances: []api.Instance{
								{Name: "a"},
								{Name: "b"},
							},
							CPUTotal:    2,
							MemoryTotal: 2048,
							CPUUsed:     2,
							MemoryUsed:  2048,
						},
					},
					"host3": {
						Hostname: "host3",
						Resource: lxdclient.Resource{
							Instances: []api.Instance{
								{Name: "a"},
								{Name: "b"},
							},
							CPUTotal:    2,
							MemoryTotal: 2048,
							CPUUsed:     2,
							MemoryUsed:  2048,
						},
					},
				},
			},
			want:    "host1",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Schedule(tt.input.resources, tt.input.req)
			if !ok || (tt.wantErr && ok) {
				if ok {
					t.Errorf("expected error but got success with host: %s ok: %v tt.wantErr: %v", got, ok, tt.wantErr)
				}
			} else {
				if !ok {
					t.Errorf("failed to schedule: got error when expected success")
				}
				if got != tt.want {
					t.Errorf("unexpected host: got=%s, want=%s", got, tt.want)
				}
			}
		})
	}
}

func dummyFetch(ctx context.Context, host serverconfig.HostConfig, logger *slog.Logger) (*LXDResource, error) {
	// Use the LxdHost value directly as both the hostname and API address
	// This ensures we get consistent mapping between hosts and API addresses
	lxdHost := host.LxdHost
	hostname := lxdHost

	return &LXDResource{
		Hostname: hostname,
		Resource: lxdclient.Resource{
			Instances:   nil,
			CPUTotal:    40,
			CPUUsed:     0,
			MemoryTotal: 4096 * 1024 * 1024, // 4 GB in bytes
			MemoryUsed:  0,
		},
	}, nil
}

func TestScheduler_ServeHTTP(t *testing.T) {
	// Reset test environment and get a clean environment
	_, _, s := resetTestEnvironment()

	// Create a schedule request
	reqBody, _ := json.Marshal(ScheduleRequest{CPU: 1, Memory: 1024})
	req := httptest.NewRequest(http.MethodPost, "/schedule", bytes.NewReader(reqBody))
	w := httptest.NewRecorder()

	// Execute the HTTP handler
	s.ServeHTTP(w, req)

	// Check that we got a 200 OK
	if w.Code != http.StatusOK {
		t.Errorf("unexpected status: %d", w.Code)
	}

	// Check the response body
	var resp ScheduleResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Errorf("decode error: %v", err)
	}

	// Our scheduler will select the host with least used resources
	// All hosts have the same resources initially, so it will select the first one
	if resp.Host == "" {
		t.Errorf("unexpected scheduled host")
	}

	// Check if the scheduled host is stored in the mock storage
	mock := s.ResourceManager.storage.(*mockStorage)

	// Look for scheduled resources with the correct key
	scheduledKey := "scheduled:" + resp.Host
	r, ok := mock.data[scheduledKey]
	if !ok {
		t.Errorf("scheduled resources not found in mock storage with key %s", scheduledKey)
		return
	}

	// Verify it has scheduled resources data
	var schedList []ScheduledResource
	err := json.Unmarshal([]byte(r.Status), &schedList)
	if err != nil {
		t.Errorf("failed to unmarshal scheduled resources: %v", err)
		return
	}

	if len(schedList) != 1 {
		t.Errorf("expected 1 scheduled resource, got %d", len(schedList))
		return
	}

	if schedList[0].CPU != 1 || schedList[0].Memory != 1024 {
		t.Errorf("incorrect scheduled resource values: CPU=%d, Memory=%d",
			schedList[0].CPU, schedList[0].Memory)
	}
}

// TestScheduledResources tests that scheduled resources are properly tracked and considered for future scheduling
func TestScheduledResources(t *testing.T) {
	// Reset test environment and get a clean storage
	_, rm, s := resetTestEnvironment()

	ctx := context.Background()

	// Update resources
	rm.updateAll(ctx)

	// First scheduling request
	host1, ok := s.Schedule(ctx, ScheduleRequest{CPU: 2, Memory: 1024})
	if !ok {
		t.Fatal("First scheduling failed")
	}
	if host1 == "" {
		t.Errorf("Expected scheduling to succeed with host")
	}

	// Second scheduling request with the same requirements
	// Should still work but choose another host since we track resource usage
	host2, ok := s.Schedule(ctx, ScheduleRequest{CPU: 2, Memory: 1024})
	if !ok {
		t.Fatal("Second scheduling failed")
	}
	if host2 == host1 {
		t.Errorf("Expected a different host, got %s again", host1)
	}

	// Get the scheduled resources
	scheduledResources, err := s.GetScheduledResources(ctx)
	if err != nil {
		t.Fatal("Failed to get scheduled resources:", err)
	}

	// Verify scheduled resources were tracked
	if len(scheduledResources) != 2 {
		t.Errorf("Expected 2 hosts with scheduled resources, got %d", len(scheduledResources))
	}

	host1Resources, exists := scheduledResources[host1]
	if !exists {
		t.Errorf("No scheduled resources found for %s", host1)
	} else if len(host1Resources) != 1 {
		t.Errorf("Expected 1 scheduled resource for %s, got %d", host1, len(host1Resources))
	} else {
		if host1Resources[0].CPU != 2 || host1Resources[0].Memory != 1024 {
			t.Errorf("Incorrect resource values for %s: got CPU=%d, Memory=%d",
				host1, host1Resources[0].CPU, host1Resources[0].Memory)
		}
	}
}
