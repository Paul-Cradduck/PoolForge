package metadata

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/poolforge/poolforge/internal/engine"
	"pgregory.net/rapid"
)

// Feature: poolforge-phase5-enclosure-support, Property 84: Pool metadata round-trip with Phase 5 fields
func TestPropertyP84_MetadataRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rapid.Check(t, func(t *rapid.T) {
		store := NewJSONStore(filepath.Join(dir, "meta.json"))

		opStatuses := []engine.PoolOperationalStatus{engine.PoolRunning, engine.PoolOffline, engine.PoolSafeToShutdown}
		opStatus := opStatuses[rapid.IntRange(0, 2).Draw(t, "opStatus")]
		isExt := rapid.Bool().Draw(t, "isExternal")
		manualStart := rapid.Bool().Draw(t, "manualStart")
		hasShutdown := rapid.Bool().Draw(t, "hasShutdown")
		hasStartup := rapid.Bool().Draw(t, "hasStartup")
		uuid := rapid.StringMatching(`[0-9a-f]{8}:[0-9a-f]{8}:[0-9a-f]{8}:[0-9a-f]{8}`).Draw(t, "uuid")

		now := time.Now().Truncate(time.Second)
		var shutdown, startup *time.Time
		if hasShutdown {
			s := now.Add(-1 * time.Hour)
			shutdown = &s
		}
		if hasStartup {
			s := now.Add(-30 * time.Minute)
			startup = &s
		}

		pool := &engine.Pool{
			ID: "prop-84", Name: "test", ParityMode: engine.Parity1,
			State: engine.PoolHealthy, VolumeGroup: "vg", LogicalVolume: "lv",
			MountPoint: "/mnt/t", CreatedAt: now, UpdatedAt: now,
			IsExternal: isExt, RequiresManualStart: manualStart,
			OperationalStatus: opStatus, LastShutdown: shutdown, LastStartup: startup,
			Disks: []engine.DiskInfo{{Device: "/dev/sda", CapacityBytes: 1e9, State: engine.DiskHealthy}},
			RAIDArrays: []engine.RAIDArray{{Device: "/dev/md0", RAIDLevel: 5, TierIndex: 0, State: engine.ArrayHealthy, Members: []string{"/dev/sda1"}, UUID: uuid}},
		}

		if err := store.SavePool(pool); err != nil {
			t.Fatal(err)
		}
		loaded, err := store.LoadPool("prop-84")
		if err != nil {
			t.Fatal(err)
		}

		if loaded.IsExternal != isExt {
			t.Errorf("IsExternal: got %v, want %v", loaded.IsExternal, isExt)
		}
		if loaded.RequiresManualStart != manualStart {
			t.Errorf("RequiresManualStart: got %v, want %v", loaded.RequiresManualStart, manualStart)
		}
		if loaded.OperationalStatus != opStatus {
			t.Errorf("OperationalStatus: got %q, want %q", loaded.OperationalStatus, opStatus)
		}
		if hasShutdown && (loaded.LastShutdown == nil || !loaded.LastShutdown.Equal(*shutdown)) {
			t.Error("LastShutdown mismatch")
		}
		if !hasShutdown && loaded.LastShutdown != nil {
			t.Error("LastShutdown should be nil")
		}
		if hasStartup && (loaded.LastStartup == nil || !loaded.LastStartup.Equal(*startup)) {
			t.Error("LastStartup mismatch")
		}
		if !hasStartup && loaded.LastStartup != nil {
			t.Error("LastStartup should be nil")
		}
		if loaded.RAIDArrays[0].UUID != uuid {
			t.Errorf("UUID: got %q, want %q", loaded.RAIDArrays[0].UUID, uuid)
		}
	})
}

// Feature: poolforge-phase5-enclosure-support, Property 85: Backward-compatible metadata defaults
func TestPropertyP85_BackwardCompatibleDefaults(t *testing.T) {
	dir := t.TempDir()
	rapid.Check(t, func(t *rapid.T) {
		store := NewJSONStore(filepath.Join(dir, "meta.json"))
		now := time.Now().Truncate(time.Second)
		name := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "name")

		// Simulate Phase 4 pool: save with empty Phase 5 fields
		pool := &engine.Pool{
			ID: "compat-test", Name: name, ParityMode: engine.Parity1,
			State: engine.PoolHealthy, VolumeGroup: "vg", LogicalVolume: "lv",
			MountPoint: "/mnt/" + name, CreatedAt: now, UpdatedAt: now,
			OperationalStatus: "", // simulate missing
			Disks:      []engine.DiskInfo{{Device: "/dev/sda", CapacityBytes: 1e9, State: engine.DiskHealthy}},
			RAIDArrays: []engine.RAIDArray{{Device: "/dev/md0", RAIDLevel: 5, TierIndex: 0, State: engine.ArrayHealthy, Members: []string{"/dev/sda1"}}},
		}
		store.SavePool(pool)

		loaded, err := store.LoadPool("compat-test")
		if err != nil {
			t.Fatal(err)
		}

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
			t.Error("LastShutdown should be nil")
		}
		if loaded.LastStartup != nil {
			t.Error("LastStartup should be nil")
		}
		if loaded.RAIDArrays[0].UUID != "" {
			t.Error("UUID should default to empty")
		}
	})
}
