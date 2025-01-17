package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis/goredis/v9"
	goredislib "github.com/redis/go-redis/v9"

	"github.com/whywaita/shoes-lxd-multi/server/pkg/resourcecache"
)

type Redis struct {
	Client goredislib.UniversalClient
}

func NewRedis(addresses []string) (*Redis, error) {
	client := goredislib.NewUniversalClient(&goredislib.UniversalOptions{
		Addrs: addresses,
	})
	return &Redis{
		Client: client,
	}, nil
}

func getCacheKey(hostname string) string {
	return fmt.Sprintf("host-%s", hostname)
}

func getHostnameFromCacheKey(key string) string {
	return strings.TrimPrefix(key, "host-")
}

func getLockKey(hostname string) string {
	return fmt.Sprintf("lock-%s", hostname)
}

func (r *Redis) GetResourceCache(ctx context.Context, hostname string) (*resourcecache.Resource, *time.Time, error) {
	pool := goredis.NewPool(r.Client)
	rs := redsync.New(pool)
	mu := rs.NewMutex(getLockKey(hostname), redsync.WithExpiry(resourcecache.DefaultLockTTL), redsync.WithTries(1))
	err := mu.Lock()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lock: %w", err)
	}
	defer mu.Unlock()

	pipe := r.Client.Pipeline()
	getCmd := pipe.Get(ctx, getCacheKey(hostname))
	expireCmd := pipe.TTL(ctx, getCacheKey(hostname))

	if _, err := pipe.Exec(ctx); err != nil {
		if errors.Is(err, goredislib.Nil) {
			return nil, nil, resourcecache.ErrCacheNotFound
		}

		return nil, nil, fmt.Errorf("failed to get cache: %w", err)
	}

	val, err := getCmd.Result()
	if err != nil {
		if errors.Is(err, goredislib.Nil) {
			return nil, nil, resourcecache.ErrCacheNotFound
		}

		return nil, nil, fmt.Errorf("failed to get cache: %w", err)
	}
	var resource resourcecache.Resource
	if err := json.Unmarshal([]byte(val), &resource); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal json: %w", err)
	}

	ttl, err := expireCmd.Result()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get expire: %w", err)
	}

	expiredAt := time.Now().Add(ttl)
	return &resource, &expiredAt, nil
}

func (r *Redis) SetResourceCache(ctx context.Context, hostname string, resource resourcecache.Resource, expired time.Duration) error {
	pool := goredis.NewPool(r.Client)
	rs := redsync.New(pool)
	mu := rs.NewMutex(getLockKey(hostname), redsync.WithExpiry(resourcecache.DefaultLockTTL), redsync.WithTries(1))
	err := mu.Lock()
	if err != nil {
		return fmt.Errorf("failed to lock: %w", err)
	}
	defer mu.Unlock()

	b, err := json.Marshal(resource)
	if err != nil {
		return fmt.Errorf("failed to marshal json: %w", err)
	}

	if err := r.Client.Set(ctx, getCacheKey(hostname), string(b), expired).Err(); err != nil {
		return fmt.Errorf("failed to set cache: %w", err)
	}

	return nil
}

func (r *Redis) ListResourceCache(ctx context.Context) ([]resourcecache.Resource, []string, []time.Time, error) {
	pool := goredis.NewPool(r.Client)
	rs := redsync.New(pool)
	mu := rs.NewMutex("list-resource-cache", redsync.WithExpiry(resourcecache.DefaultLockTTL), redsync.WithTries(1))
	err := mu.Lock()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to lock: %w", err)
	}
	defer mu.Unlock()

	keys, err := r.Client.Keys(ctx, "host-*").Result()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get keys: %w", err)
	}

	keyDataList, err := getValuesAndTTLs(ctx, r.Client, keys)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get values and TTLs: %w", err)
	}

	var resources []resourcecache.Resource
	var expireds []time.Time
	var hostnames []string

	for _, keyData := range keyDataList {
		var resource resourcecache.Resource
		if err := json.Unmarshal([]byte(keyData.Value), &resource); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to unmarshal json: %w", err)
		}
		resources = append(resources, resource)
		expireds = append(expireds, time.Now().Add(keyData.TTL))
		hostnames = append(hostnames, getHostnameFromCacheKey(keyData.Key))
	}

	return resources, hostnames, expireds, nil
}

// KeyData is a struct that holds key, value, and TTL.
type KeyData struct {
	Key   string
	Value string
	TTL   time.Duration
}

// getValuesAndTTLs retrieves values and TTLs of the specified keys from Redis (using pipeline and MGET).
func getValuesAndTTLs(ctx context.Context, rdb goredislib.UniversalClient, keys []string) ([]KeyData, error) {
	var results []KeyData

	pipe := rdb.Pipeline()

	mgetCmd := pipe.MGet(ctx, keys...)

	ttlCmds := make([]*goredislib.DurationCmd, len(keys))
	for i, key := range keys {
		ttlCmds[i] = pipe.TTL(ctx, key)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		if errors.Is(err, goredislib.Nil) {
			return nil, resourcecache.ErrCacheNotFound
		}
		return nil, fmt.Errorf("failed to execute pipeline: %w", err)
	}

	values, err := mgetCmd.Result()
	if err != nil {
		if errors.Is(err, goredislib.Nil) {
			return nil, resourcecache.ErrCacheNotFound
		}
		return nil, fmt.Errorf("failed to get values: %w", err)
	}

	for i, key := range keys {
		var valueStr string
		if values[i] != nil {
			valueStr, _ = values[i].(string)
		} else {
			valueStr = ""
		}

		ttl, err := ttlCmds[i].Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get TTL: %w", err)
		}

		results = append(results, KeyData{
			Key:   key,
			Value: valueStr,
			TTL:   ttl,
		})
	}

	return results, nil
}

func (r *Redis) Lock(ctx context.Context, hostname string) error {
	pool := goredis.NewPool(r.Client)
	rs := redsync.New(pool)
	mu := rs.NewMutex(getLockKey(hostname), redsync.WithExpiry(resourcecache.DefaultLockTTL), redsync.WithTries(1))
	err := mu.LockContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to lock: %w", err)
	}

	return nil
}

func (r *Redis) Unlock(ctx context.Context, hostname string) error {
	pool := goredis.NewPool(r.Client)
	rs := redsync.New(pool)
	mu := rs.NewMutex(getLockKey(hostname), redsync.WithExpiry(resourcecache.DefaultLockTTL), redsync.WithTries(1))
	ok, err := mu.UnlockContext(ctx)
	if !ok || err != nil {
		return fmt.Errorf("failed to unlock: %w", err)
	}

	return nil
}
