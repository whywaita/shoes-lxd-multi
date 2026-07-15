package api

import (
	"testing"

	"github.com/lxc/lxd/shared/api"

	"github.com/whywaita/shoes-lxd-multi/server/pkg/config"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/lxdclient"
)

func TestFindInstanceHostFromCache(t *testing.T) {
	hostA := &lxdclient.LXDHost{HostConfig: config.HostConfig{LxdHost: "test-host-a"}}
	hostB := &lxdclient.LXDHost{HostConfig: config.HostConfig{LxdHost: "test-host-b"}}
	hostNoCache := &lxdclient.LXDHost{HostConfig: config.HostConfig{LxdHost: "test-host-no-cache"}}

	if err := lxdclient.SetStatusCache(hostA.HostConfig.LxdHost, lxdclient.LXDStatus{
		Resource: lxdclient.Resource{Instances: []api.Instance{{Name: "instance-on-a"}}},
	}); err != nil {
		t.Fatalf("failed to set status cache for host a: %+v", err)
	}
	if err := lxdclient.SetStatusCache(hostB.HostConfig.LxdHost, lxdclient.LXDStatus{
		Resource: lxdclient.Resource{Instances: []api.Instance{{Name: "instance-on-b-1"}, {Name: "instance-on-b-2"}}},
	}); err != nil {
		t.Fatalf("failed to set status cache for host b: %+v", err)
	}

	targets := []*lxdclient.LXDHost{hostNoCache, hostA, hostB}

	tests := []struct {
		name         string
		instanceName string
		want         *lxdclient.LXDHost
	}{
		{name: "found on host a", instanceName: "instance-on-a", want: hostA},
		{name: "found on host b (second entry)", instanceName: "instance-on-b-2", want: hostB},
		{name: "not cached anywhere", instanceName: "instance-unknown", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findInstanceHostFromCache(targets, tt.instanceName)
			if got != tt.want {
				t.Errorf("findInstanceHostFromCache(%q) = %v, want %v", tt.instanceName, got, tt.want)
			}
		})
	}
}

// TestFindInstanceHostFromCacheSkipsHostWithoutCache ensures a cache miss for a host does
// not stop the lookup from checking the remaining hosts.
func TestFindInstanceHostFromCacheSkipsHostWithoutCache(t *testing.T) {
	hostMiss := &lxdclient.LXDHost{HostConfig: config.HostConfig{LxdHost: "test-skip-miss"}}
	hostHit := &lxdclient.LXDHost{HostConfig: config.HostConfig{LxdHost: "test-skip-hit"}}

	if err := lxdclient.SetStatusCache(hostHit.HostConfig.LxdHost, lxdclient.LXDStatus{
		Resource: lxdclient.Resource{Instances: []api.Instance{{Name: "wanted"}}},
	}); err != nil {
		t.Fatalf("failed to set status cache: %+v", err)
	}

	got := findInstanceHostFromCache([]*lxdclient.LXDHost{hostMiss, hostHit}, "wanted")
	if got != hostHit {
		t.Errorf("findInstanceHostFromCache() = %v, want %v", got, hostHit)
	}
}
