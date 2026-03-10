package safety

import (
	"strings"
	"testing"
)

func TestBootConfigZeroPools(t *testing.T) {
	content := RenderBootConfig(nil)
	if !strings.Contains(content, "AUTO -all") {
		t.Error("missing AUTO -all")
	}
	if strings.Contains(content, "ARRAY /dev") {
		t.Error("should have no ARRAY definitions with zero pools")
	}
}

func TestBootConfigOneAutoStartPool(t *testing.T) {
	pools := []PoolBootInfo{{
		PoolName: "internal", RequiresManualStart: false,
		Arrays: []ArrayBootInfo{
			{Device: "/dev/md0", UUID: "aaa:bbb:ccc:ddd"},
			{Device: "/dev/md1", UUID: "eee:fff:111:222"},
		},
	}}
	content := RenderBootConfig(pools)
	if !strings.Contains(content, "AUTO -all") {
		t.Error("missing AUTO -all")
	}
	if !strings.Contains(content, "ARRAY /dev/md0 metadata=1.2 UUID=aaa:bbb:ccc:ddd") {
		t.Error("missing ARRAY for md0")
	}
	if !strings.Contains(content, "ARRAY /dev/md1 metadata=1.2 UUID=eee:fff:111:222") {
		t.Error("missing ARRAY for md1")
	}
	if !strings.Contains(content, "auto-start") {
		t.Error("missing auto-start comment")
	}
}

func TestBootConfigOneManualStartPool(t *testing.T) {
	pools := []PoolBootInfo{{
		PoolName: "external", RequiresManualStart: true,
		Arrays: []ArrayBootInfo{
			{Device: "/dev/md0", UUID: "aaa:bbb:ccc:ddd"},
		},
	}}
	content := RenderBootConfig(pools)
	if !strings.Contains(content, "AUTO -all") {
		t.Error("missing AUTO -all")
	}
	if strings.Contains(content, "ARRAY /dev") {
		t.Error("manual-start pool should have no ARRAY definitions")
	}
	if !strings.Contains(content, "manual-start") {
		t.Error("missing manual-start comment")
	}
}

func TestBootConfigMixedPools(t *testing.T) {
	pools := []PoolBootInfo{
		{PoolName: "internal", RequiresManualStart: false,
			Arrays: []ArrayBootInfo{{Device: "/dev/md0", UUID: "int-uuid"}}},
		{PoolName: "external", RequiresManualStart: true,
			Arrays: []ArrayBootInfo{{Device: "/dev/md1", UUID: "ext-uuid"}}},
	}
	content := RenderBootConfig(pools)
	if !strings.Contains(content, "ARRAY /dev/md0 metadata=1.2 UUID=int-uuid") {
		t.Error("auto-start pool should have ARRAY definition")
	}
	if strings.Contains(content, "ARRAY /dev/md1") {
		t.Error("manual-start pool should NOT have ARRAY definition")
	}
}

func TestBootConfigEmptyUUIDSkipped(t *testing.T) {
	pools := []PoolBootInfo{{
		PoolName: "pool", RequiresManualStart: false,
		Arrays: []ArrayBootInfo{{Device: "/dev/md0", UUID: ""}},
	}}
	content := RenderBootConfig(pools)
	if strings.Contains(content, "ARRAY /dev") {
		t.Error("empty UUID should be skipped")
	}
}

func TestBootConfigAutoDirectiveBeforeArrays(t *testing.T) {
	pools := []PoolBootInfo{{
		PoolName: "pool", RequiresManualStart: false,
		Arrays: []ArrayBootInfo{{Device: "/dev/md0", UUID: "test-uuid"}},
	}}
	content := RenderBootConfig(pools)
	autoIdx := strings.Index(content, "AUTO -all")
	arrayIdx := strings.Index(content, "ARRAY")
	if autoIdx < 0 || arrayIdx < 0 {
		t.Fatal("missing AUTO or ARRAY")
	}
	if autoIdx >= arrayIdx {
		t.Error("AUTO -all must appear before ARRAY definitions")
	}
}
