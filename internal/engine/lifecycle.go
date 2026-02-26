package engine

import (
	"context"
	"fmt"
	"time"
)

// HandleDiskFailure marks a disk as failed and updates all affected arrays to degraded.
func (e *engineImpl) HandleDiskFailure(ctx context.Context, poolID string, disk string) error {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return err
	}

	now := time.Now()
	found := false
	for i := range pool.Disks {
		if pool.Disks[i].Device == disk {
			pool.Disks[i].State = DiskFailed
			pool.Disks[i].FailedAt = &now
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("disk %s not found in pool %q", disk, pool.Name)
	}

	// Mark affected arrays as degraded
	for i := range pool.RAIDArrays {
		for _, m := range pool.RAIDArrays[i].Members {
			// Check if this member belongs to the failed disk
			for _, d := range pool.Disks {
				if d.Device == disk {
					for _, sl := range d.Slices {
						if sl.PartitionDevice == m {
							pool.RAIDArrays[i].State = ArrayDegraded
						}
					}
				}
			}
		}
	}

	// Update pool state
	pool.State = PoolDegraded
	pool.UpdatedAt = now
	return e.meta.SavePool(pool)
}

// ReplaceDisk partitions a replacement disk to match the failed disk's layout
// and adds slices to degraded arrays to trigger rebuilds.
func (e *engineImpl) ReplaceDisk(ctx context.Context, poolID string, oldDisk string, newDisk string) error {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return err
	}

	// Validate old disk is failed
	var failedDisk *DiskInfo
	for i := range pool.Disks {
		if pool.Disks[i].Device == oldDisk {
			if pool.Disks[i].State != DiskFailed {
				return fmt.Errorf("disk %s is not in failed state", oldDisk)
			}
			failedDisk = &pool.Disks[i]
			break
		}
	}
	if failedDisk == nil {
		return fmt.Errorf("disk %s not found in pool %q", oldDisk, pool.Name)
	}

	// Validate new disk not in any pool
	existing, _ := e.meta.ListPools()
	for _, ps := range existing {
		p, err := e.meta.LoadPool(ps.ID)
		if err != nil {
			continue
		}
		for _, d := range p.Disks {
			if d.Device == newDisk {
				return fmt.Errorf("disk %s is already a member of pool %q", newDisk, p.Name)
			}
		}
	}

	// Get new disk capacity
	newInfo, err := e.disk.GetDiskInfo(newDisk)
	if err != nil {
		return fmt.Errorf("cannot read disk %s: %w", newDisk, err)
	}

	// Partition new disk to match failed disk's slices
	if err := e.disk.CreateGPTPartitionTable(newDisk); err != nil {
		return err
	}

	var newSlices []SliceInfo
	var offset uint64 = 1048576 // 1 MiB alignment
	for _, sl := range failedDisk.Slices {
		if offset+sl.SizeBytes > newInfo.CapacityBytes+1048576 {
			// New disk too small for this slice
			continue
		}
		part, err := e.disk.CreatePartition(newDisk, offset, sl.SizeBytes)
		if err != nil {
			return err
		}
		newSlices = append(newSlices, SliceInfo{
			TierIndex:       sl.TierIndex,
			PartitionNumber: part.Number,
			PartitionDevice: part.Device,
			SizeBytes:       sl.SizeBytes,
		})
		offset += sl.SizeBytes
	}

	// Add new slices to degraded arrays
	for _, sl := range newSlices {
		for i := range pool.RAIDArrays {
			if pool.RAIDArrays[i].TierIndex == sl.TierIndex {
				if err := e.raid.AddMember(pool.RAIDArrays[i].Device, sl.PartitionDevice); err != nil {
					return err
				}
				// Update members: remove old, add new
				var newMembers []string
				for _, m := range pool.RAIDArrays[i].Members {
					isOld := false
					for _, fsl := range failedDisk.Slices {
						if fsl.PartitionDevice == m {
							isOld = true
							break
						}
					}
					if !isOld {
						newMembers = append(newMembers, m)
					}
				}
				newMembers = append(newMembers, sl.PartitionDevice)
				pool.RAIDArrays[i].Members = newMembers
				pool.RAIDArrays[i].State = ArrayRebuilding
			}
		}
	}

	// Replace disk entry in pool
	newDiskInfo := DiskInfo{
		Device:        newDisk,
		CapacityBytes: newInfo.CapacityBytes,
		State:         DiskHealthy,
		Slices:        newSlices,
	}
	for i := range pool.Disks {
		if pool.Disks[i].Device == oldDisk {
			pool.Disks[i] = newDiskInfo
			break
		}
	}

	pool.UpdatedAt = time.Now()
	return e.meta.SavePool(pool)
}

// DeletePool tears down all pool resources and removes metadata.
func (e *engineImpl) DeletePool(ctx context.Context, poolID string) error {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return err
	}

	// Unmount
	e.fs.UnmountFilesystem(pool.MountPoint)

	// Remove LV and VG
	lvPath := fmt.Sprintf("/dev/%s/%s", pool.VolumeGroup, pool.LogicalVolume)
	e.lvm.RemoveLogicalVolume(lvPath)
	e.lvm.RemoveVolumeGroup(pool.VolumeGroup)

	// Stop arrays and remove PVs
	for _, a := range pool.RAIDArrays {
		e.lvm.RemovePhysicalVolume(a.Device)
		e.raid.StopArray(a.Device)
	}

	// Wipe all member disks
	for _, d := range pool.Disks {
		for _, sl := range d.Slices {
			e.raid.RemoveMember("", sl.PartitionDevice) // best-effort cleanup
		}
		e.disk.WipePartitionTable(d.Device)
	}

	return e.meta.DeletePool(poolID)
}

// AddDisk adds a new disk to an existing pool, partitions it for matching tiers,
// reshapes arrays, and extends the LV/filesystem.
func (e *engineImpl) AddDisk(ctx context.Context, poolID string, disk string) error {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return err
	}

	// Pre-checks: all arrays healthy
	for _, a := range pool.RAIDArrays {
		if a.State != ArrayHealthy {
			return fmt.Errorf("array %s is %s, all arrays must be healthy to add a disk", a.Device, a.State)
		}
	}

	// Check disk not in any pool
	existing, _ := e.meta.ListPools()
	for _, ps := range existing {
		p, err := e.meta.LoadPool(ps.ID)
		if err != nil {
			continue
		}
		for _, d := range p.Disks {
			if d.Device == disk {
				return fmt.Errorf("disk %s is already a member of pool %q", disk, p.Name)
			}
		}
	}

	newInfo, err := e.disk.GetDiskInfo(disk)
	if err != nil {
		return fmt.Errorf("cannot read disk %s: %w", disk, err)
	}

	// Partition new disk for matching tiers
	if err := e.disk.CreateGPTPartitionTable(disk); err != nil {
		return err
	}

	slices := ComputeDiskSlices(newInfo.CapacityBytes, pool.CapacityTiers)
	var newSlices []SliceInfo
	var offset uint64 = 1048576
	for _, sl := range slices {
		part, err := e.disk.CreatePartition(disk, offset, sl.SizeBytes)
		if err != nil {
			return err
		}
		newSlices = append(newSlices, SliceInfo{
			TierIndex:       sl.TierIndex,
			PartitionNumber: part.Number,
			PartitionDevice: part.Device,
			SizeBytes:       sl.SizeBytes,
		})
		offset += sl.SizeBytes
	}

	// Add slices to existing arrays and reshape
	for _, sl := range newSlices {
		for i := range pool.RAIDArrays {
			if pool.RAIDArrays[i].TierIndex == sl.TierIndex {
				if err := e.raid.AddMember(pool.RAIDArrays[i].Device, sl.PartitionDevice); err != nil {
					return err
				}
				pool.RAIDArrays[i].Members = append(pool.RAIDArrays[i].Members, sl.PartitionDevice)
				newCount := len(pool.RAIDArrays[i].Members)
				newLevel, _ := SelectRAIDLevel(pool.ParityMode, newCount)
				if err := e.raid.ReshapeArray(pool.RAIDArrays[i].Device, newCount, newLevel); err != nil {
					return err
				}
				pool.RAIDArrays[i].RAIDLevel = newLevel
				pool.CapacityTiers[pool.RAIDArrays[i].TierIndex].EligibleDiskCount = newCount
			}
		}
	}

	// Add disk to pool
	pool.Disks = append(pool.Disks, DiskInfo{
		Device:        disk,
		CapacityBytes: newInfo.CapacityBytes,
		State:         DiskHealthy,
		Slices:        newSlices,
	})

	// Extend LV and resize filesystem
	lvPath := fmt.Sprintf("/dev/%s/%s", pool.VolumeGroup, pool.LogicalVolume)
	e.lvm.ExtendLogicalVolume(lvPath)
	e.fs.ResizeFilesystem(lvPath)

	pool.UpdatedAt = time.Now()
	return e.meta.SavePool(pool)
}

// RemoveDisk removes a disk from the pool after evaluating downgrade safety.
func (e *engineImpl) RemoveDisk(ctx context.Context, poolID string, disk string) error {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return err
	}

	// Must have >2 disks after removal
	if len(pool.Disks) <= 2 {
		return fmt.Errorf("cannot remove disk: minimum 2 disks required, pool has %d", len(pool.Disks))
	}

	// Find the disk
	var target *DiskInfo
	for i := range pool.Disks {
		if pool.Disks[i].Device == disk {
			target = &pool.Disks[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("disk %s not found in pool %q", disk, pool.Name)
	}

	// Evaluate downgrade
	report := EvaluateDowngrade(pool, disk)
	if !report.Safe {
		return fmt.Errorf("cannot safely remove disk %s: would leave array with <2 members", disk)
	}

	// Remove slices from arrays and reshape
	for _, sl := range target.Slices {
		for i := range pool.RAIDArrays {
			if pool.RAIDArrays[i].TierIndex == sl.TierIndex {
				e.raid.RemoveMember(pool.RAIDArrays[i].Device, sl.PartitionDevice)
				// Update members
				var kept []string
				for _, m := range pool.RAIDArrays[i].Members {
					if m != sl.PartitionDevice {
						kept = append(kept, m)
					}
				}
				pool.RAIDArrays[i].Members = kept
				if len(kept) >= 2 {
					newLevel, _ := SelectRAIDLevel(pool.ParityMode, len(kept))
					e.raid.ReshapeArray(pool.RAIDArrays[i].Device, len(kept), newLevel)
					pool.RAIDArrays[i].RAIDLevel = newLevel
				}
			}
		}
	}

	// Remove disk from pool
	var remaining []DiskInfo
	for _, d := range pool.Disks {
		if d.Device != disk {
			remaining = append(remaining, d)
		}
	}
	pool.Disks = remaining

	// Wipe removed disk
	e.disk.WipePartitionTable(disk)

	pool.UpdatedAt = time.Now()
	return e.meta.SavePool(pool)
}

// GetRebuildProgress returns rebuild status for a specific array.
func (e *engineImpl) GetRebuildProgress(ctx context.Context, poolID string, arrayDevice string) (*RebuildProgress, error) {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return nil, err
	}

	for _, a := range pool.RAIDArrays {
		if a.Device == arrayDevice {
			sync, err := e.raid.GetSyncStatus(arrayDevice)
			if err != nil {
				return nil, err
			}
			state := RebuildComplete
			if !sync.InSync {
				state = RebuildInProgress
			}
			return &RebuildProgress{
				ArrayDevice:     arrayDevice,
				TierIndex:       a.TierIndex,
				State:           state,
				PercentComplete: sync.PercentComplete,
			}, nil
		}
	}
	return nil, fmt.Errorf("array %s not found in pool", arrayDevice)
}
