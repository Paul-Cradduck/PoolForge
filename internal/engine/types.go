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
	PoolHealthy  PoolState = "healthy"
	PoolDegraded PoolState = "degraded"
	PoolFailed   PoolState = "failed"
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
}

type DiskInfo struct {
	Device        string
	CapacityBytes uint64
	State         DiskState
	Slices        []SliceInfo
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
	Name       string
	Disks      []string
	ParityMode ParityMode
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
	Pool          Pool
	ArrayStatuses []ArrayStatus
	DiskStatuses  []DiskStatusInfo
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
}
