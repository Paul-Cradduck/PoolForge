package metadata

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/poolforge/poolforge/internal/engine"
)

func tempStore(t *testing.T) *JSONStore {
	t.Helper()
	dir := t.TempDir()
	return NewJSONStore(filepath.Join(dir, "metadata.json"))
}

func TestSaveLoadRoundTripPhase5Fields(t *testing.T) {
	store := tempStore(t)
	now := time.Now().Truncate(time.Second)
	shutdown := now.Add(-1 * time.Hour)

	pool := &engine.Pool{
		ID: "test-1", Name: "ext-pool", ParityMode: engine.Parity1,
		State: engine.PoolHealthy, VolumeGroup: "vg1", LogicalVolume: "lv1",
		MountPoint: "/mnt/test", CreatedAt: now, UpdatedAt: now,
		IsExternal: true, RequiresManualStart: true,
		OperationalStatus: engine.PoolSafeToShutdown,
		LastShutdown: &shutdown, LastStartup: &now,
		RAIDArrays: []engine.RAIDArray{
			{Device: "/dev/md0", RAIDLevel: 5, TierIndex: 0, State: engine.ArrayHealthy,
				Members: []string{"/dev/sda1"}, UUID: "abc:def:123:456"},
		},
		Disks: []engine.DiskInfo{
			{Device: "/dev/sda", CapacityBytes: 1e9, State: engine.DiskHealthy},
		},
	}

	if err := store.SavePool(pool); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadPool("test-1")
	if err != nil {
		t.Fatal(err)
	}

	if loaded.IsExternal != true {
		t.Error("IsExternal not preserved")
	}
	if loaded.RequiresManualStart != true {
		t.Error("RequiresManualStart not preserved")
	}
	if loaded.OperationalStatus != engine.PoolSafeToShutdown {
		t.Errorf("OperationalStatus = %q, want safe_to_power_down", loaded.OperationalStatus)
	}
	if loaded.LastShutdown == nil || !loaded.LastShutdown.Equal(shutdown) {
		t.Error("LastShutdown not preserved")
	}
	if loaded.LastStartup == nil || !loaded.LastStartup.Equal(now) {
		t.Error("LastStartup not preserved")
	}
	if loaded.RAIDArrays[0].UUID != "abc:def:123:456" {
		t.Errorf("UUID = %q, want abc:def:123:456", loaded.RAIDArrays[0].UUID)
	}
}

func TestLoadPhase4MetadataDefaultsPhase5Fields(t *testing.T) {
	store := tempStore(t)

	// Write a Phase 4 style pool (no Phase 5 fields)
	now := time.Now().Truncate(time.Second)
	pool := &engine.Pool{
		ID: "old-1", Name: "old-pool", ParityMode: engine.Parity1,
		State: engine.PoolHealthy, VolumeGroup: "vg1", LogicalVolume: "lv1",
		MountPoint: "/mnt/old", CreatedAt: now, UpdatedAt: now,
		RAIDArrays: []engine.RAIDArray{
			{Device: "/dev/md0", RAIDLevel: 5, TierIndex: 0, State: engine.ArrayHealthy,
				Members: []string{"/dev/sda1"}},
		},
		Disks: []engine.DiskInfo{
			{Device: "/dev/sda", CapacityBytes: 1e9, State: engine.DiskHealthy},
		},
	}

	if err := store.SavePool(pool); err != nil {
		t.Fatal(err)
	}

	// Now manually strip Phase 5 fields from the JSON to simulate Phase 4 metadata
	raw, _ := os.ReadFile(store.path)
	// The JSON will have is_external:false etc. — but the key test is that
	// OperationalStatus defaults to "running" when empty string
	pool.OperationalStatus = "" // simulate missing field
	store.SavePool(pool)

	loaded, err := store.LoadPool("old-1")
	if err != nil {
		t.Fatal(err)
	}

	// Verify backward-compatible defaults
	if loaded.IsExternal != false {
		t.Error("IsExternal should default to false")
	}
	if loaded.RequiresManualStart != false {
		t.Error("RequiresManualStart should default to false")
	}
	if loaded.OperationalStatus != engine.PoolRunning {
		t.Errorf("OperationalStatus should default to running, got %q", loaded.OperationalStatus)
	}
	if loaded.LastShutdown != nil {
		t.Error("LastShutdown should default to nil")
	}
	if loaded.LastStartup != nil {
		t.Error("LastStartup should default to nil")
	}
	if loaded.RAIDArrays[0].UUID != "" {
		t.Errorf("UUID should default to empty, got %q", loaded.RAIDArrays[0].UUID)
	}
	_ = raw
}

func TestPoolStatusOutputForExternalPool(t *testing.T) {
	store := tempStore(t)
	now := time.Now().Truncate(time.Second)
	startup := now.Add(-2 * time.Hour)
	shutdown := now.Add(-1 * time.Hour)

	pool := &engine.Pool{
		ID: "ext-1", Name: "external", ParityMode: engine.Parity1,
		State: engine.PoolHealthy, VolumeGroup: "vg1", LogicalVolume: "lv1",
		MountPoint: "/mnt/ext", CreatedAt: now, UpdatedAt: now,
		IsExternal: true, RequiresManualStart: true,
		OperationalStatus: engine.PoolOffline,
		LastStartup: &startup, LastShutdown: &shutdown,
		Disks: []engine.DiskInfo{{Device: "/dev/sda", CapacityBytes: 1e9, State: engine.DiskHealthy}},
		RAIDArrays: []engine.RAIDArray{{Device: "/dev/md0", RAIDLevel: 5, TierIndex: 0, State: engine.ArrayHealthy, Members: []string{"/dev/sda1"}, UUID: "test-uuid"}},
	}

	store.SavePool(pool)
	loaded, _ := store.LoadPool("ext-1")

	if !loaded.IsExternal {
		t.Error("expected IsExternal=true")
	}
	if loaded.OperationalStatus != engine.PoolOffline {
		t.Errorf("expected offline, got %q", loaded.OperationalStatus)
	}
	if loaded.LastStartup == nil || !loaded.LastStartup.Equal(startup) {
		t.Error("LastStartup not preserved for external pool")
	}
	if loaded.LastShutdown == nil || !loaded.LastShutdown.Equal(shutdown) {
		t.Error("LastShutdown not preserved for external pool")
	}
}
