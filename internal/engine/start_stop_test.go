package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/poolforge/poolforge/internal/storage"
)

// Mock implementations for testing

type mockRAID struct {
	assembleCalls  []string // UUIDs passed to AssembleArrayBySuperblock
	stopCalls      []string // devices passed to StopArray
	readdCalls     []string
	addCalls       []string
	detailState    string
	detailMembers  []storage.MemberInfo
	scanMatches    []storage.SuperblockMatch
	readdFail      bool
	assembleFail   bool
	uuid           string
}

func (m *mockRAID) CreateArray(opts storage.RAIDCreateOpts) (*storage.RAIDArrayInfo, error) { return nil, nil }
func (m *mockRAID) GetArrayDetail(device string) (*storage.RAIDArrayDetail, error) {
	return &storage.RAIDArrayDetail{Device: device, State: m.detailState, Members: m.detailMembers, UUID: m.uuid}, nil
}
func (m *mockRAID) AssembleArray(device string, members []string) error { return nil }
func (m *mockRAID) StopArray(device string) error {
	m.stopCalls = append(m.stopCalls, device)
	return nil
}
func (m *mockRAID) AddMember(device string, member string) error {
	m.addCalls = append(m.addCalls, member)
	return nil
}
func (m *mockRAID) RemoveMember(device string, member string) error { return nil }
func (m *mockRAID) ReshapeArray(device string, newCount int, newLevel int) error { return nil }
func (m *mockRAID) GetSyncStatus(device string) (*storage.SyncStatus, error) {
	return &storage.SyncStatus{InSync: true}, nil
}
func (m *mockRAID) GetArrayUUID(device string) (string, error) { return m.uuid, nil }
func (m *mockRAID) AssembleArrayBySuperblock(uuid string) (*storage.RAIDArrayInfo, error) {
	m.assembleCalls = append(m.assembleCalls, uuid)
	if m.assembleFail {
		return nil, fmt.Errorf("assembly failed")
	}
	return &storage.RAIDArrayInfo{Device: "/dev/md0", State: "active"}, nil
}
func (m *mockRAID) ReAddMember(arrayDevice string, member string) error {
	m.readdCalls = append(m.readdCalls, member)
	if m.readdFail {
		return fmt.Errorf("re-add failed")
	}
	return nil
}
func (m *mockRAID) ScanSuperblocks(arrayUUID string) ([]storage.SuperblockMatch, error) {
	return m.scanMatches, nil
}

type mockDisk struct {
	disks map[string]uint64
}

func (m *mockDisk) GetDiskInfo(device string) (*storage.DiskInfoResult, error) {
	if cap, ok := m.disks[device]; ok {
		return &storage.DiskInfoResult{Device: device, CapacityBytes: cap}, nil
	}
	return nil, fmt.Errorf("not found")
}
func (m *mockDisk) CreateGPTPartitionTable(device string) error { return nil }
func (m *mockDisk) CreatePartition(device string, start, size uint64) (*storage.Partition, error) { return nil, nil }
func (m *mockDisk) ListPartitions(device string) ([]storage.Partition, error) { return nil, nil }
func (m *mockDisk) WipePartitionTable(device string) error { return nil }
func (m *mockDisk) HasExistingData(device string) (bool, error) { return false, nil }

type mockLVM struct{}

func (m *mockLVM) CreatePhysicalVolume(device string) error { return nil }
func (m *mockLVM) CreateVolumeGroup(name string, pvDevices []string) error { return nil }
func (m *mockLVM) CreateLogicalVolume(vgName string, lvName string, sizePercent int) error { return nil }
func (m *mockLVM) GetVolumeGroupInfo(name string) (*storage.VGInfo, error) { return nil, nil }
func (m *mockLVM) ExtendVolumeGroup(name string, pvDevice string) error { return nil }
func (m *mockLVM) ExtendLogicalVolume(lvPath string) error { return nil }
func (m *mockLVM) RemoveLogicalVolume(lvPath string) error { return nil }
func (m *mockLVM) RemoveVolumeGroup(name string) error { return nil }
func (m *mockLVM) RemovePhysicalVolume(device string) error { return nil }
func (m *mockLVM) CheckPhysicalVolume(device string) bool { return true }
func (m *mockLVM) RestoreMissingPV(vgName string, device string) error { return nil }
func (m *mockLVM) ActivateVolumeGroup(name string) error { return nil }
func (m *mockLVM) DeactivateVolumeGroup(name string) error { return nil }
func (m *mockLVM) DeactivateLogicalVolume(lvPath string) error { return nil }

type mockFS struct {
	mounted map[string]bool
}

func (m *mockFS) CreateFilesystem(device string) error { return nil }
func (m *mockFS) MountFilesystem(device string, mountPoint string) error {
	if m.mounted == nil {
		m.mounted = map[string]bool{}
	}
	m.mounted[mountPoint] = true
	return nil
}
func (m *mockFS) UnmountFilesystem(mountPoint string) error {
	if m.mounted != nil {
		delete(m.mounted, mountPoint)
	}
	return nil
}
func (m *mockFS) GetUsage(mountPoint string) (*storage.FSUsage, error) {
	return &storage.FSUsage{TotalBytes: 1e9, UsedBytes: 5e8, FreeBytes: 5e8}, nil
}
func (m *mockFS) ResizeFilesystem(device string) error { return nil }

type mockMeta struct {
	pools map[string]*Pool
}

func (m *mockMeta) SavePool(pool *Pool) error {
	if m.pools == nil {
		m.pools = map[string]*Pool{}
	}
	m.pools[pool.ID] = pool
	return nil
}
func (m *mockMeta) LoadPool(poolID string) (*Pool, error) {
	if p, ok := m.pools[poolID]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("pool %q not found", poolID)
}
func (m *mockMeta) ListPools() ([]PoolSummary, error) {
	var out []PoolSummary
	for _, p := range m.pools {
		out = append(out, PoolSummary{ID: p.ID, Name: p.Name, State: p.State, DiskCount: len(p.Disks)})
	}
	return out, nil
}
func (m *mockMeta) DeletePool(poolID string) error {
	delete(m.pools, poolID)
	return nil
}

func makeTestPool(status PoolOperationalStatus) *Pool {
	return &Pool{
		ID: "test-1", Name: "mypool", ParityMode: Parity1,
		State: PoolHealthy, VolumeGroup: "vg1", LogicalVolume: "lv1",
		MountPoint: "/mnt/test", OperationalStatus: status,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Disks: []DiskInfo{
			{Device: "/dev/sda", CapacityBytes: 1e9, State: DiskHealthy,
				Slices: []SliceInfo{{TierIndex: 0, PartitionNumber: 1, PartitionDevice: "/dev/sda1", SizeBytes: 5e8}}},
			{Device: "/dev/sdb", CapacityBytes: 1e9, State: DiskHealthy,
				Slices: []SliceInfo{{TierIndex: 0, PartitionNumber: 1, PartitionDevice: "/dev/sdb1", SizeBytes: 5e8}}},
		},
		RAIDArrays: []RAIDArray{
			{Device: "/dev/md0", RAIDLevel: 5, TierIndex: 0, State: ArrayHealthy,
				Members: []string{"/dev/sda1", "/dev/sdb1"}, UUID: "test-uuid-0"},
		},
	}
}

func makeTestPoolMultiTier(status PoolOperationalStatus) *Pool {
	return &Pool{
		ID: "test-mt", Name: "multitier", ParityMode: Parity1,
		State: PoolHealthy, VolumeGroup: "vg1", LogicalVolume: "lv1",
		MountPoint: "/mnt/test", OperationalStatus: status,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Disks: []DiskInfo{
			{Device: "/dev/sda", CapacityBytes: 1e9, State: DiskHealthy},
			{Device: "/dev/sdb", CapacityBytes: 2e9, State: DiskHealthy},
		},
		RAIDArrays: []RAIDArray{
			{Device: "/dev/md0", RAIDLevel: 5, TierIndex: 0, State: ArrayHealthy,
				Members: []string{"/dev/sda1", "/dev/sdb1"}, UUID: "uuid-0"},
			{Device: "/dev/md1", RAIDLevel: 5, TierIndex: 1, State: ArrayHealthy,
				Members: []string{"/dev/sdb2"}, UUID: "uuid-1"},
			{Device: "/dev/md2", RAIDLevel: 5, TierIndex: 2, State: ArrayHealthy,
				Members: []string{"/dev/sdb3"}, UUID: "uuid-2"},
		},
	}
}

func newTestEngine(raid *mockRAID, meta *mockMeta) *engineImpl {
	return &engineImpl{
		disk:      &mockDisk{disks: map[string]uint64{"/dev/sda": 1e9, "/dev/sdb": 2e9}},
		raid:      raid,
		lvm:       &mockLVM{},
		fs:        &mockFS{},
		meta:      meta,
		stopDelay: time.Millisecond,
	}
}

// --- Unit Tests ---

func TestStartPoolOffline(t *testing.T) {
	pool := makeTestPool(PoolOffline)
	meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
	raid := &mockRAID{detailState: "active", detailMembers: []storage.MemberInfo{{Device: "/dev/sda1", State: "active"}, {Device: "/dev/sdb1", State: "active"}}}
	eng := newTestEngine(raid, meta)

	result, err := eng.StartPool(context.Background(), "mypool", true)
	if err != nil {
		t.Fatal(err)
	}
	if result.PoolName != "mypool" {
		t.Errorf("PoolName = %q", result.PoolName)
	}
	if meta.pools[pool.ID].OperationalStatus != PoolRunning {
		t.Error("expected Running after start")
	}
	if meta.pools[pool.ID].LastStartup == nil {
		t.Error("LastStartup should be set")
	}
}

func TestStartPoolSafeToShutdown(t *testing.T) {
	pool := makeTestPool(PoolSafeToShutdown)
	meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
	raid := &mockRAID{detailState: "active", detailMembers: []storage.MemberInfo{{Device: "/dev/sda1"}, {Device: "/dev/sdb1"}}}
	eng := newTestEngine(raid, meta)

	_, err := eng.StartPool(context.Background(), "mypool", true)
	if err != nil {
		t.Fatal(err)
	}
	if meta.pools[pool.ID].OperationalStatus != PoolRunning {
		t.Error("expected Running")
	}
}

func TestStartPoolAlreadyRunning(t *testing.T) {
	pool := makeTestPool(PoolRunning)
	meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
	eng := newTestEngine(&mockRAID{}, meta)

	_, err := eng.StartPool(context.Background(), "mypool", true)
	if err == nil || !contains(err.Error(), "already running") {
		t.Errorf("expected 'already running' error, got %v", err)
	}
}

func TestStartPoolFewerDrivesNoForce(t *testing.T) {
	pool := makeTestPool(PoolOffline)
	meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
	// Only /dev/sda is detected, /dev/sdb is missing
	eng := &engineImpl{
		disk:      &mockDisk{disks: map[string]uint64{"/dev/sda": 1e9}},
		raid:      &mockRAID{detailState: "active"},
		lvm:       &mockLVM{},
		fs:        &mockFS{},
		meta:      meta,
		stopDelay: time.Millisecond,
	}

	result, err := eng.StartPool(context.Background(), "mypool", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warning about fewer drives")
	}
	if len(result.ArrayResults) != 0 {
		t.Error("should not have assembled arrays without force")
	}
}

func TestStartPoolFewerDrivesForce(t *testing.T) {
	pool := makeTestPool(PoolOffline)
	meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
	eng := &engineImpl{
		disk:      &mockDisk{disks: map[string]uint64{"/dev/sda": 1e9}},
		raid:      &mockRAID{detailState: "active", detailMembers: []storage.MemberInfo{{Device: "/dev/sda1"}}},
		lvm:       &mockLVM{},
		fs:        &mockFS{},
		meta:      meta,
		stopDelay: time.Millisecond,
	}

	result, err := eng.StartPool(context.Background(), "mypool", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warning even with force")
	}
	if len(result.ArrayResults) == 0 {
		t.Error("should have assembled arrays with force")
	}
}

func TestStartPoolDegradedReAdd(t *testing.T) {
	pool := makeTestPool(PoolOffline)
	meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
	raid := &mockRAID{
		detailState:   "active, degraded",
		detailMembers: []storage.MemberInfo{{Device: "/dev/sda1", State: "active"}},
		scanMatches:   []storage.SuperblockMatch{{PartitionDevice: "/dev/sdj1", ArrayUUID: "test-uuid-0"}},
	}
	eng := newTestEngine(raid, meta)

	result, err := eng.StartPool(context.Background(), "mypool", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(raid.readdCalls) != 1 || raid.readdCalls[0] != "/dev/sdj1" {
		t.Errorf("expected re-add of /dev/sdj1, got %v", raid.readdCalls)
	}
	if len(result.ArrayResults) > 0 && len(result.ArrayResults[0].ReAddedParts) != 1 {
		t.Error("expected 1 re-added part in result")
	}
}

func TestStartPoolDegradedReAddFailsFallback(t *testing.T) {
	pool := makeTestPool(PoolOffline)
	meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
	raid := &mockRAID{
		detailState:   "active, degraded",
		detailMembers: []storage.MemberInfo{{Device: "/dev/sda1", State: "active"}},
		scanMatches:   []storage.SuperblockMatch{{PartitionDevice: "/dev/sdj1", ArrayUUID: "test-uuid-0"}},
		readdFail:     true,
	}
	eng := newTestEngine(raid, meta)

	result, err := eng.StartPool(context.Background(), "mypool", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(raid.addCalls) != 1 {
		t.Error("expected fallback to AddMember")
	}
	if len(result.ArrayResults) > 0 && len(result.ArrayResults[0].FullRebuilds) != 1 {
		t.Error("expected 1 full rebuild in result")
	}
}

func TestStartPoolAssemblyFailure(t *testing.T) {
	pool := makeTestPool(PoolOffline)
	meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
	raid := &mockRAID{assembleFail: true}
	eng := newTestEngine(raid, meta)

	_, err := eng.StartPool(context.Background(), "mypool", true)
	if err == nil {
		t.Error("expected error on assembly failure")
	}
	if meta.pools[pool.ID].OperationalStatus != PoolOffline {
		t.Error("pool should remain Offline on assembly failure")
	}
}

func TestStartPoolAscendingTierOrder(t *testing.T) {
	pool := makeTestPoolMultiTier(PoolOffline)
	meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
	raid := &mockRAID{detailState: "active", detailMembers: []storage.MemberInfo{}}
	eng := newTestEngine(raid, meta)

	_, err := eng.StartPool(context.Background(), "multitier", true)
	if err != nil {
		t.Fatal(err)
	}
	// Verify assembly order: uuid-0, uuid-1, uuid-2
	if len(raid.assembleCalls) != 3 {
		t.Fatalf("expected 3 assembly calls, got %d", len(raid.assembleCalls))
	}
	if raid.assembleCalls[0] != "uuid-0" || raid.assembleCalls[1] != "uuid-1" || raid.assembleCalls[2] != "uuid-2" {
		t.Errorf("wrong assembly order: %v", raid.assembleCalls)
	}
}

func TestStopPoolRunning(t *testing.T) {
	pool := makeTestPool(PoolRunning)
	meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
	raid := &mockRAID{}
	eng := newTestEngine(raid, meta)

	err := eng.StopPool(context.Background(), "mypool")
	if err != nil {
		t.Fatal(err)
	}
	if meta.pools[pool.ID].OperationalStatus != PoolSafeToShutdown {
		t.Error("expected Safe_To_Power_Down after stop")
	}
	if meta.pools[pool.ID].LastShutdown == nil {
		t.Error("LastShutdown should be set")
	}
}

func TestStopPoolOffline(t *testing.T) {
	pool := makeTestPool(PoolOffline)
	meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
	eng := newTestEngine(&mockRAID{}, meta)

	err := eng.StopPool(context.Background(), "mypool")
	if err == nil || !contains(err.Error(), "not running") {
		t.Errorf("expected 'not running' error, got %v", err)
	}
}

func TestStopPoolSafeToShutdown(t *testing.T) {
	pool := makeTestPool(PoolSafeToShutdown)
	meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
	eng := newTestEngine(&mockRAID{}, meta)

	err := eng.StopPool(context.Background(), "mypool")
	if err == nil || !contains(err.Error(), "not running") {
		t.Errorf("expected 'not running' error, got %v", err)
	}
}

func TestStopPoolDescendingTierOrder(t *testing.T) {
	pool := makeTestPoolMultiTier(PoolRunning)
	meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
	raid := &mockRAID{}
	eng := newTestEngine(raid, meta)

	err := eng.StopPool(context.Background(), "multitier")
	if err != nil {
		t.Fatal(err)
	}
	if len(raid.stopCalls) != 3 {
		t.Fatalf("expected 3 stop calls, got %d", len(raid.stopCalls))
	}
	// Descending: md2, md1, md0
	if raid.stopCalls[0] != "/dev/md2" || raid.stopCalls[1] != "/dev/md1" || raid.stopCalls[2] != "/dev/md0" {
		t.Errorf("wrong stop order: %v", raid.stopCalls)
	}
}

func TestSetAutoStartFalse(t *testing.T) {
	pool := makeTestPool(PoolRunning)
	pool.RequiresManualStart = false
	meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
	eng := newTestEngine(&mockRAID{}, meta)

	err := eng.SetAutoStart(context.Background(), "mypool", false)
	if err != nil {
		t.Fatal(err)
	}
	if !meta.pools[pool.ID].RequiresManualStart {
		t.Error("RequiresManualStart should be true")
	}
}

func TestSetAutoStartTrue(t *testing.T) {
	pool := makeTestPool(PoolRunning)
	pool.RequiresManualStart = true
	meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
	eng := newTestEngine(&mockRAID{}, meta)

	err := eng.SetAutoStart(context.Background(), "mypool", true)
	if err != nil {
		t.Fatal(err)
	}
	if meta.pools[pool.ID].RequiresManualStart {
		t.Error("RequiresManualStart should be false")
	}
}

func TestSetAutoStartUnknownPool(t *testing.T) {
	meta := &mockMeta{pools: map[string]*Pool{}}
	eng := newTestEngine(&mockRAID{}, meta)

	err := eng.SetAutoStart(context.Background(), "nonexistent", true)
	if err == nil || !contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got %v", err)
	}
}

func TestStartPoolNotFound(t *testing.T) {
	meta := &mockMeta{pools: map[string]*Pool{}}
	eng := newTestEngine(&mockRAID{}, meta)

	_, err := eng.StartPool(context.Background(), "nonexistent", true)
	if err == nil || !contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got %v", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
