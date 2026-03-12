package safety

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func genPoolBootInfo(t *rapid.T, idx int) PoolBootInfo {
	manual := rapid.Bool().Draw(t, "manual")
	nArrays := rapid.IntRange(1, 4).Draw(t, "nArrays")
	var arrays []ArrayBootInfo
	for i := 0; i < nArrays; i++ {
		arrays = append(arrays, ArrayBootInfo{
			Device: rapid.StringMatching(`/dev/md\d+`).Draw(t, "dev"),
			UUID:   rapid.StringMatching(`[0-9a-f]{8}:[0-9a-f]{8}:[0-9a-f]{8}:[0-9a-f]{8}`).Draw(t, "uuid"),
		})
	}
	return PoolBootInfo{
		PoolName:            rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "name"),
		RequiresManualStart: manual,
		Arrays:              arrays,
	}
}

// Feature: poolforge-phase5-enclosure-support, Property 77: Boot_Config ARRAY definitions match auto-start pools only
func TestPropertyP77_ArrayDefsMatchAutoStartOnly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		nPools := rapid.IntRange(0, 5).Draw(t, "nPools")
		var pools []PoolBootInfo
		for i := 0; i < nPools; i++ {
			pools = append(pools, genPoolBootInfo(t, i))
		}

		content := RenderBootConfig(pools)

		// AUTO -all must always be present
		if !strings.Contains(content, "AUTO -all") {
			t.Error("missing AUTO -all")
		}

		for _, p := range pools {
			for _, a := range p.Arrays {
				arrayLine := "ARRAY " + a.Device + " metadata=1.2 UUID=" + a.UUID
				hasLine := strings.Contains(content, arrayLine)
				if p.RequiresManualStart && hasLine {
					t.Errorf("manual-start pool %q should not have ARRAY def for %s", p.PoolName, a.Device)
				}
				if !p.RequiresManualStart && a.UUID != "" && !hasLine {
					t.Errorf("auto-start pool %q should have ARRAY def for %s", p.PoolName, a.Device)
				}
			}
		}
	})
}

// Feature: poolforge-phase5-enclosure-support, Property 78: Boot_Config generation is idempotent
func TestPropertyP78_BootConfigIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		nPools := rapid.IntRange(0, 5).Draw(t, "nPools")
		var pools []PoolBootInfo
		for i := 0; i < nPools; i++ {
			pools = append(pools, genPoolBootInfo(t, i))
		}

		content1 := RenderBootConfig(pools)
		content2 := RenderBootConfig(pools)

		if content1 != content2 {
			t.Error("generating boot config twice should produce identical output")
		}
	})
}

// Feature: poolforge-phase5-enclosure-support, Property 79: Boot_Config consistency after mutation
func TestPropertyP79_BootConfigConsistencyAfterMutation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := genPoolBootInfo(t, 0)

		// Mutation: toggle RequiresManualStart
		p.RequiresManualStart = false
		contentAuto := RenderBootConfig([]PoolBootInfo{p})
		for _, a := range p.Arrays {
			if !strings.Contains(contentAuto, "UUID="+a.UUID) {
				t.Error("auto-start pool should have ARRAY defs after set-autostart true")
			}
		}

		p.RequiresManualStart = true
		contentManual := RenderBootConfig([]PoolBootInfo{p})
		for _, a := range p.Arrays {
			if strings.Contains(contentManual, "ARRAY "+a.Device+" ") {
				t.Error("manual-start pool should NOT have ARRAY defs after set-autostart false")
			}
		}

		// Mutation: delete pool
		contentEmpty := RenderBootConfig(nil)
		if strings.Contains(contentEmpty, "ARRAY /dev") {
			t.Error("deleted pool should have no ARRAY defs")
		}
	})
}
