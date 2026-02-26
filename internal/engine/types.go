package engine

import (
	"fmt"
	"time"
)

type ParityMode int

const (
	SHR1 ParityMode = iota
	SHR2
)

func (p ParityMode) String() string {
	if p == SHR2 {
		return "shr2"
	}
	return "shr1"
}

func ParseParityMode(s string) (ParityMode, error) {
	switch s {
	case "shr1":
		return SHR1, nil
	case "shr2":
		return SHR2, nil
	default:
		return 0, fmt.Errorf("unsupported parity mode %q, use shr1 or shr2", s)
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

type Pool struct {
	ID             string
	Name           string
	ParityMode     ParityMode
	State          PoolState
	Disks          []DiskInfo
	CapacityTiers  []CapacityTier
	RAIDArrays     []RAIDArray
	VolumeGroup    string
	LogicalVolume  string
	MountPoint     string
	Shares         []Share
	Users          []NASUser
	SnapshotConfig SnapshotConfig
	Snapshots      []Snapshot
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Share struct {
	Name         string   `json:"name"`
	Path         string   `json:"path"`
	Protocols    []string `json:"protocols"`
	NFSClients   string   `json:"nfs_clients"`
	SMBPublic    bool     `json:"smb_public"`
	SMBBrowsable bool     `json:"smb_browsable"`
	ReadOnly     bool     `json:"read_only"`
}

type NASUser struct {
	Name         string `json:"name"`
	UID          int    `json:"uid"`
	PoolID       string `json:"pool_id"`
	GlobalAccess bool   `json:"global_access"`
}

// Monitoring types

type DiskIOStats struct {
	Device   string  `json:"device"`
	ReadMBps float64 `json:"read_mbps"`
	WriteMBps float64 `json:"write_mbps"`
	ReadIOPS  float64 `json:"read_iops"`
	WriteIOPS float64 `json:"write_iops"`
}

type NetIOStats struct {
	Interface string  `json:"interface"`
	RxMBps    float64 `json:"rx_mbps"`
	TxMBps    float64 `json:"tx_mbps"`
	Protocol  string  `json:"protocol,omitempty"`
}

type ClientConnection struct {
	User        string `json:"user"`
	IP          string `json:"ip"`
	Share       string `json:"share"`
	Protocol    string `json:"protocol"`
	ConnectedAt int64  `json:"connected_at"`
}

type MetricsSnapshot struct {
	Timestamp int64         `json:"ts"`
	DiskIO    []DiskIOStats `json:"disk_io"`
	NetIO     []NetIOStats  `json:"net_io"`
}

// Snapshot types

type SnapshotConfig struct {
	ReservePercent int `json:"reserve_percent"`
}

type Snapshot struct {
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
	SizeBytes uint64 `json:"size_bytes"`
	MountPath string `json:"mount_path,omitempty"`
}

type SnapshotSchedule struct {
	Interval string `json:"interval"`
	MaxAge   string `json:"max_age"`
	MaxCount int    `json:"max_count"`
}

// Replication types

type PairedNode struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	PairedAt  int64  `json:"paired_at"`
	PublicKey string `json:"public_key"`
}

type SyncJob struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	RemoteNode string   `json:"remote_node"`
	LocalPool  string   `json:"local_pool"`
	RemotePool string   `json:"remote_pool"`
	Shares     []string `json:"shares,omitempty"`
	Mode       string   `json:"mode"`
	Schedule   string   `json:"schedule,omitempty"`
	Enabled    bool     `json:"enabled"`
}

type SyncRun struct {
	JobID        string `json:"job_id"`
	StartedAt    int64  `json:"started_at"`
	FinishedAt   int64  `json:"finished_at,omitempty"`
	BytesSent    uint64 `json:"bytes_sent"`
	BytesRecv    uint64 `json:"bytes_recv"`
	FilesChanged int    `json:"files_changed"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
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
}

type CreatePoolRequest struct {
	Name            string
	Disks           []string
	ParityMode      ParityMode
	SnapshotReserve int // percent, 0 = default 10
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
