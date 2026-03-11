package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"
)

func (e *engineImpl) StartPool(ctx context.Context, poolName string, force bool) (*StartPoolResult, error) {
	pool, err := e.resolvePoolByName(poolName)
	if err != nil {
		return nil, err
	}

	if pool.OperationalStatus == PoolRunning {
		return nil, fmt.Errorf("pool '%s' is already running", poolName)
	}

	result := &StartPoolResult{PoolName: poolName, MountPoint: pool.MountPoint}

	// Drive verification: scan block devices and match capacities
	detected := e.countDetectedDrives(pool)
	expected := len(pool.Disks)
	if detected < expected {
		msg := fmt.Sprintf("Expected %d drives, detected %d. Use --force to proceed.", expected, detected)
		if !force {
			result.Warnings = append(result.Warnings, msg)
			return result, nil
		}
		result.Warnings = append(result.Warnings, msg)
	}

	// Assemble arrays in ascending tier order
	arrays := make([]RAIDArray, len(pool.RAIDArrays))
	copy(arrays, pool.RAIDArrays)
	sort.Slice(arrays, func(i, j int) bool { return arrays[i].TierIndex < arrays[j].TierIndex })

	for _, arr := range arrays {
		ar := ArrayStartResult{Device: arr.Device, TierIndex: arr.TierIndex}

		// Assemble by UUID if available, otherwise by device name + members
		if arr.UUID != "" {
			if _, err := e.raid.AssembleArrayBySuperblock(arr.UUID); err != nil {
				// If array is already active, that's fine
				if _, derr := e.raid.GetArrayDetail(arr.Device); derr != nil {
					return nil, fmt.Errorf("failed to assemble %s (UUID %s): %w", arr.Device, arr.UUID, err)
				}
			}
		} else {
			// No UUID — try scan-based assembly first, then member-based
			out, scanErr := exec.Command("mdadm", "--assemble", "--scan", arr.Device).CombinedOutput()
			if scanErr != nil {
				members := make([]string, len(arr.Members))
				copy(members, arr.Members)
				if err := e.raid.AssembleArray(arr.Device, members); err != nil {
					if _, derr := e.raid.GetArrayDetail(arr.Device); derr != nil {
						return nil, fmt.Errorf("failed to assemble %s by members: %w (scan: %s)", arr.Device, err, string(out))
					}
				}
			}
		}

		// Check if degraded and attempt repair
		detail, err := e.raid.GetArrayDetail(arr.Device)
		if err == nil && strings.Contains(detail.State, "degraded") {
			ar.State = ArrayDegraded

			// Remove any failed members first so re-add can work
			exec.Command("mdadm", "--manage", arr.Device, "--remove", "failed").Run()

			// Scan for matching partitions to re-add
			matches, _ := e.raid.ScanSuperblocks(arr.UUID)
			activeMemberSet := make(map[string]bool)
			for _, m := range detail.Members {
				activeMemberSet[m.Device] = true
			}
			for _, match := range matches {
				if activeMemberSet[match.PartitionDevice] {
					continue
				}
				// Try re-add first (fast bitmap recovery)
				if err := e.raid.ReAddMember(arr.Device, match.PartitionDevice); err != nil {
					// Fallback to full rebuild
					if err2 := e.raid.AddMember(arr.Device, match.PartitionDevice); err2 == nil {
						ar.FullRebuilds = append(ar.FullRebuilds, match.PartitionDevice)
					}
				} else {
					ar.ReAddedParts = append(ar.ReAddedParts, match.PartitionDevice)
				}
			}
			// Re-check state after repair attempts
			if detail2, err := e.raid.GetArrayDetail(arr.Device); err == nil {
				if !strings.Contains(detail2.State, "degraded") {
					ar.State = ArrayHealthy
				} else if len(ar.ReAddedParts) > 0 || len(ar.FullRebuilds) > 0 {
					ar.State = ArrayRebuilding
				}
			}
		} else {
			ar.State = ArrayHealthy
		}

		result.ArrayResults = append(result.ArrayResults, ar)
	}

	// Activate LVM
	lvPath := fmt.Sprintf("/dev/%s/%s", pool.VolumeGroup, pool.LogicalVolume)
	if err := e.lvm.ActivateVolumeGroup(pool.VolumeGroup); err != nil {
		return nil, fmt.Errorf("vgchange -ay failed: %w", err)
	}

	// Mount filesystem
	if err := e.fs.MountFilesystem(lvPath, pool.MountPoint); err != nil {
		return nil, fmt.Errorf("mount failed: %w", err)
	}

	// Reconcile ALL device names from mdadm --detail
	e.reconcileDeviceNames(pool)

	// Populate UUIDs if missing (first start after upgrade)
	for i, arr := range pool.RAIDArrays {
		if arr.UUID == "" {
			if uuid, err := e.raid.GetArrayUUID(arr.Device); err == nil && uuid != "" {
				pool.RAIDArrays[i].UUID = uuid
			}
		}
	}

	// Update metadata
	now := time.Now()
	pool.OperationalStatus = PoolRunning
	pool.LastStartup = &now
	pool.UpdatedAt = now
	e.meta.SavePool(pool)

	return result, nil
}

func (e *engineImpl) StopPool(ctx context.Context, poolName string) error {
	pool, err := e.resolvePoolByName(poolName)
	if err != nil {
		return err
	}

	if pool.OperationalStatus != PoolRunning {
		return fmt.Errorf("pool '%s' is not running", poolName)
	}

	// Sync pending writes
	syscall.Sync()

	// Unmount filesystem
	if err := e.fs.UnmountFilesystem(pool.MountPoint); err != nil {
		return fmt.Errorf("unmount failed: %w", err)
	}

	// Deactivate LVM
	lvPath := fmt.Sprintf("/dev/%s/%s", pool.VolumeGroup, pool.LogicalVolume)
	e.lvm.DeactivateLogicalVolume(lvPath)
	e.lvm.DeactivateVolumeGroup(pool.VolumeGroup)

	// Stop arrays in descending tier order
	arrays := make([]RAIDArray, len(pool.RAIDArrays))
	copy(arrays, pool.RAIDArrays)
	sort.Slice(arrays, func(i, j int) bool { return arrays[i].TierIndex > arrays[j].TierIndex })

	for _, arr := range arrays {
		syscall.Sync()
		if err := e.raid.StopArray(arr.Device); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to stop %s: %v\n", arr.Device, err)
		}
		delay := e.stopDelay
		if delay == 0 {
			delay = 1 * time.Second
		}
		time.Sleep(delay)
	}

	// Verify no arrays remain in /proc/mdstat
	e.verifyArraysStopped(pool)

	// Verify AUTO -all in mdadm.conf
	e.verifyAutoDirective()

	// Update metadata
	now := time.Now()
	pool.OperationalStatus = PoolSafeToShutdown
	pool.LastShutdown = &now
	pool.UpdatedAt = now
	e.meta.SavePool(pool)

	return nil
}

func (e *engineImpl) SetAutoStart(ctx context.Context, poolName string, autoStart bool) error {
	pool, err := e.resolvePoolByName(poolName)
	if err != nil {
		return err
	}

	pool.RequiresManualStart = !autoStart
	pool.UpdatedAt = time.Now()
	if err := e.meta.SavePool(pool); err != nil {
		return err
	}

	// Regenerate boot config — import here to avoid circular dependency
	// The caller (CLI/API) should trigger boot config regeneration
	return nil
}

// resolvePoolByName finds a pool by name and returns the full Pool struct.
func (e *engineImpl) resolvePoolByName(name string) (*Pool, error) {
	summaries, err := e.meta.ListPools()
	if err != nil {
		return nil, err
	}
	for _, s := range summaries {
		if s.Name == name {
			return e.meta.LoadPool(s.ID)
		}
	}
	return nil, fmt.Errorf("pool '%s' not found", name)
}

// countDetectedDrives counts how many of the pool's expected drives are currently visible.
func (e *engineImpl) countDetectedDrives(pool *Pool) int {
	// Match pool disks by capacity against all block devices,
	// not by device path, since paths change after power cycles.
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return 0
	}
	type avail struct {
		cap  int64
		used bool
	}
	var avails []avail
	for _, ent := range entries {
		name := ent.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "dm-") || strings.HasPrefix(name, "md") || strings.HasPrefix(name, "ram") {
			continue
		}
		sizeBytes, err := os.ReadFile("/sys/block/" + name + "/size")
		if err != nil {
			continue
		}
		sectors := strings.TrimSpace(string(sizeBytes))
		var s int64
		fmt.Sscanf(sectors, "%d", &s)
		avails = append(avails, avail{cap: s * 512})
	}
	const tolerance int64 = 50 * 1024 * 1024 // 50MB
	count := 0
	for _, d := range pool.Disks {
		for i := range avails {
			if !avails[i].used && abs64(avails[i].cap-int64(d.CapacityBytes)) <= tolerance {
				avails[i].used = true
				count++
				break
			}
		}
	}
	return count
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// reconcileDeviceNames updates all disk/partition device paths from mdadm --detail.
func (e *engineImpl) reconcileDeviceNames(pool *Pool) {
	for i, arr := range pool.RAIDArrays {
		detail, err := e.raid.GetArrayDetail(arr.Device)
		if err != nil {
			continue
		}
		// Update array UUID if not set
		if arr.UUID == "" {
			if uuid, err := e.raid.GetArrayUUID(arr.Device); err == nil {
				pool.RAIDArrays[i].UUID = uuid
			}
		}
		// Build map: old member → new member from detail
		newMembers := make([]string, 0, len(detail.Members))
		for _, m := range detail.Members {
			newMembers = append(newMembers, m.Device)
		}
		pool.RAIDArrays[i].Members = newMembers

		// Update disk slice partition devices
		// Build set of current pool disk devices
		poolDiskDevs := make(map[string]bool)
		for _, d := range pool.Disks {
			poolDiskDevs[d.Device] = true
		}
		for _, m := range detail.Members {
			mDisk := diskFromPartition(m.Device)
			if poolDiskDevs[mDisk] {
				continue // already known
			}
			// This member's disk is not in pool metadata — find which pool disk it replaced
			for di := range pool.Disks {
				if poolDiskDevs[pool.Disks[di].Device] {
					// Check if this disk's device still exists
					if _, err := e.disk.GetDiskInfo(pool.Disks[di].Device); err == nil {
						continue // disk still exists at old path
					}
				}
				// Check capacity match
				info, err := e.disk.GetDiskInfo(mDisk)
				if err != nil {
					continue
				}
				diff := int64(info.CapacityBytes) - int64(pool.Disks[di].CapacityBytes)
				if diff < 0 {
					diff = -diff
				}
				if diff > 50*1024*1024 {
					continue
				}
				// Found the renamed disk — update device and all slice partitions
				oldDev := pool.Disks[di].Device
				pool.Disks[di].Device = mDisk
				for si := range pool.Disks[di].Slices {
					oldPart := pool.Disks[di].Slices[si].PartitionDevice
					pNum := extractPartitionNumber(oldPart)
					pool.Disks[di].Slices[si].PartitionDevice = fmt.Sprintf("%s%d", mDisk, pNum)
				}
				delete(poolDiskDevs, oldDev)
				poolDiskDevs[mDisk] = true
				break
			}
		}
	}
}

// partitionMatchesDisk checks if a partition device could belong to a disk based on partition number.
func partitionMatchesDisk(partDev, diskDev string, partNum int) bool {
	expected := fmt.Sprintf("%s%d", diskDev, partNum)
	// Also handle nvme style: /dev/nvme0n1p1
	expectedNvme := fmt.Sprintf("%sp%d", diskDev, partNum)
	return partDev == expected || partDev == expectedNvme
}

// diskFromPartition extracts the disk device from a partition device path.
func diskFromPartition(partDev string) string {
	// Handle nvme: /dev/nvme0n1p1 → /dev/nvme0n1
	if strings.Contains(partDev, "nvme") {
		idx := strings.LastIndex(partDev, "p")
		if idx > 0 {
			return partDev[:idx]
		}
	}
	// Handle sd: /dev/sda1 → /dev/sda
	return strings.TrimRight(partDev, "0123456789")
}

// extractPartitionNumber gets the partition number from a device path.
func extractPartitionNumber(partDev string) int {
	// /dev/sda1 → 1, /dev/nvme0n1p2 → 2
	s := partDev
	if strings.Contains(s, "nvme") {
		idx := strings.LastIndex(s, "p")
		if idx >= 0 {
			s = s[idx+1:]
		}
	} else {
		s = strings.TrimLeft(s, "/devabcdfghijklmnopqrstuvwxyz")
	}
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

func (e *engineImpl) verifyArraysStopped(pool *Pool) {
	data, err := os.ReadFile("/proc/mdstat")
	if err != nil {
		return
	}
	content := string(data)
	for _, arr := range pool.RAIDArrays {
		devName := strings.TrimPrefix(arr.Device, "/dev/")
		if strings.Contains(content, devName) {
			fmt.Fprintf(os.Stderr, "warning: %s still active in /proc/mdstat after stop\n", arr.Device)
		}
	}
}

func (e *engineImpl) verifyAutoDirective() {
	data, err := os.ReadFile("/etc/mdadm/mdadm.conf")
	if err != nil {
		return
	}
	if !strings.Contains(string(data), "AUTO -all") {
		fmt.Fprintf(os.Stderr, "warning: AUTO -all directive missing from /etc/mdadm/mdadm.conf\n")
	}
}



func (e *engineImpl) AssembleArrays(ctx context.Context, poolName string) error {
	pool, err := e.resolvePoolByName(poolName)
	if err != nil {
		return err
	}
	sort.Slice(pool.RAIDArrays, func(i, j int) bool {
		return pool.RAIDArrays[i].TierIndex < pool.RAIDArrays[j].TierIndex
	})
	for _, arr := range pool.RAIDArrays {
		if arr.UUID != "" {
			if _, err := e.raid.AssembleArrayBySuperblock(arr.UUID); err != nil {
				if err2 := e.raid.AssembleArray(arr.Device, arr.Members); err2 != nil {
					if _, derr := e.raid.GetArrayDetail(arr.Device); derr != nil {
						return fmt.Errorf("assemble %s: %w", arr.Device, err2)
					}
				}
			}
		} else {
			if err := e.raid.AssembleArray(arr.Device, arr.Members); err != nil {
				if _, derr := e.raid.GetArrayDetail(arr.Device); derr != nil {
					return fmt.Errorf("assemble %s: %w", arr.Device, err)
				}
			}
		}
		// Remove failed and re-add
		exec.Command("mdadm", "--manage", arr.Device, "--remove", "failed").Run()
		matches, _ := e.raid.ScanSuperblocks(arr.UUID)
		detail, _ := e.raid.GetArrayDetail(arr.Device)
		if detail != nil {
			active := make(map[string]bool)
			for _, m := range detail.Members {
				active[m.Device] = true
			}
			for _, match := range matches {
				if !active[match.PartitionDevice] {
					e.raid.ReAddMember(arr.Device, match.PartitionDevice)
				}
			}
		}
	}
	e.reconcileDeviceNames(pool)
	e.meta.SavePool(pool)
	return nil
}

func (e *engineImpl) ActivateLVM(ctx context.Context, poolName string) error {
	pool, err := e.resolvePoolByName(poolName)
	if err != nil {
		return err
	}
	return e.lvm.ActivateVolumeGroup(pool.VolumeGroup)
}

func (e *engineImpl) MountPool(ctx context.Context, poolName string) error {
	pool, err := e.resolvePoolByName(poolName)
	if err != nil {
		return err
	}
	lvPath := fmt.Sprintf("/dev/%s/%s", pool.VolumeGroup, pool.LogicalVolume)
	if err := e.fs.MountFilesystem(lvPath, pool.MountPoint); err != nil {
		return err
	}
	pool.OperationalStatus = PoolRunning
	e.meta.SavePool(pool)
	return nil
}
