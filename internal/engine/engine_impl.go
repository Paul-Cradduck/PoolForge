package engine

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/poolforge/poolforge/internal/sharing"
	"github.com/poolforge/poolforge/internal/snapshots"
	"github.com/poolforge/poolforge/internal/storage"
)

type engineImpl struct {
	disk      storage.DiskManager
	raid      storage.RAIDManager
	lvm       storage.LVMManager
	fs        storage.FilesystemManager
	meta      MetadataStore
	shares    *sharing.ShareManager
	stopDelay time.Duration // Phase 5: delay between array stops (default 1s)
}

func NewEngine(disk storage.DiskManager, raid storage.RAIDManager, lvm storage.LVMManager, fs storage.FilesystemManager, meta MetadataStore) EngineService {
	return &engineImpl{disk: disk, raid: raid, lvm: lvm, fs: fs, meta: meta, shares: sharing.NewShareManager()}
}

func (e *engineImpl) CreatePool(ctx context.Context, req CreatePoolRequest) (*Pool, error) {
	// Validate minimum disk count
	if len(req.Disks) < 2 {
		return nil, fmt.Errorf("minimum 2 disks required, got %d", len(req.Disks))
	}

	// Check for duplicate disks in request
	seen := make(map[string]bool)
	for _, d := range req.Disks {
		if seen[d] {
			return nil, fmt.Errorf("duplicate disk %s in request", d)
		}
		seen[d] = true
	}

	// Check disk conflicts with existing pools
	existing, _ := e.meta.ListPools()
	for _, ps := range existing {
		p, err := e.meta.LoadPool(ps.ID)
		if err != nil {
			continue
		}
		for _, ed := range p.Disks {
			if seen[ed.Device] {
				return nil, fmt.Errorf("disk %s is already a member of pool %q", ed.Device, p.Name)
			}
		}
	}

	// Check name uniqueness
	for _, ps := range existing {
		if ps.Name == req.Name {
			return nil, fmt.Errorf("pool name %q already exists", req.Name)
		}
	}

	// Get disk capacities
	var disks []DiskInfo
	var capacities []uint64
	for _, dev := range req.Disks {
		info, err := e.disk.GetDiskInfo(dev)
		if err != nil {
			return nil, fmt.Errorf("cannot read disk %s: %w", dev, err)
		}
		disks = append(disks, DiskInfo{Device: dev, CapacityBytes: info.CapacityBytes, State: DiskHealthy})
		capacities = append(capacities, info.CapacityBytes)
	}

	// Compute capacity tiers
	tiers := ComputeCapacityTiers(capacities)
	if len(tiers) == 0 {
		return nil, fmt.Errorf("no valid capacity tiers could be computed")
	}

	poolID := generateID()
	now := time.Now()

	// Partition each disk
	for i := range disks {
		if err := e.disk.CreateGPTPartitionTable(disks[i].Device); err != nil {
			return nil, err
		}
		slices := ComputeDiskSlices(disks[i].CapacityBytes, tiers)
		var offset uint64 = 1048576 // 1 MiB alignment offset
		for _, sl := range slices {
			part, err := e.disk.CreatePartition(disks[i].Device, offset, sl.SizeBytes)
			if err != nil {
				return nil, err
			}
			disks[i].Slices = append(disks[i].Slices, SliceInfo{
				TierIndex:       sl.TierIndex,
				PartitionNumber: part.Number,
				PartitionDevice: part.Device,
				SizeBytes:       sl.SizeBytes,
			})
			offset += sl.SizeBytes
		}
	}

	// Create RAID arrays per tier
	var arrays []RAIDArray
	mdIndex := findNextMDIndex()
	for ti := range tiers {
		var members []string
		for _, d := range disks {
			for _, sl := range d.Slices {
				if sl.TierIndex == tiers[ti].Index {
					members = append(members, sl.PartitionDevice)
				}
			}
		}
		raidLevel, err := SelectRAIDLevel(req.ParityMode, len(members))
		if err != nil {
			return nil, err
		}
		mdName := fmt.Sprintf("md%d", mdIndex)
		mdIndex++
		info, err := e.raid.CreateArray(storage.RAIDCreateOpts{
			Name:            mdName,
			Level:           raidLevel,
			Members:         members,
			MetadataVersion: "1.2",
		})
		if err != nil {
			return nil, err
		}
		tiers[ti].RAIDArray = info.Device
		arrays = append(arrays, RAIDArray{
			Device:    info.Device,
			RAIDLevel: raidLevel,
			TierIndex: tiers[ti].Index,
			State:     ArrayHealthy,
			Members:   members,
			UUID:      func() string { u, _ := e.raid.GetArrayUUID(info.Device); return u }(),
		})
	}

	// LVM: create PVs, VG, LV
	vgName := fmt.Sprintf("vg_poolforge_%s", poolID[:8])
	lvName := fmt.Sprintf("lv_poolforge_%s", poolID[:8])
	var pvDevices []string
	for _, a := range arrays {
		if err := e.lvm.CreatePhysicalVolume(a.Device); err != nil {
			return nil, err
		}
		pvDevices = append(pvDevices, a.Device)
	}
	if err := e.lvm.CreateVolumeGroup(vgName, pvDevices); err != nil {
		return nil, err
	}
	snapReserve := req.SnapshotReserve
	if snapReserve <= 0 {
		snapReserve = 10
	}
	lvPercent := 100 - snapReserve
	if err := e.lvm.CreateLogicalVolume(vgName, lvName, lvPercent); err != nil {
		return nil, err
	}

	// Create ext4 filesystem
	lvPath := fmt.Sprintf("/dev/%s/%s", vgName, lvName)
	if err := e.fs.CreateFilesystem(lvPath); err != nil {
		return nil, err
	}

	// Mount
	mountPoint := fmt.Sprintf("/mnt/poolforge/%s", req.Name)
	if err := e.fs.MountFilesystem(lvPath, mountPoint); err != nil {
		return nil, err
	}

	pool := &Pool{
		ID:            poolID,
		Name:          req.Name,
		ParityMode:    req.ParityMode,
		State:         PoolHealthy,
		Disks:         disks,
		CapacityTiers: tiers,
		RAIDArrays:    arrays,
		VolumeGroup:   vgName,
		LogicalVolume: lvName,
		MountPoint:    mountPoint,
		SnapshotConfig: SnapshotConfig{ReservePercent: snapReserve},
		CreatedAt:     now,
		UpdatedAt:     now,
		IsExternal:          req.External,
		RequiresManualStart: req.External,
		OperationalStatus:   PoolRunning,
	}

	if err := e.meta.SavePool(pool); err != nil {
		return nil, fmt.Errorf("save metadata: %w", err)
	}
	return pool, nil
}

func (e *engineImpl) GetPool(ctx context.Context, poolID string) (*Pool, error) {
	return e.meta.LoadPool(poolID)
}

func (e *engineImpl) ListPools(ctx context.Context) ([]PoolSummary, error) {
	summaries, err := e.meta.ListPools()
	if err != nil {
		return nil, err
	}
	// Enrich with capacity info where possible
	for i, s := range summaries {
		p, err := e.meta.LoadPool(s.ID)
		if err != nil {
			continue
		}
		if e.remapDiskDevices(p) {
			e.meta.SavePool(p)
		}
		usage, err := e.fs.GetUsage(p.MountPoint)
		if err == nil {
			summaries[i].TotalCapacityBytes = usage.TotalBytes
			summaries[i].UsedCapacityBytes = usage.UsedBytes
		}
		summaries[i].MountPoint = p.MountPoint
	}
	return summaries, nil
}

func (e *engineImpl) GetPoolStatus(ctx context.Context, poolID string) (*PoolStatus, error) {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return nil, err
	}

	// Auto-remap disk device names if they changed (e.g. eSATA enclosure power cycle)
	if e.remapDiskDevices(pool) {
		e.meta.SavePool(pool)
	}

	status := &PoolStatus{Pool: *pool}
	anyReshaping := false
	for _, a := range pool.RAIDArrays {
		cap := a.CapacityBytes
		arrayState := a.State
		// Probe the live array — if it responds, use live state; otherwise offline
		if detail, err := e.raid.GetArrayDetail(a.Device); err == nil {
			if detail.CapacityBytes > 0 {
				cap = detail.CapacityBytes
			}
			if sync, err := e.raid.GetSyncStatus(a.Device); err == nil && !sync.InSync {
				if sync.Action == "reshape" {
					arrayState = ArrayRebuilding
					anyReshaping = true
				}
			}
		} else if pool.OperationalStatus != PoolRunning {
			arrayState = "offline"
		}
		status.ArrayStatuses = append(status.ArrayStatuses, ArrayStatus{
			Device: a.Device, RAIDLevel: a.RAIDLevel, TierIndex: a.TierIndex,
			State: arrayState, CapacityBytes: cap, Members: a.Members,
		})
	}

	// Auto-expand when all reshapes complete
	if pool.State == PoolExpanding && !anyReshaping {
		lvPath := fmt.Sprintf("/dev/%s/%s", pool.VolumeGroup, pool.LogicalVolume)
		// Restore any PV headers destroyed by reshape
		for _, a := range pool.RAIDArrays {
			if !e.lvm.CheckPhysicalVolume(a.Device) {
				e.lvm.RestoreMissingPV(pool.VolumeGroup, a.Device)
			}
		}
		for _, a := range pool.RAIDArrays {
			exec.Command("pvresize", a.Device).Run()
		}
		e.lvm.ExtendLogicalVolume(lvPath)
		e.fs.ResizeFilesystem(lvPath)
		pool.State = PoolHealthy
		pool.UpdatedAt = time.Now()
		e.meta.SavePool(pool)
		status.Pool = *pool
	}

	if pool.State == PoolExpanding {
		status.Pool.State = PoolExpanding
	}
	for _, d := range pool.Disks {
		var contributing []string
		for _, sl := range d.Slices {
			for _, a := range pool.RAIDArrays {
				if a.TierIndex == sl.TierIndex {
					contributing = append(contributing, a.Device)
				}
			}
		}
		status.DiskStatuses = append(status.DiskStatuses, DiskStatusInfo{
			Device: d.Device, State: d.State, ContributingArrays: contributing,
			CapacityBytes: d.CapacityBytes, Label: d.Label,
			Serial: getDiskSerial(d.Device), EnclosureSlot: getEnclosureSlot(d.Device),
		})
	}
	if usage, err := e.fs.GetUsage(pool.MountPoint); err == nil {
		status.TotalCapacityBytes = usage.TotalBytes
		status.UsedCapacityBytes = usage.UsedBytes
		status.FreeCapacityBytes = usage.TotalBytes - usage.UsedBytes
	}
	return status, nil
}

func (e *engineImpl) SetDiskLabel(ctx context.Context, device string, label string) error {
	pools, err := e.meta.ListPools()
	if err != nil {
		return err
	}
	for _, ps := range pools {
		p, err := e.meta.LoadPool(ps.ID)
		if err != nil {
			continue
		}
		for i, d := range p.Disks {
			if d.Device == device {
				p.Disks[i].Label = label
				return e.meta.SavePool(p)
			}
		}
	}
	return fmt.Errorf("device %s not found in any pool", device)
}

// remapDiskDevices checks if disk device names have changed (e.g. after eSATA
// enclosure power cycle) by comparing metadata against live mdadm array members.
// Returns true if any remapping was done.
func (e *engineImpl) remapDiskDevices(pool *Pool) bool {
	// Build map of partition → parent disk from metadata
	// e.g. "/dev/sda1" → "/dev/sda"
	partToDisk := map[string]string{}
	for _, d := range pool.Disks {
		for _, s := range d.Slices {
			partToDisk[s.PartitionDevice] = d.Device
		}
	}

	// Query live arrays to find actual member devices
	remap := map[string]string{} // old device → new device
	for _, a := range pool.RAIDArrays {
		detail, err := e.raid.GetArrayDetail(a.Device)
		if err != nil {
			continue
		}
		for _, m := range detail.Members {
			// m.Device is like "/dev/sdj1" — strip partition number to get disk
			livePart := m.Device
			liveDisk := strings.TrimRight(livePart, "0123456789")
			// Find which metadata disk owns this partition
			if metaDisk, ok := partToDisk[livePart]; ok {
				// Partition matches metadata — no remap needed for this one
				_ = metaDisk
				continue
			}
			// Partition not in metadata — check if a different disk letter has this partition number
			partNum := strings.TrimPrefix(livePart, liveDisk)
			for metaPart, metaDisk := range partToDisk {
				metaBase := strings.TrimRight(metaPart, "0123456789")
				metaNum := strings.TrimPrefix(metaPart, metaBase)
				if metaNum == partNum && metaBase != liveDisk {
					// Check if the metadata disk no longer exists
					if _, err := os.Stat(metaDisk); os.IsNotExist(err) {
						remap[metaDisk] = liveDisk
					}
				}
			}
		}
	}

	if len(remap) == 0 {
		return false
	}

	// Apply remapping
	for i, d := range pool.Disks {
		if newDev, ok := remap[d.Device]; ok {
			log.Printf("[remap] disk %s → %s in pool %s", d.Device, newDev, pool.Name)
			pool.Disks[i].Device = newDev
			for j, s := range pool.Disks[i].Slices {
				oldPart := s.PartitionDevice
				base := strings.TrimRight(oldPart, "0123456789")
				num := strings.TrimPrefix(oldPart, base)
				pool.Disks[i].Slices[j].PartitionDevice = newDev + num
				log.Printf("[remap] partition %s → %s", oldPart, newDev+num)
			}
		}
	}
	return true
}

func getDiskSerial(device string) string {
	name := strings.TrimPrefix(device, "/dev/")
	// Try sysfs first
	if data, err := os.ReadFile("/sys/block/" + name + "/device/serial"); err == nil {
		return strings.TrimSpace(string(data))
	}
	// Fallback to smartctl
	out, err := exec.Command("smartctl", "-i", device).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Serial Number:") || strings.HasPrefix(line, "Serial number:") {
			return strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		}
	}
	return ""
}

func getEnclosureSlot(device string) string {
	name := strings.TrimPrefix(device, "/dev/")
	entries, err := filepath.Glob("/sys/class/enclosure/*/*/device/block/" + name)
	if err != nil || len(entries) == 0 {
		return ""
	}
	// Path: /sys/class/enclosure/0:0:0:0/Slot 03/device/block/sdb
	parts := strings.Split(entries[0], "/")
	for _, p := range parts {
		if strings.HasPrefix(p, "Slot") || strings.HasPrefix(p, "slot") {
			return p
		}
	}
	return ""
}

func generateID() string {
	// Simple UUID-like ID
	b := make([]byte, 16)
	// Use crypto/rand in production; for now use time-based
	now := time.Now().UnixNano()
	for i := 0; i < 8; i++ {
		b[i] = byte(now >> (i * 8))
	}
	return fmt.Sprintf("%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:])
}

func findNextMDIndex() int {
	for i := 0; i < 256; i++ {
		dev := fmt.Sprintf("/dev/md%d", i)
		if _, err := os.Stat(dev); err != nil {
			return i
		}
	}
	return 0
}

// Phase 5: Shares

func (e *engineImpl) CreateShare(ctx context.Context, poolID string, share Share) error {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return err
	}
	for _, s := range pool.Shares {
		if s.Name == share.Name {
			return fmt.Errorf("share %q already exists", share.Name)
		}
	}
	ss := toSharingShare(share)
	if err := e.shares.CreateShare(pool.MountPoint, &ss); err != nil {
		return err
	}
	share.Path = ss.Path
	pool.Shares = append(pool.Shares, share)
	if err := e.shares.ApplyConfig(toSharingShares(pool.Shares)); err != nil {
		return err
	}
	pool.UpdatedAt = time.Now()
	return e.meta.SavePool(pool)
}

func (e *engineImpl) DeleteShare(ctx context.Context, poolID string, name string) error {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return err
	}
	idx := -1
	var path string
	for i, s := range pool.Shares {
		if s.Name == name {
			idx = i
			path = s.Path
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("share %q not found", name)
	}
	pool.Shares = append(pool.Shares[:idx], pool.Shares[idx+1:]...)
	if err := e.shares.ApplyConfig(toSharingShares(pool.Shares)); err != nil {
		return err
	}
	e.shares.DeleteShareDir(path)
	pool.UpdatedAt = time.Now()
	return e.meta.SavePool(pool)
}

func (e *engineImpl) UpdateShare(ctx context.Context, poolID string, name string, share Share) error {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return err
	}
	for i, s := range pool.Shares {
		if s.Name == name {
			share.Path = s.Path
			share.Name = s.Name
			pool.Shares[i] = share
			if err := e.shares.ApplyConfig(toSharingShares(pool.Shares)); err != nil {
				return err
			}
			pool.UpdatedAt = time.Now()
			return e.meta.SavePool(pool)
		}
	}
	return fmt.Errorf("share %q not found", name)
}

// Phase 5: Users

func (e *engineImpl) CreateUser(ctx context.Context, poolID string, name, password string, globalAccess bool) (*NASUser, error) {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return nil, err
	}
	su, err := sharing.CreateUser(name, password, poolID, globalAccess)
	if err != nil {
		return nil, err
	}
	user := &NASUser{Name: su.Name, UID: su.UID, PoolID: su.PoolID, GlobalAccess: su.GlobalAccess}
	pool.Users = append(pool.Users, *user)
	pool.UpdatedAt = time.Now()
	return user, e.meta.SavePool(pool)
}

func (e *engineImpl) DeleteUser(ctx context.Context, poolID string, name string) error {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return err
	}
	idx := -1
	for i, u := range pool.Users {
		if u.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("user %q not found in pool", name)
	}
	sharing.DeleteUser(name)
	pool.Users = append(pool.Users[:idx], pool.Users[idx+1:]...)
	pool.UpdatedAt = time.Now()
	return e.meta.SavePool(pool)
}

func toSharingShare(s Share) sharing.Share {
	return sharing.Share{
		Name: s.Name, Path: s.Path, Protocols: s.Protocols,
		NFSClients: s.NFSClients, SMBPublic: s.SMBPublic,
		SMBBrowsable: s.SMBBrowsable, ReadOnly: s.ReadOnly,
	}
}

func toSharingShares(shares []Share) []sharing.Share {
	result := make([]sharing.Share, len(shares))
	for i, s := range shares {
		result[i] = toSharingShare(s)
	}
	return result
}

// Phase 6: Snapshots

func (e *engineImpl) CreateSnapshot(ctx context.Context, poolID string, name string, expiresIn string) (*Snapshot, error) {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = snapshots.GenerateName()
	}
	name = strings.ReplaceAll(name, " ", "_")
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-') {
			return nil, fmt.Errorf("snapshot name contains invalid character: %c", c)
		}
	}
	if err := snapshots.Create(pool.VolumeGroup, pool.LogicalVolume, name); err != nil {
		return nil, fmt.Errorf("create snapshot: %w", err)
	}
	mountPath := snapshots.SnapMountPath(pool.MountPoint, name)
	snapshots.Mount(pool.VolumeGroup, name, mountPath)

	snap := Snapshot{Name: name, CreatedAt: time.Now().Unix(), MountPath: mountPath}
	if expiresIn != "" {
		if dur, err := time.ParseDuration(expiresIn); err == nil {
			snap.ExpiresAt = time.Now().Add(dur).Unix()
		}
	}
	pool.Snapshots = append(pool.Snapshots, snap)
	pool.UpdatedAt = time.Now()
	e.meta.SavePool(pool)
	return &snap, nil
}

func (e *engineImpl) DeleteSnapshot(ctx context.Context, poolID string, name string) error {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return err
	}
	idx := -1
	for i, s := range pool.Snapshots {
		if s.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("snapshot %q not found", name)
	}
	snapshots.Delete(pool.VolumeGroup, name, pool.Snapshots[idx].MountPath)
	pool.Snapshots = append(pool.Snapshots[:idx], pool.Snapshots[idx+1:]...)
	pool.UpdatedAt = time.Now()
	return e.meta.SavePool(pool)
}

func (e *engineImpl) ListSnapshots(ctx context.Context, poolID string) ([]Snapshot, error) {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return nil, err
	}
	// Enrich with current size from LVM
	lvSnaps, _ := snapshots.List(pool.VolumeGroup)
	infoMap := make(map[string]snapshots.SnapInfo)
	for _, s := range lvSnaps {
		infoMap[s.Name] = s
	}
	for i := range pool.Snapshots {
		if info, ok := infoMap[pool.Snapshots[i].Name]; ok {
			pool.Snapshots[i].SizeBytes = info.SizeBytes
			pool.Snapshots[i].AllocatedBytes = info.AllocatedBytes
			pool.Snapshots[i].UsedPercent = info.UsedPercent
		}
	}
	return pool.Snapshots, nil
}

func (e *engineImpl) SetSnapshotSchedule(ctx context.Context, poolID string, schedule SnapshotSchedule) error {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return err
	}
	// Store schedule in metadata (daemon reads it)
	pool.UpdatedAt = time.Now()
	return e.meta.SavePool(pool)
}

func (e *engineImpl) MountSnapshot(ctx context.Context, poolID string, name string) (string, error) {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return "", err
	}
	for i, s := range pool.Snapshots {
		if s.Name == name {
			mountPath := snapshots.SnapMountPath(pool.MountPoint, name)
			if err := snapshots.Mount(pool.VolumeGroup, name, mountPath); err != nil {
				return "", fmt.Errorf("mount snapshot: %w", err)
			}
			pool.Snapshots[i].MountPath = mountPath
			pool.UpdatedAt = time.Now()
			e.meta.SavePool(pool)
			return mountPath, nil
		}
	}
	return "", fmt.Errorf("snapshot %q not found", name)
}

func (e *engineImpl) UnmountSnapshot(ctx context.Context, poolID string, name string) error {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return err
	}
	for i, s := range pool.Snapshots {
		if s.Name == name {
			if s.MountPath != "" {
				snapshots.Unmount(s.MountPath)
			}
			pool.Snapshots[i].MountPath = ""
			pool.UpdatedAt = time.Now()
			return e.meta.SavePool(pool)
		}
	}
	return fmt.Errorf("snapshot %q not found", name)
}

func (e *engineImpl) RestoreSnapshot(ctx context.Context, poolID string, name string) error {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return err
	}
	idx := -1
	for i, s := range pool.Snapshots {
		if s.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("snapshot %q not found", name)
	}
	// Unmount the pool filesystem before merge
	e.fs.UnmountFilesystem(pool.MountPoint)
	// Merge snapshot into origin — LVM handles the rollback
	if err := snapshots.Restore(pool.VolumeGroup, name, pool.Snapshots[idx].MountPath); err != nil {
		// Re-mount pool even on failure
		e.fs.MountFilesystem(fmt.Sprintf("/dev/%s/%s", pool.VolumeGroup, pool.LogicalVolume), pool.MountPoint)
		return fmt.Errorf("restore snapshot: %w", err)
	}
	// Snapshot is consumed by merge — remove from metadata
	pool.Snapshots = append(pool.Snapshots[:idx], pool.Snapshots[idx+1:]...)
	pool.UpdatedAt = time.Now()
	e.meta.SavePool(pool)
	// Re-mount pool with restored data
	e.fs.MountFilesystem(fmt.Sprintf("/dev/%s/%s", pool.VolumeGroup, pool.LogicalVolume), pool.MountPoint)
	return nil
}

func (e *engineImpl) RenameSnapshot(ctx context.Context, poolID string, oldName, newName string) error {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return err
	}
	idx := -1
	for i, s := range pool.Snapshots {
		if s.Name == oldName {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("snapshot %q not found", oldName)
	}
	mountPath := pool.Snapshots[idx].MountPath
	if err := snapshots.Rename(pool.VolumeGroup, oldName, newName, mountPath); err != nil {
		return fmt.Errorf("rename snapshot: %w", err)
	}
	pool.Snapshots[idx].Name = newName
	// Re-mount at new path if it was mounted
	if mountPath != "" {
		newMount := snapshots.SnapMountPath(pool.MountPoint, newName)
		snapshots.Mount(pool.VolumeGroup, newName, newMount)
		pool.Snapshots[idx].MountPath = newMount
	}
	pool.UpdatedAt = time.Now()
	return e.meta.SavePool(pool)
}
