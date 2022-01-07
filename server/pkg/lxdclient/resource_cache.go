package lxdclient

import (
	"fmt"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/whywaita/shoes-lxd-multi/server/pkg/config"
)

// LXDStatus is status for LXD
type LXDStatus struct {
	IsGood bool

	Resource   Resource
	HostConfig config.HostConfig
}

var (
	inmemoryCache = cache.New(10*time.Minute, cache.NoExpiration)

	// ErrCacheNotFound is error message for cache not found
	ErrCacheNotFound = fmt.Errorf("cache not found")
)

// GetCacheKey get a key of cache
func GetCacheKey(hostname string) string {
	return fmt.Sprintf("host-%s", hostname)
}

// GetStatusCache get a cache
func GetStatusCache(hostname string) (LXDStatus, error) {
	resp, ok := inmemoryCache.Get(GetCacheKey(hostname))
	if !ok {
		return LXDStatus{}, ErrCacheNotFound
	}

	status, ok := resp.(LXDStatus)
	if !ok {
		return LXDStatus{}, fmt.Errorf("failed to cast status")
	}
	return status, nil
}

// SetStatusCache set cache
func SetStatusCache(hostname string, status LXDStatus) error {
	inmemoryCache.Set(GetCacheKey(hostname), status, cache.DefaultExpiration)
	return nil
}
