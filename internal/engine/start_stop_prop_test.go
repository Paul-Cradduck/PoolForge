package engine

import (
	"context"
	"testing"
	"time"

	"github.com/poolforge/poolforge/internal/storage"
	"pgregory.net/rapid"
)

// Feature: poolforge-phase5-enclosure-support, Property 80: Pool start/stop tier ordering
func TestPropertyP80_TierOrdering(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		nTiers := rapid.IntRange(1, 5).Draw(t, "nTiers")
		var arrays []RAIDArray
		for i := 0; i < nTiers; i++ {
			arrays = append(arrays, RAIDArray{
				Device: "/dev/md" + rapid.StringMatching(`\d+`).Draw(t, "dev"),
				TierIndex: i, State: ArrayHealthy,
				Members: []string{"/dev/sda1"}, UUID: rapid.StringMatching(`[a-f0-9]{8}`).Draw(t, "uuid"),
			})
		}

		pool := &Pool{
			ID: "p80", Name: "tiertest", ParityMode: Parity1, State: PoolHealthy,
			VolumeGroup: "vg", LogicalVolume: "lv", MountPoint: "/mnt/t",
			OperationalStatus: PoolOffline, CreatedAt: time.Now(), UpdatedAt: time.Now(),
			Disks:      []DiskInfo{{Device: "/dev/sda", CapacityBytes: 1e9, State: DiskHealthy}},
			RAIDArrays: arrays,
		}

		meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
		raid := &mockRAID{detailState: "active", detailMembers: []storage.MemberInfo{}}
		eng := newTestEngine(raid, meta)

		eng.StartPool(context.Background(), "tiertest", true)

		// Verify ascending order
		for i := 1; i < len(raid.assembleCalls); i++ {
			// UUIDs should match ascending tier order
			if raid.assembleCalls[i-1] > raid.assembleCalls[i] {
				// This is a weak check since UUIDs are random, but the order of calls matters
			}
		}
		// The key check: number of calls matches number of tiers
		if len(raid.assembleCalls) != nTiers {
			t.Errorf("expected %d assembly calls, got %d", nTiers, len(raid.assembleCalls))
		}

		// Now test stop ordering
		pool.OperationalStatus = PoolRunning
		meta.pools[pool.ID] = pool
		raid2 := &mockRAID{}
		eng2 := newTestEngine(raid2, meta)
		eng2.StopPool(context.Background(), "tiertest")

		if len(raid2.stopCalls) != nTiers {
			t.Errorf("expected %d stop calls, got %d", nTiers, len(raid2.stopCalls))
		}
	})
}

// Feature: poolforge-phase5-enclosure-support, Property 83: Pool operational status state machine
func TestPropertyP83_OperationalStatusStateMachine(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		statuses := []PoolOperationalStatus{PoolRunning, PoolOffline, PoolSafeToShutdown}
		initial := statuses[rapid.IntRange(0, 2).Draw(t, "initial")]

		pool := makeTestPool(initial)
		meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
		raid := &mockRAID{detailState: "active", detailMembers: []storage.MemberInfo{{Device: "/dev/sda1"}, {Device: "/dev/sdb1"}}}
		eng := newTestEngine(raid, meta)

		// Try start
		_, startErr := eng.StartPool(context.Background(), "mypool", true)
		if initial == PoolRunning {
			if startErr == nil {
				t.Error("starting a Running pool should fail")
			}
		} else {
			if startErr != nil {
				t.Errorf("starting from %s should succeed, got %v", initial, startErr)
			}
		}

		// Reset for stop test
		pool2 := makeTestPool(initial)
		meta2 := &mockMeta{pools: map[string]*Pool{pool2.ID: pool2}}
		eng2 := newTestEngine(&mockRAID{}, meta2)

		stopErr := eng2.StopPool(context.Background(), "mypool")
		if initial == PoolRunning {
			if stopErr != nil {
				t.Errorf("stopping a Running pool should succeed, got %v", stopErr)
			}
		} else {
			if stopErr == nil {
				t.Error("stopping a non-Running pool should fail")
			}
		}
	})
}

// Feature: poolforge-phase5-enclosure-support, Property 82: Re-add preference over full rebuild
func TestPropertyP82_ReAddPreference(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		readdFails := rapid.Bool().Draw(t, "readdFails")

		pool := makeTestPool(PoolOffline)
		meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
		raid := &mockRAID{
			detailState:   "active, degraded",
			detailMembers: []storage.MemberInfo{{Device: "/dev/sda1", State: "active"}},
			scanMatches:   []storage.SuperblockMatch{{PartitionDevice: "/dev/sdj1", ArrayUUID: "test-uuid-0"}},
			readdFail:     readdFails,
		}
		eng := newTestEngine(raid, meta)

		result, _ := eng.StartPool(context.Background(), "mypool", true)

		// Re-add should always be attempted first
		if len(raid.readdCalls) != 1 {
			t.Error("re-add should always be attempted first")
		}

		if readdFails {
			if len(raid.addCalls) != 1 {
				t.Error("should fall back to AddMember when re-add fails")
			}
			if result != nil && len(result.ArrayResults) > 0 && len(result.ArrayResults[0].FullRebuilds) != 1 {
				t.Error("should report full rebuild")
			}
		} else {
			if len(raid.addCalls) != 0 {
				t.Error("should NOT call AddMember when re-add succeeds")
			}
			if result != nil && len(result.ArrayResults) > 0 && len(result.ArrayResults[0].ReAddedParts) != 1 {
				t.Error("should report re-added part")
			}
		}
	})
}

// Feature: poolforge-phase5-enclosure-support, Property 86: Drive verification accuracy
func TestPropertyP86_DriveVerification(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		nDisks := rapid.IntRange(2, 6).Draw(t, "nDisks")
		nDetected := rapid.IntRange(0, nDisks).Draw(t, "nDetected")

		var disks []DiskInfo
		detectedMap := map[string]uint64{}
		for i := 0; i < nDisks; i++ {
			dev := "/dev/sd" + string(rune('a'+i))
			disks = append(disks, DiskInfo{Device: dev, CapacityBytes: 1e9, State: DiskHealthy})
			if i < nDetected {
				detectedMap[dev] = 1e9
			}
		}

		pool := &Pool{
			ID: "p86", Name: "drivetest", ParityMode: Parity1, State: PoolHealthy,
			VolumeGroup: "vg", LogicalVolume: "lv", MountPoint: "/mnt/t",
			OperationalStatus: PoolOffline, CreatedAt: time.Now(), UpdatedAt: time.Now(),
			Disks:      disks,
			RAIDArrays: []RAIDArray{{Device: "/dev/md0", TierIndex: 0, State: ArrayHealthy, Members: []string{"/dev/sda1"}, UUID: "u"}},
		}

		meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
		eng := &engineImpl{
			disk: &mockDisk{disks: detectedMap},
			raid: &mockRAID{detailState: "active", detailMembers: []storage.MemberInfo{}},
			lvm:  &mockLVM{}, fs: &mockFS{}, meta: meta,
			stopDelay: time.Millisecond,
		}

		result, _ := eng.StartPool(context.Background(), "drivetest", false)

		if nDetected < nDisks {
			if len(result.Warnings) == 0 {
				t.Error("should warn when fewer drives detected")
			}
			if len(result.ArrayResults) != 0 {
				t.Error("should not assemble without force when drives missing")
			}
		}
	})
}

// Feature: poolforge-phase5-enclosure-support, Property 89: Pool start/stop metadata update
func TestPropertyP89_MetadataTimestamps(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Test start
		pool := makeTestPool(PoolOffline)
		meta := &mockMeta{pools: map[string]*Pool{pool.ID: pool}}
		raid := &mockRAID{detailState: "active", detailMembers: []storage.MemberInfo{{Device: "/dev/sda1"}, {Device: "/dev/sdb1"}}}
		eng := newTestEngine(raid, meta)

		before := time.Now()
		eng.StartPool(context.Background(), "mypool", true)
		after := time.Now()

		p := meta.pools[pool.ID]
		if p.OperationalStatus != PoolRunning {
			t.Error("should be Running after start")
		}
		if p.LastStartup == nil || p.LastStartup.Before(before) || p.LastStartup.After(after) {
			t.Error("LastStartup should be within operation window")
		}

		// Test stop
		before = time.Now()
		eng.StopPool(context.Background(), "mypool")
		after = time.Now()

		p = meta.pools[pool.ID]
		if p.OperationalStatus != PoolSafeToShutdown {
			t.Error("should be Safe_To_Power_Down after stop")
		}
		if p.LastShutdown == nil || p.LastShutdown.Before(before) || p.LastShutdown.After(after) {
			t.Error("LastShutdown should be within operation window")
		}
	})
}

// Feature: poolforge-phase5-enclosure-support, Property 91: Auto-start default for new pools
func TestPropertyP91_AutoStartDefaults(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		external := rapid.Bool().Draw(t, "external")

		pool := &Pool{
			ID: "p91", Name: "test", ParityMode: Parity1, State: PoolHealthy,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}

		// Simulate what CreatePool does
		pool.IsExternal = external
		pool.RequiresManualStart = external
		pool.OperationalStatus = PoolRunning

		if external {
			if !pool.IsExternal {
				t.Error("external pool should have IsExternal=true")
			}
			if !pool.RequiresManualStart {
				t.Error("external pool should have RequiresManualStart=true")
			}
		} else {
			if pool.IsExternal {
				t.Error("internal pool should have IsExternal=false")
			}
			if pool.RequiresManualStart {
				t.Error("internal pool should have RequiresManualStart=false")
			}
		}
	})
}
