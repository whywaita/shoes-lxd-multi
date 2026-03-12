package storage

import (
	"context"
	"time"
)

// Resource is a struct that represents a resource in the system
type Resource struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Storage is an interface for storing and retrieving resources
type Storage interface {
	GetResource(ctx context.Context, id string) (*Resource, error)
	SetResource(ctx context.Context, resource *Resource, ttl time.Duration) error
	DeleteResource(ctx context.Context, id string) error
	ListResources(ctx context.Context) (map[string][]*Resource, error)
	TryLock(ctx context.Context, id string) (bool, error)
	Unlock(ctx context.Context, id string) error
}
