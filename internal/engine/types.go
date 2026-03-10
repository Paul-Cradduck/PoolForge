package engine

import (
	"fmt"
	"time"
)

type ParityMode int

const (
	Parity1 ParityMode = iota
	Parity2
)

func (p ParityMode) String() string {
	if p == Parity2 {
		return "parity2"
	}
	return "parity1"
}

func ParseParityMode(s string) (ParityMode, error) {
	switch s {
	case "parity1":
		return Parity1, nil
	case "parity2":
		return Parity2, nil
	default:
		return 0, fmt.Errorf("unsupported parity mode %q, use parity1 or parity2", s)
	}
}

type PoolState string

const (
	PoolHealthy   PoolState = "healthy"
	PoolDegraded  PoolState = "degraded"
	PoolFailed    PoolState = "failed"
	PoolExpanding PoolState = "expanding"
)

type ArrayState string

const (
	ArrayHealthy    ArrayState = "healthy"
	ArrayDegraded   ArrayState = "degraded"
	ArrayRebuilding ArrayState = "rebuilding"
	ArrayFailed     ArrayState = "failed"
)

type DiskState string

const (
	DiskHealthy DiskState = "healthy"
	DiskFailed  DiskState = "failed"
)

// PoolOperationalStatus represents the runtime state of a pool.
type PoolOperationalStatus string

const (
	PoolRunning        PoolOperationalStatus = "running"
	PoolOffline        PoolOperationalStatus = "offline"
	PoolSafeToShutdown PoolOperationalStatus = "safe_to_power_down"
)

type Pool struct {
	ID            string
	Name          string
	ParityMode    ParityMode
	State         PoolState
	Disks         []DiskInfo
	CapacityTiers []CapacityTier
	RAIDArrays    []RAIDArray
	VolumeGroup   string
	LogicalVolume string
	MountPoint    string
	CreatedAt     time.Time
	UpdatedAt     time.Time

	// Phase 5: External enclosure support
	IsExternal          bool                  `json:"is_external"`
	RequiresManualStart bool                  `json:"requires_manual_start"`
	OperationalStatus   PoolOperationalStatus `json:"operational_status"`
	LastShutdown        *time.Time            `json:"last_shutdown"`
	LastStartup         *time.Time            `json:"last_startup"`
}

type DiskInfo struct {
	Device        string
	CapacityBytes uint64
	State         DiskState
	Slices        []SliceInfo
	FailedAt      *time.Time
}

type SliceInfo struct {
	TierIndex       int
	PartitionNumber int
	PartitionDevice string
	SizeBytes       uint64
}

type CapacityTier struct {
	Index             int
	SliceSizeBytes    uint64
	EligibleDiskCount int
	RAIDArray         string
}

type RAIDArray struct {
	Device        string
	RAIDLevel     int
	TierIndex     int
	State         ArrayState
	Members       []string
	CapacityBytes uint64
	UUID          string `json:"uuid"`
}

type CreatePoolRequest struct {
	Name       string
	Disks      []string
	ParityMode ParityMode
	External   bool // Phase 5: if true, sets IsExternal=true, RequiresManualStart=true
}

type PoolSummary struct {
	ID                 string
	Name               string
	State              PoolState
	TotalCapacityBytes uint64
	UsedCapacityBytes  uint64
	DiskCount          int
}

type PoolStatus struct {
	Pool               Pool
	ArrayStatuses      []ArrayStatus
	DiskStatuses       []DiskStatusInfo
	TotalCapacityBytes uint64
	UsedCapacityBytes  uint64
	FreeCapacityBytes  uint64
}

type ArrayStatus struct {
	Device        string
	RAIDLevel     int
	TierIndex     int
	State         ArrayState
	CapacityBytes uint64
	Members       []string
}

type DiskStatusInfo struct {
	Device             string
	State              DiskState
	ContributingArrays []string
	CapacityBytes      uint64
}

type RebuildProgress struct {
	ArrayDevice     string
	TierIndex       int
	State           RebuildState
	PercentComplete float64
	TargetDisk      string
	StartedAt       time.Time
}

type RebuildState string

const (
	RebuildInProgress RebuildState = "rebuilding"
	RebuildComplete   RebuildState = "complete"
	RebuildFailed     RebuildState = "failed"
)

type DowngradeReport struct {
	Safe          bool
	ArrayChanges  []ArrayChange
	CapacityLoss  uint64
	TiersRemoved  []int
}

type ArrayChange struct {
	Device       string
	OldLevel     int
	NewLevel     int
	OldMembers   int
	NewMembers   int
	Destroyed    bool
}

// Phase 5: Pool Start/Stop result types

type StartPoolResult struct {
	PoolName     string
	MountPoint   string
	ArrayResults []ArrayStartResult
	Warnings     []string
}

type ArrayStartResult struct {
	Device       string
	TierIndex    int
	State        ArrayState
	ReAddedParts []string
	FullRebuilds []string
}

type SuperblockMatch struct {
	PartitionDevice string
	ArrayUUID       string
	PreviousDevice  string
}
