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

	// Phase 5: Shares
	CreateShare(ctx context.Context, poolID string, share Share) error
	DeleteShare(ctx context.Context, poolID string, name string) error
	UpdateShare(ctx context.Context, poolID string, name string, share Share) error

	// Phase 5: Users
	CreateUser(ctx context.Context, poolID string, name, password string, globalAccess bool) (*NASUser, error)
	DeleteUser(ctx context.Context, poolID string, name string) error

	// Phase 6: Snapshots
	CreateSnapshot(ctx context.Context, poolID string, name string, expiresIn string) (*Snapshot, error)
	DeleteSnapshot(ctx context.Context, poolID string, name string) error
	ListSnapshots(ctx context.Context, poolID string) ([]Snapshot, error)
	SetSnapshotSchedule(ctx context.Context, poolID string, schedule SnapshotSchedule) error
}

// MetadataStore defines the persistence interface.
type MetadataStore interface {
	SavePool(pool *Pool) error
	LoadPool(poolID string) (*Pool, error)
	ListPools() ([]PoolSummary, error)
	DeletePool(poolID string) error
}
