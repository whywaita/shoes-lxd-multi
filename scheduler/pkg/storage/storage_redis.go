package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// KeyPrefix is the prefix for Redis keys
	KeyPrefix = "shoes:resource:"
	// ResourceTTL is the default TTL for resources
	ResourceTTL = 24 * time.Hour
	// LockTTL is the TTL for locks
	LockTTL = 30 * time.Second
)

// RedisStorage is a Storage implementation for saving resources in Redis
type RedisStorage struct {
	client *redis.Client
}

// NewRedisStorage creates a new RedisStorage
func NewRedisStorage(client *redis.Client) *RedisStorage {
	return &RedisStorage{client: client}
}

// resourceKey generates a Redis key from a resource ID
func resourceKey(id string) string {
	return fmt.Sprintf("%s%s", KeyPrefix, id)
}

// idFromKey extracts the resource ID from a Redis key
func idFromKey(key string) string {
	if len(key) <= len(KeyPrefix) {
		return ""
	}
	return key[len(KeyPrefix):]
}

// lockKey generates a lock key from a resource ID
func lockKey(id string) string {
	return fmt.Sprintf("%s%s:lock", KeyPrefix, id)
}

// GetResource retrieves a resource by ID
func (s *RedisStorage) GetResource(ctx context.Context, id string) (*Resource, error) {
	key := resourceKey(id)
	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("resource not found: %s", id)
		}
		return nil, err
	}

	var resource Resource
	if err := json.Unmarshal(data, &resource); err != nil {
		return nil, err
	}

	return &resource, nil
}

// SetResource saves a resource
func (s *RedisStorage) SetResource(ctx context.Context, resource *Resource, ttl time.Duration) error {
	key := resourceKey(resource.ID)
	data, err := json.Marshal(resource)
	if err != nil {
		return err
	}

	return s.client.Set(ctx, key, data, ttl).Err()
}

// DeleteResource deletes a resource
func (s *RedisStorage) DeleteResource(ctx context.Context, id string) error {
	key := resourceKey(id)
	return s.client.Del(ctx, key).Err()
}

// ListResources retrieves all resources
func (s *RedisStorage) ListResources(ctx context.Context) (map[string][]*Resource, error) {
	pattern := resourceKey("*")
	keys, err := s.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, err
	}

	resources := make(map[string][]*Resource)
	for _, key := range keys {
		id := idFromKey(key)
		if id == "" {
			continue
		}

		data, err := s.client.Get(ctx, key).Bytes()
		if err != nil {
			continue
		}

		var resource Resource
		if err := json.Unmarshal(data, &resource); err != nil {
			continue
		}

		resources[id] = append(resources[id], &resource)
	}

	return resources, nil
}

// TryLock attempts to acquire a lock on a resource
func (s *RedisStorage) TryLock(ctx context.Context, id string) (bool, error) {
	key := lockKey(id)
	return s.client.SetNX(ctx, key, "locked", LockTTL).Result()
}

// Unlock releases the lock on a resource
func (s *RedisStorage) Unlock(ctx context.Context, id string) error {
	key := lockKey(id)
	return s.client.Del(ctx, key).Err()
}
