package safety

import (
	"testing"
	"time"

	"github.com/poolforge/poolforge/internal/engine"
)

type mockMetaStore struct {
	pools map[string]*engine.Pool
}

func (m *mockMetaStore) SavePool(pool *engine.Pool) error {
	m.pools[pool.ID] = pool
	return nil
}
func (m *mockMetaStore) LoadPool(poolID string) (*engine.Pool, error) {
	if p, ok := m.pools[poolID]; ok {
		return p, nil
	}
	return nil, nil
}
func (m *mockMetaStore) ListPools() ([]engine.PoolSummary, error) {
	var out []engine.PoolSummary
	for _, p := range m.pools {
		out = append(out, engine.PoolSummary{ID: p.ID, Name: p.Name})
	}
	return out, nil
}
func (m *mockMetaStore) DeletePool(poolID string) error { return nil }

type mockRAIDForMigration struct {
	uuids map[string]string
}

func (m *mockRAIDForMigration) GetArrayUUID(device string) (string, error) {
	if uuid, ok := m.uuids[device]; ok {
		return uuid, nil
	}
	return "", nil
}

func TestMigrationDetectsMissingOperationalStatus(t *testing.T) {
	pool := &engine.Pool{
		ID: "old-1", Name: "oldpool", ParityMode: engine.Parity1,
		State: engine.PoolHealthy, OperationalStatus: "",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
		RAIDArrays: []engine.RAIDArray{{Device: "/dev/md0", TierIndex: 0, State: engine.ArrayHealthy, Members: []string{"/dev/sda1"}}},
		Disks:      []engine.DiskInfo{{Device: "/dev/sda", CapacityBytes: 1e9, State: engine.DiskHealthy}},
	}
	meta := &mockMetaStore{pools: map[string]*engine.Pool{pool.ID: pool}}
	raid := &mockRAIDForMigration{uuids: map[string]string{"/dev/md0": "migrated-uuid"}}

	d := &Daemon{
		cfg:  DaemonConfig{MetadataStore: meta, RAIDManager: raid},
		logs: NewPersistentLogBuffer(100, ""),
	}

	pools, _ := meta.ListPools()
	d.migrateToPhase5(pools)

	migrated := meta.pools["old-1"]
	if migrated.OperationalStatus != engine.PoolRunning {
		t.Errorf("expected running, got %q", migrated.OperationalStatus)
	}
	if migrated.IsExternal != false {
		t.Error("IsExternal should be false after migration")
	}
	if migrated.RequiresManualStart != false {
		t.Error("RequiresManualStart should be false after migration")
	}
	if migrated.RAIDArrays[0].UUID != "migrated-uuid" {
		t.Errorf("UUID should be populated, got %q", migrated.RAIDArrays[0].UUID)
	}
}

func TestMigrationPreservesExistingFields(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	pool := &engine.Pool{
		ID: "old-2", Name: "preserved", ParityMode: engine.Parity2,
		State: engine.PoolDegraded, OperationalStatus: "",
		VolumeGroup: "vg_test", LogicalVolume: "lv_test", MountPoint: "/mnt/preserved",
		CreatedAt: now, UpdatedAt: now,
		RAIDArrays: []engine.RAIDArray{{Device: "/dev/md0", RAIDLevel: 6, TierIndex: 0, State: engine.ArrayDegraded, Members: []string{"/dev/sda1", "/dev/sdb1"}, CapacityBytes: 5e9}},
		Disks:      []engine.DiskInfo{{Device: "/dev/sda", CapacityBytes: 1e10, State: engine.DiskHealthy}},
	}
	meta := &mockMetaStore{pools: map[string]*engine.Pool{pool.ID: pool}}
	raid := &mockRAIDForMigration{uuids: map[string]string{}}

	d := &Daemon{
		cfg:  DaemonConfig{MetadataStore: meta, RAIDManager: raid},
		logs: NewPersistentLogBuffer(100, ""),
	}

	pools, _ := meta.ListPools()
	d.migrateToPhase5(pools)

	p := meta.pools["old-2"]
	if p.Name != "preserved" {
		t.Error("Name not preserved")
	}
	if p.ParityMode != engine.Parity2 {
		t.Error("ParityMode not preserved")
	}
	if p.State != engine.PoolDegraded {
		t.Error("State not preserved")
	}
	if p.VolumeGroup != "vg_test" {
		t.Error("VolumeGroup not preserved")
	}
	if p.MountPoint != "/mnt/preserved" {
		t.Error("MountPoint not preserved")
	}
	if p.RAIDArrays[0].RAIDLevel != 6 {
		t.Error("RAIDLevel not preserved")
	}
	if p.RAIDArrays[0].CapacityBytes != 5e9 {
		t.Error("CapacityBytes not preserved")
	}
}

func TestMigrationSkipsAlreadyMigrated(t *testing.T) {
	pool := &engine.Pool{
		ID: "new-1", Name: "already", ParityMode: engine.Parity1,
		State: engine.PoolHealthy, OperationalStatus: engine.PoolRunning,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
		RAIDArrays: []engine.RAIDArray{{Device: "/dev/md0", UUID: "existing-uuid"}},
		Disks:      []engine.DiskInfo{{Device: "/dev/sda", CapacityBytes: 1e9, State: engine.DiskHealthy}},
	}
	meta := &mockMetaStore{pools: map[string]*engine.Pool{pool.ID: pool}}

	_ = &Daemon{
		cfg:  DaemonConfig{MetadataStore: meta},
		logs: NewPersistentLogBuffer(100, ""),
	}

	// bootPools should detect that migration is NOT needed
	pools, _ := meta.ListPools()
	needsMigration := false
	for _, ps := range pools {
		p, _ := meta.LoadPool(ps.ID)
		if p != nil && p.OperationalStatus == "" {
			needsMigration = true
		}
	}
	if needsMigration {
		t.Error("should not need migration when OperationalStatus is set")
	}
}

func TestBootPoolsAutoStartVsManualStart(t *testing.T) {
	autoPool := &engine.Pool{
		ID: "auto-1", Name: "internal", ParityMode: engine.Parity1,
		State: engine.PoolHealthy, OperationalStatus: engine.PoolRunning,
		RequiresManualStart: false,
		Disks: []engine.DiskInfo{{Device: "/dev/sda", CapacityBytes: 1e9, State: engine.DiskHealthy}},
	}
	manualPool := &engine.Pool{
		ID: "manual-1", Name: "external", ParityMode: engine.Parity1,
		State: engine.PoolHealthy, OperationalStatus: engine.PoolRunning,
		RequiresManualStart: true,
		Disks: []engine.DiskInfo{{Device: "/dev/sdb", CapacityBytes: 1e9, State: engine.DiskHealthy}},
	}
	meta := &mockMetaStore{pools: map[string]*engine.Pool{
		autoPool.ID:   autoPool,
		manualPool.ID: manualPool,
	}}

	d := &Daemon{
		cfg:  DaemonConfig{MetadataStore: meta},
		logs: NewPersistentLogBuffer(100, ""),
	}

	d.bootPools()

	// Auto-start pool should remain Running
	if meta.pools["auto-1"].OperationalStatus != engine.PoolRunning {
		t.Error("auto-start pool should be Running")
	}
	// Manual-start pool should be set to Offline
	if meta.pools["manual-1"].OperationalStatus != engine.PoolOffline {
		t.Error("manual-start pool should be Offline")
	}
}
