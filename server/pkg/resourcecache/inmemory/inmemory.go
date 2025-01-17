package inmemory

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/whywaita/shoes-lxd-multi/server/pkg/resourcecache"

	"github.com/patrickmn/go-cache"
)

type Memory struct {
	Cache *cache.Cache
	Mutex sync.Map
}

func (m *Memory) createMutex(name string) *sync.Mutex {
	mutex, ok := m.Mutex.Load(name)
	if !ok {
		newMutex := &sync.Mutex{}
		mutex, _ = m.Mutex.LoadOrStore(name, newMutex)
	}
	return mutex.(*sync.Mutex)
}

func (m *Memory) storeMutex(name string, mutex *sync.Mutex) {
	m.Mutex.Store(name, mutex)
}

func NewMemory() *Memory {
	return &Memory{
		Cache: cache.New(10*time.Minute, cache.NoExpiration),
		Mutex: sync.Map{},
	}
}

// getCacheKey get a key of cache
func getCacheKey(hostname string) string {
	return fmt.Sprintf("host-%s", hostname)
}

func getHostnameFromCacheKey(key string) string {
	return strings.TrimPrefix(key, "host-")
}

// GetResourceCache get a cache
func (m *Memory) GetResourceCache(ctx context.Context, hostname string) (*resourcecache.Resource, *time.Time, error) {
	resp, expired, ok := m.Cache.GetWithExpiration(getCacheKey(hostname))
	if !ok {
		return nil, nil, resourcecache.ErrCacheNotFound
	}

	resource, ok := resp.(resourcecache.Resource)
	if !ok {
		return nil, nil, fmt.Errorf("failed to cast resource")
	}
	return &resource, &expired, nil
}

// SetResourceCache set cache
func (m *Memory) SetResourceCache(ctx context.Context, hostname string, status resourcecache.Resource, expired time.Duration) error {
	m.Cache.Set(getCacheKey(hostname), status, expired)
	return nil
}

// ListResourceCache list cache
func (m *Memory) ListResourceCache(ctx context.Context) ([]resourcecache.Resource, []string, []time.Time, error) {
	var resources []resourcecache.Resource
	var expireds []time.Time
	var hostnames []string

	for key := range m.Cache.Items() {
		resp, expired, ok := m.Cache.GetWithExpiration(key)
		if !ok {
			return nil, nil, nil, fmt.Errorf("failed to get cache")
		}

		resource, ok := resp.(resourcecache.Resource)
		if !ok {
			return nil, nil, nil, fmt.Errorf("failed to cast resoruce")
		}

		resources = append(resources, resource)
		expireds = append(expireds, expired)
		hostnames = append(hostnames, getHostnameFromCacheKey(key))
	}

	return resources, hostnames, expireds, nil
}

// Lock cache
func (m *Memory) Lock(ctx context.Context, hostname string) error {
	mutex := m.createMutex(hostname)
	mutex.Lock()
	m.storeMutex(hostname, mutex)
	return nil
}

// Unlock cache
func (m *Memory) Unlock(ctx context.Context, hostname string) error {
	mutex, ok := m.Mutex.Load(hostname)
	if !ok {
		return fmt.Errorf("failed to get mutex")
	}

	mutex.(*sync.Mutex).Unlock()
	return nil
}
