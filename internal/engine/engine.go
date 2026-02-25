package engine

import "context"

// EngineService defines the Phase 1 core operations interface.
type EngineService interface {
	CreatePool(ctx context.Context, req CreatePoolRequest) (*Pool, error)
	GetPool(ctx context.Context, poolID string) (*Pool, error)
	ListPools(ctx context.Context) ([]PoolSummary, error)
	GetPoolStatus(ctx context.Context, poolID string) (*PoolStatus, error)
}

// MetadataStore defines the persistence interface (implemented in internal/metadata).
type MetadataStore interface {
	SavePool(pool *Pool) error
	LoadPool(poolID string) (*Pool, error)
	ListPools() ([]PoolSummary, error)
}
