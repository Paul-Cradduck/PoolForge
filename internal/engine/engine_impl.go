package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/poolforge/poolforge/internal/sharing"
	"github.com/poolforge/poolforge/internal/storage"
)

type engineImpl struct {
	disk   storage.DiskManager
	raid   storage.RAIDManager
	lvm    storage.LVMManager
	fs     storage.FilesystemManager
	meta   MetadataStore
	shares *sharing.ShareManager
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
	if err := e.lvm.CreateLogicalVolume(vgName, lvName, 100); err != nil {
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
		CreatedAt:     now,
		UpdatedAt:     now,
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
		usage, err := e.fs.GetUsage(p.MountPoint)
		if err == nil {
			summaries[i].TotalCapacityBytes = usage.TotalBytes
			summaries[i].UsedCapacityBytes = usage.UsedBytes
		}
	}
	return summaries, nil
}

func (e *engineImpl) GetPoolStatus(ctx context.Context, poolID string) (*PoolStatus, error) {
	pool, err := e.meta.LoadPool(poolID)
	if err != nil {
		return nil, err
	}

	status := &PoolStatus{Pool: *pool}
	anyReshaping := false
	for _, a := range pool.RAIDArrays {
		cap := a.CapacityBytes
		arrayState := a.State
		if detail, err := e.raid.GetArrayDetail(a.Device); err == nil {
			if detail.CapacityBytes > 0 {
				cap = detail.CapacityBytes
			}
		}
		if sync, err := e.raid.GetSyncStatus(a.Device); err == nil && !sync.InSync {
			if sync.Action == "reshape" {
				arrayState = ArrayRebuilding
				anyReshaping = true
			}
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
			CapacityBytes: d.CapacityBytes,
		})
	}
	if usage, err := e.fs.GetUsage(pool.MountPoint); err == nil {
		status.TotalCapacityBytes = usage.TotalBytes
		status.UsedCapacityBytes = usage.UsedBytes
		status.FreeCapacityBytes = usage.TotalBytes - usage.UsedBytes
	}
	return status, nil
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
