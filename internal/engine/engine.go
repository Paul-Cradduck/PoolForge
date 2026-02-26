package engine

import "context"

// EngineService defines the Phase 1 + Phase 2 operations interface.
type EngineService interface {
	// Phase 1
	CreatePool(ctx context.Context, req CreatePoolRequest) (*Pool, error)
	GetPool(ctx context.Context, poolID string) (*Pool, error)
	ListPools(ctx context.Context) ([]PoolSummary, error)
	GetPoolStatus(ctx context.Context, poolID string) (*PoolStatus, error)

	// Phase 2: Disk Lifecycle
	AddDisk(ctx context.Context, poolID string, disk string) error
	ReplaceDisk(ctx context.Context, poolID string, oldDisk string, newDisk string) error
	RemoveDisk(ctx context.Context, poolID string, disk string) error

	// Phase 2: Pool Lifecycle
	DeletePool(ctx context.Context, poolID string) error

	// Phase 2: Self-Healing
	HandleDiskFailure(ctx context.Context, poolID string, disk string) error
	GetRebuildProgress(ctx context.Context, poolID string, arrayDevice string) (*RebuildProgress, error)

	// Import
	ImportPool() (*ImportResult, error)
}

// MetadataStore defines the persistence interface.
type MetadataStore interface {
	SavePool(pool *Pool) error
	LoadPool(poolID string) (*Pool, error)
	ListPools() ([]PoolSummary, error)
	DeletePool(poolID string) error
}
