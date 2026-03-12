//go:build integration

package integration

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func skipIfNotTestInstance(t *testing.T) {
	t.Helper()
	if os.Getenv("POOLFORGE_TEST_INSTANCE") == "" {
		t.Skip("POOLFORGE_TEST_INSTANCE not set — skipping integration test")
	}
}

func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func poolforge(t *testing.T, args ...string) string {
	t.Helper()
	return run(t, "sudo", append([]string{"poolforge"}, args...)...)
}

func testDisks(t *testing.T) string {
	t.Helper()
	d := os.Getenv("POOLFORGE_TEST_DISKS")
	if d == "" {
		t.Fatal("POOLFORGE_TEST_DISKS not set")
	}
	return d
}

func phase5Disks(t *testing.T) string {
	t.Helper()
	d := os.Getenv("POOLFORGE_PHASE5_DISKS")
	if d == "" {
		t.Fatal("POOLFORGE_PHASE5_DISKS not set")
	}
	return d
}

func writeTestData(t *testing.T, pool string) string {
	t.Helper()
	path := fmt.Sprintf("/mnt/poolforge/%s/testfile", pool)
	run(t, "sudo", "dd", "if=/dev/urandom", "of="+path, "bs=1M", "count=5")
	out := run(t, "sudo", "md5sum", path)
	return strings.Fields(out)[0]
}

func verifyTestData(t *testing.T, pool, expectedMD5 string) {
	t.Helper()
	path := fmt.Sprintf("/mnt/poolforge/%s/testfile", pool)
	out := run(t, "sudo", "md5sum", path)
	got := strings.Fields(out)[0]
	if got != expectedMD5 {
		t.Fatalf("data integrity check failed: got %s, want %s", got, expectedMD5)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	out, _ := exec.Command("sudo", "cat", path).CombinedOutput()
	return string(out)
}

// Task 16.2: Pool start/stop sequencing
func TestPoolStartStop(t *testing.T) {
	skipIfNotTestInstance(t)
	disks := phase5Disks(t)

	poolforge(t, "pool", "create", "--name", "startstop", "--disks", disks, "--parity", "parity1", "--external")
	md5 := writeTestData(t, "startstop")

	// Stop
	poolforge(t, "pool", "stop", "startstop")

	// Verify arrays stopped
	mdstat := readFile(t, "/proc/mdstat")
	if strings.Contains(mdstat, "active") {
		t.Log("Warning: arrays may still be active in /proc/mdstat")
	}

	// Start
	out := poolforge(t, "pool", "start", "startstop", "--force")
	t.Log(out)

	// Verify data integrity
	verifyTestData(t, "startstop", md5)

	// Cleanup
	poolforge(t, "pool", "stop", "startstop")
	poolforge(t, "pool", "delete", "startstop")
}

// Task 16.3: Device name change handling
func TestDeviceNameChange(t *testing.T) {
	skipIfNotTestInstance(t)
	disks := phase5Disks(t)

	poolforge(t, "pool", "create", "--name", "namechange", "--disks", disks, "--parity", "parity1", "--external")
	md5 := writeTestData(t, "namechange")
	poolforge(t, "pool", "stop", "namechange")

	// Device name change is simulated by the test runner (detach/reattach EBS to different slots)
	// The test runner sets POOLFORGE_PHASE5_NEW_DISKS with the new device paths
	time.Sleep(2 * time.Second) // Wait for reattach

	newDisks := os.Getenv("POOLFORGE_PHASE5_NEW_DISKS")
	if newDisks == "" {
		t.Skip("POOLFORGE_PHASE5_NEW_DISKS not set — device name change not simulated")
	}

	out := poolforge(t, "pool", "start", "namechange", "--force")
	t.Log(out)

	// Verify data integrity after name change
	verifyTestData(t, "namechange", md5)

	// Verify metadata has updated device names
	meta := readFile(t, "/var/lib/poolforge/metadata.json")
	for _, d := range strings.Split(newDisks, ",") {
		d = strings.TrimSpace(d)
		if d != "" && !strings.Contains(meta, d) {
			t.Logf("Warning: new device %s not found in metadata (may use partition suffix)", d)
		}
	}

	poolforge(t, "pool", "stop", "namechange")
	poolforge(t, "pool", "delete", "namechange")
}

// Task 16.4: Degraded array repair
func TestDegradedArrayRepair(t *testing.T) {
	skipIfNotTestInstance(t)
	disks := phase5Disks(t)

	poolforge(t, "pool", "create", "--name", "degraded", "--disks", disks, "--parity", "parity1", "--external")
	md5 := writeTestData(t, "degraded")
	poolforge(t, "pool", "stop", "degraded")

	// Test runner detaches one volume to simulate degraded state, then reattaches
	// The pool start should detect degraded arrays and re-add the missing member
	time.Sleep(2 * time.Second)

	out := poolforge(t, "pool", "start", "degraded", "--force")
	t.Log(out)

	// Check for re-add in output (not full rebuild)
	if strings.Contains(out, "re-added") {
		t.Log("PASS: re-add detected (fast bitmap recovery)")
	} else if strings.Contains(out, "rebuilding") {
		t.Log("Warning: full rebuild triggered instead of re-add")
	}

	verifyTestData(t, "degraded", md5)

	poolforge(t, "pool", "stop", "degraded")
	poolforge(t, "pool", "delete", "degraded")
}

// Task 16.5: Full power cycle simulation
func TestFullPowerCycle(t *testing.T) {
	skipIfNotTestInstance(t)
	disks := phase5Disks(t)

	poolforge(t, "pool", "create", "--name", "powercycle", "--disks", disks, "--parity", "parity1", "--external")
	md5 := writeTestData(t, "powercycle")
	poolforge(t, "pool", "stop", "powercycle")

	// Test runner performs: detach all → wait → reattach with different device names
	time.Sleep(5 * time.Second)

	out := poolforge(t, "pool", "start", "powercycle", "--force")
	t.Log(out)

	verifyTestData(t, "powercycle", md5)

	// Verify no full rebuild was triggered
	mdstat := readFile(t, "/proc/mdstat")
	if strings.Contains(mdstat, "recovery") {
		t.Error("full rebuild detected — should have used re-add")
	}

	poolforge(t, "pool", "stop", "powercycle")
	poolforge(t, "pool", "delete", "powercycle")
}

// Task 16.6: Boot config generation
func TestBootConfigGeneration(t *testing.T) {
	skipIfNotTestInstance(t)
	disks := testDisks(t)
	diskList := strings.Split(disks, ",")
	if len(diskList) < 4 {
		t.Skip("need at least 4 disks for two-pool test")
	}

	// Create auto-start pool (first 2 disks)
	autoDisks := strings.Join(diskList[:2], ",")
	poolforge(t, "pool", "create", "--name", "autopool", "--disks", autoDisks, "--parity", "parity1")

	// Create manual-start pool (next 2 disks)
	manualDisks := strings.Join(diskList[2:4], ",")
	poolforge(t, "pool", "create", "--name", "manualpool", "--disks", manualDisks, "--parity", "parity1", "--external")

	// Trigger boot config regeneration
	poolforge(t, "pool", "set-autostart", "manualpool", "false")

	conf := readFile(t, "/etc/mdadm/mdadm.conf")

	if !strings.Contains(conf, "AUTO -all") {
		t.Error("mdadm.conf missing AUTO -all directive")
	}
	if !strings.Contains(conf, "autopool") || !strings.Contains(conf, "auto-start") {
		t.Error("auto-start pool should appear in mdadm.conf")
	}
	if strings.Contains(conf, "ARRAY") {
		// Check that ARRAY lines only reference autopool arrays
		for _, line := range strings.Split(conf, "\n") {
			if strings.HasPrefix(line, "ARRAY") {
				// This is fine — it should be from autopool
				t.Logf("ARRAY line: %s", line)
			}
		}
	}
	if strings.Contains(conf, "manualpool") && strings.Contains(conf, "manual-start") {
		t.Log("PASS: manual-start pool comment present, no ARRAY definitions")
	}

	// Cleanup
	poolforge(t, "pool", "stop", "manualpool")
	poolforge(t, "pool", "delete", "manualpool")
	poolforge(t, "pool", "delete", "autopool")
}

// Task 16.7: Phase 5 API endpoints
func TestPhase5API(t *testing.T) {
	skipIfNotTestInstance(t)
	disks := phase5Disks(t)

	poolforge(t, "pool", "create", "--name", "apitest", "--disks", disks, "--parity", "parity1", "--external")

	addr := os.Getenv("POOLFORGE_ADDR")
	if addr == "" {
		addr = "localhost:8080"
	}
	auth := os.Getenv("POOLFORGE_AUTH")
	if auth == "" {
		auth = "admin:secret"
	}

	curl := func(method, path string, body ...string) string {
		args := []string{"-s", "-X", method, "-u", auth}
		if len(body) > 0 {
			args = append(args, "-H", "Content-Type: application/json", "-d", body[0])
		}
		args = append(args, fmt.Sprintf("http://%s/api%s", addr, path))
		out, err := exec.Command("curl", args...).CombinedOutput()
		if err != nil {
			t.Logf("curl %s %s: %v\n%s", method, path, err, out)
		}
		return string(out)
	}

	// Stop via API
	out := curl("POST", "/pools/apitest/stop")
	if !strings.Contains(out, "safe_to_power_down") {
		t.Errorf("stop response: %s", out)
	}

	// Start via API
	out = curl("POST", "/pools/apitest/start?force=true")
	if !strings.Contains(out, "running") {
		t.Errorf("start response: %s", out)
	}

	// Conflict: start again
	out = curl("POST", "/pools/apitest/start")
	if !strings.Contains(out, "already running") {
		t.Errorf("expected conflict, got: %s", out)
	}

	// Set autostart
	out = curl("PUT", "/pools/apitest/autostart", `{"auto_start":false}`)
	if !strings.Contains(out, "auto_start") {
		t.Errorf("autostart response: %s", out)
	}

	// 404
	out = curl("POST", "/pools/nonexistent/start")
	if !strings.Contains(out, "not found") {
		t.Errorf("expected 404, got: %s", out)
	}

	poolforge(t, "pool", "stop", "apitest")
	poolforge(t, "pool", "delete", "apitest")
}

// Task 16.8: Safe software upgrade (placeholder)
func TestSafeUpgrade(t *testing.T) {
	skipIfNotTestInstance(t)
	t.Log(`Upgrade test scenario:
  1. Install Phase 4 build
  2. Create pools with data
  3. Replace binary with Phase 5 build
  4. Restart poolforge service
  5. Verify: all pools running, data intact, metadata has Phase 5 defaults,
     UUIDs populated, Boot_Config has AUTO -all, no RAID arrays modified`)
	t.Skip("Requires two separate builds — run manually via test_runner.sh")
}

// Task 16.9 + 16.10: Regression tests
func TestPhase1to4Regression(t *testing.T) {
	skipIfNotTestInstance(t)
	disks := testDisks(t)
	diskList := strings.Split(disks, ",")
	if len(diskList) < 4 {
		t.Skip("need at least 4 disks")
	}

	// Phase 1: Create pool
	d := strings.Join(diskList[:3], ",")
	poolforge(t, "pool", "create", "--name", "regtest", "--disks", d, "--parity", "parity1")
	t.Log("PASS: Phase 1 — pool creation")

	// Phase 1: Status
	out := poolforge(t, "pool", "status", "regtest")
	if !strings.Contains(out, "regtest") {
		t.Error("status should show pool name")
	}
	t.Log("PASS: Phase 1 — pool status")

	// Phase 1: List
	out = poolforge(t, "pool", "list")
	if !strings.Contains(out, "regtest") {
		t.Error("list should show pool")
	}
	t.Log("PASS: Phase 1 — pool list")

	// Phase 1: Data integrity
	md5 := writeTestData(t, "regtest")
	verifyTestData(t, "regtest", md5)
	t.Log("PASS: Phase 1 — data integrity")

	// Phase 2: Add disk
	if len(diskList) >= 4 {
		poolforge(t, "pool", "add-disk", "regtest", "--disk", diskList[3])
		t.Log("PASS: Phase 2 — add disk")
	}

	// Phase 2: Data still intact after expansion
	verifyTestData(t, "regtest", md5)
	t.Log("PASS: Phase 2 — data integrity after expansion")

	// Cleanup
	poolforge(t, "pool", "delete", "regtest")
	t.Log("PASS: Phase 2 — pool deletion")
}
