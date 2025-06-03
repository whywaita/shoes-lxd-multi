package scheduler

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lxc/lxd/shared/api"

	serverconfig "github.com/whywaita/shoes-lxd-multi/server/pkg/config"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"
)

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

func Test_Schedule(t *testing.T) {
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
	return &LXDResource{
		Hostname: "host1",
		Resource: lxdclient.Resource{
			Instances:   nil,
			CPUTotal:    40,
			MemoryTotal: 0,
			CPUUsed:     0,
			MemoryUsed:  0,
		},
	}, nil
}

func TestScheduler_ServeHTTP(t *testing.T) {
	logger := slog.Default()
	rm := NewLXDResourceManager(newDummyHostConfigMap(), 10*time.Second, dummyFetch, logger)
	rm.updateAll(context.Background())
	s := &Scheduler{ResourceManager: rm}

	reqBody, _ := json.Marshal(ScheduleRequest{CPU: 1, Memory: 1024})
	req := httptest.NewRequest(http.MethodPost, "/schedule", bytes.NewReader(reqBody))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("unexpected status: %d", w.Code)
	}
	var resp ScheduleResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Errorf("decode error: %v", err)
	}
	if resp.Host != "host1" {
		t.Errorf("unexpected host: %s", resp.Host)
	}
}
