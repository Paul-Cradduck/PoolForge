package safety

import (
	"testing"
	"time"

	"github.com/poolforge/poolforge/internal/engine"
	"pgregory.net/rapid"
)

// Feature: poolforge-phase5-enclosure-support, Property 98: Safe software upgrade preserves data and configuration
func TestPropertyP98_SafeUpgrade(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "name")
		nArrays := rapid.IntRange(1, 4).Draw(t, "nArrays")
		now := time.Now().Truncate(time.Second)

		var arrays []engine.RAIDArray
		uuids := map[string]string{}
		for i := 0; i < nArrays; i++ {
			dev := "/dev/md" + rapid.StringMatching(`\d+`).Draw(t, "dev")
			uuid := rapid.StringMatching(`[0-9a-f]{8}`).Draw(t, "uuid")
			arrays = append(arrays, engine.RAIDArray{
				Device: dev, RAIDLevel: 5, TierIndex: i, State: engine.ArrayHealthy,
				Members: []string{"/dev/sda1"}, UUID: "", // Phase 4: no UUID
			})
			uuids[dev] = uuid
		}

		// Simulate Phase 4 pool (no Phase 5 fields)
		pool := &engine.Pool{
			ID: "upgrade-test", Name: name, ParityMode: engine.Parity1,
			State: engine.PoolHealthy, OperationalStatus: "", // missing = needs migration
			VolumeGroup: "vg_" + name, LogicalVolume: "lv_" + name,
			MountPoint: "/mnt/" + name, CreatedAt: now, UpdatedAt: now,
			RAIDArrays: arrays,
			Disks:      []engine.DiskInfo{{Device: "/dev/sda", CapacityBytes: 1e9, State: engine.DiskHealthy}},
		}

		meta := &mockMetaStore{pools: map[string]*engine.Pool{pool.ID: pool}}
		raid := &mockRAIDForMigration{uuids: uuids}

		d := &Daemon{
			cfg:  DaemonConfig{MetadataStore: meta, RAIDManager: raid},
			logs: NewPersistentLogBuffer(100, ""),
		}

		pools, _ := meta.ListPools()
		d.migrateToPhase5(pools)

		migrated := meta.pools["upgrade-test"]

		// Phase 5 defaults applied
		if migrated.IsExternal != false {
			t.Error("IsExternal should default to false")
		}
		if migrated.RequiresManualStart != false {
			t.Error("RequiresManualStart should default to false")
		}
		if migrated.OperationalStatus != engine.PoolRunning {
			t.Errorf("OperationalStatus should be running, got %q", migrated.OperationalStatus)
		}

		// UUIDs populated
		for i, arr := range migrated.RAIDArrays {
			expectedUUID := uuids[arr.Device]
			if arr.UUID != expectedUUID {
				t.Errorf("array %d UUID: got %q, want %q", i, arr.UUID, expectedUUID)
			}
		}

		// Pre-existing fields preserved
		if migrated.Name != name {
			t.Error("Name not preserved")
		}
		if migrated.VolumeGroup != "vg_"+name {
			t.Error("VolumeGroup not preserved")
		}
		if migrated.MountPoint != "/mnt/"+name {
			t.Error("MountPoint not preserved")
		}
		if !migrated.CreatedAt.Equal(now) {
			t.Error("CreatedAt not preserved")
		}
	})
}
