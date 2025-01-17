package resourcecache

import (
	"context"
	"fmt"
	"time"

	"github.com/lxc/lxd/shared/api"
)

var (
	// ErrCacheNotFound is error message for cache not found
	ErrCacheNotFound = fmt.Errorf("cache not found")

	// DefaultLockTTL is default TTL for lock
	DefaultLockTTL = 5 * time.Second
	// DefaultExpireDuration is default expire duration
	DefaultExpireDuration = 10 * time.Second
)

// Resource is resource of lxd host
type Resource struct {
	Instances []api.Instance `json:"instances"`

	CPUTotal    uint64 `json:"cpu_total"`
	MemoryTotal uint64 `json:"memory_total"`
	CPUUsed     uint64 `json:"cpu_used"`
	MemoryUsed  uint64 `json:"memory_used"`
}

// ResourceCache is cache interface
type ResourceCache interface {
	GetResourceCache(ctx context.Context, hostname string) (*Resource, *time.Time, error)
	SetResourceCache(ctx context.Context, hostname string, resource Resource, expired time.Duration) error
	ListResourceCache(ctx context.Context) ([]Resource, []string, []time.Time, error)

	Lock(ctx context.Context, hostname string) error
	Unlock(ctx context.Context, hostname string) error
}
