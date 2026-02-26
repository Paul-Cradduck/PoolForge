package engine

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/poolforge/poolforge/internal/storage"
)

func execCommand(name string, args ...string) {
	exec.Command(name, args...).Run()
}

func fmtBytesShort(b uint64) string {
	if b >= 1e9 {
		return fmt.Sprintf("%.1fGB", float64(b)/1e9)
	}
	return fmt.Sprintf("%.0fMB", float64(b)/1e6)
}

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

	// Remove PVs and stop arrays
	for _, a := range pool.RAIDArrays {
		e.lvm.RemovePhysicalVolume(a.Device)
		e.raid.StopArray(a.Device)
	}

	// Zero superblocks on every partition BEFORE wiping partition tables
	// This prevents mdadm from auto-reassembling
	for _, d := range pool.Disks {
		for _, sl := range d.Slices {
			execCommand("mdadm", "--zero-superblock", sl.PartitionDevice)
		}
	}

	// Stop any arrays that auto-reassembled during cleanup
	for _, a := range pool.RAIDArrays {
		e.raid.StopArray(a.Device)
	}

	// Now wipe partition tables
	for _, d := range pool.Disks {
		e.disk.WipePartitionTable(d.Device)
	}

	return e.meta.DeletePool(poolID)
}

// AddDisk adds a new disk to an existing pool, recomputes tiers,
// creates new arrays for new tiers, and partitions existing disks for new tiers.
func (e *engineImpl) AddDisk(ctx context.Context, poolID string, disk string) error {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return err
	}

	for _, a := range pool.RAIDArrays {
		if a.State != ArrayHealthy {
			return fmt.Errorf("array %s is %s, all arrays must be healthy to add a disk", a.Device, a.State)
		}
	}

	existing, _ := e.meta.ListPools()
	for _, ps := range existing {
		p, _ := e.meta.LoadPool(ps.ID)
		if p == nil {
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

	// Reject if disk is smaller than the smallest pool disk
	var minPoolDisk uint64
	for _, d := range pool.Disks {
		if d.State != DiskFailed && (minPoolDisk == 0 || d.CapacityBytes < minPoolDisk) {
			minPoolDisk = d.CapacityBytes
		}
	}
	if newInfo.CapacityBytes < minPoolDisk {
		return fmt.Errorf("disk %s (%s) is smaller than the smallest pool disk (%s) — it cannot participate in any existing tier. Minimum size: %s",
			disk, fmtBytesShort(newInfo.CapacityBytes), fmtBytesShort(minPoolDisk), fmtBytesShort(minPoolDisk))
	}

	// Recompute tiers with all disks including the new one
	allCaps := make([]uint64, 0, len(pool.Disks)+1)
	for _, d := range pool.Disks {
		if d.State != DiskFailed {
			allCaps = append(allCaps, d.CapacityBytes)
		}
	}
	allCaps = append(allCaps, newInfo.CapacityBytes)
	newTiers := ComputeCapacityTiers(allCaps)

	existingTierSet := map[int]bool{}
	for _, t := range pool.CapacityTiers {
		existingTierSet[t.Index] = true
	}

	// Partition the new disk for ALL tiers it's eligible for
	if err := e.disk.CreateGPTPartitionTable(disk); err != nil {
		return err
	}
	newDiskSlices := ComputeDiskSlices(newInfo.CapacityBytes, newTiers)
	var newDiskSliceInfos []SliceInfo
	var offset uint64 = 1048576
	for _, sl := range newDiskSlices {
		part, err := e.disk.CreatePartition(disk, offset, sl.SizeBytes)
		if err != nil {
			return err
		}
		newDiskSliceInfos = append(newDiskSliceInfos, SliceInfo{
			TierIndex:       sl.TierIndex,
			PartitionNumber: part.Number,
			PartitionDevice: part.Device,
			SizeBytes:       sl.SizeBytes,
		})
		offset += sl.SizeBytes
	}

	// Add new disk slices to existing arrays and reshape
	for _, sl := range newDiskSliceInfos {
		if !existingTierSet[sl.TierIndex] {
			continue
		}
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

	// For new tiers: partition existing disks that have free space, create new arrays
	for _, nt := range newTiers {
		if existingTierSet[nt.Index] {
			continue
		}
		var members []string
		for _, sl := range newDiskSliceInfos {
			if sl.TierIndex == nt.Index {
				members = append(members, sl.PartitionDevice)
			}
		}
		// Partition existing disks that are eligible for this new tier
		for i := range pool.Disks {
			d := &pool.Disks[i]
			if d.State == DiskFailed {
				continue
			}
			slices := ComputeDiskSlices(d.CapacityBytes, newTiers)
			var sliceSize uint64
			for _, sl := range slices {
				if sl.TierIndex == nt.Index {
					sliceSize = sl.SizeBytes
				}
			}
			if sliceSize == 0 {
				continue
			}
			alreadyHas := false
			for _, sl := range d.Slices {
				if sl.TierIndex == nt.Index {
					alreadyHas = true
				}
			}
			if alreadyHas {
				continue
			}
			var diskOffset uint64 = 1048576
			for _, sl := range d.Slices {
				diskOffset += sl.SizeBytes
			}
			part, err := e.disk.CreatePartition(d.Device, diskOffset, sliceSize)
			if err != nil {
				return fmt.Errorf("partition %s for new tier %d: %w", d.Device, nt.Index, err)
			}
			newSlice := SliceInfo{
				TierIndex: nt.Index, PartitionNumber: part.Number,
				PartitionDevice: part.Device, SizeBytes: sliceSize,
			}
			d.Slices = append(d.Slices, newSlice)
			members = append(members, part.Device)
		}

		if len(members) < 2 {
			continue
		}
		raidLevel, err := SelectRAIDLevel(pool.ParityMode, len(members))
		if err != nil {
			continue
		}
		mdNum := len(pool.RAIDArrays)
		for _, m := range members {
			execCommand("wipefs", "-af", m)
		}
		info, err := e.raid.CreateArray(storage.RAIDCreateOpts{
			Name: fmt.Sprintf("md%d", mdNum), Level: raidLevel,
			Members: members, MetadataVersion: "1.2",
		})
		if err != nil {
			return fmt.Errorf("create array for tier %d: %w", nt.Index, err)
		}
		nt.RAIDArray = info.Device
		if err := e.lvm.CreatePhysicalVolume(info.Device); err != nil {
			return err
		}
		if err := e.lvm.ExtendVolumeGroup(pool.VolumeGroup, info.Device); err != nil {
			return err
		}
		pool.RAIDArrays = append(pool.RAIDArrays, RAIDArray{
			Device: info.Device, RAIDLevel: raidLevel, TierIndex: nt.Index,
			State: ArrayHealthy, Members: members,
		})
		pool.CapacityTiers = append(pool.CapacityTiers, nt)
	}

	pool.Disks = append(pool.Disks, DiskInfo{
		Device: disk, CapacityBytes: newInfo.CapacityBytes,
		State: DiskHealthy, Slices: newDiskSliceInfos,
	})

	pool.State = PoolExpanding
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
