package snapshots

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Create creates an LVM snapshot of the pool's logical volume.
func Create(vgName, lvName, snapName string) error {
	originLV := fmt.Sprintf("/dev/%s/%s", vgName, lvName)
	return exec.Command("lvcreate", "--snapshot", "-n", snapName, "-l", "50%FREE", originLV).Run()
}

// Delete removes a snapshot LV after unmounting.
func Delete(vgName, snapName, mountPath string) error {
	if mountPath != "" {
		exec.Command("umount", mountPath).Run()
		os.RemoveAll(mountPath)
	}
	return exec.Command("lvremove", "-f", fmt.Sprintf("/dev/%s/%s", vgName, snapName)).Run()
}

// Mount mounts a snapshot read-only at the given path.
func Mount(vgName, snapName, mountPath string) error {
	os.MkdirAll(mountPath, 0755)
	return exec.Command("mount", "-o", "ro", fmt.Sprintf("/dev/%s/%s", vgName, snapName), mountPath).Run()
}

// List returns snapshot info from lvs output.
func List(vgName string) ([]SnapInfo, error) {
	out, err := exec.Command("lvs", "--noheadings", "-o", "lv_name,lv_size,origin", "--units", "b", vgName).Output()
	if err != nil {
		return nil, err
	}
	var snaps []SnapInfo
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[2] == "" {
			continue
		}
		// Only snapshots (have an origin)
		sizeStr := strings.TrimSuffix(fields[1], "B")
		size, _ := strconv.ParseFloat(sizeStr, 64)
		snaps = append(snaps, SnapInfo{Name: fields[0], SizeBytes: uint64(size), Origin: fields[2]})
	}
	return snaps, nil
}

// SnapInfo is raw LVM snapshot info.
type SnapInfo struct {
	Name      string
	SizeBytes uint64
	Origin    string
}

// SnapMountPath returns the mount path for a snapshot.
func SnapMountPath(poolMount, snapName string) string {
	return filepath.Join(poolMount, ".snapshots", snapName)
}

// GenerateName creates a timestamp-based snapshot name.
func GenerateName() string {
	return "snap_" + time.Now().Format("20060102_150405")
}

// SpaceUsed returns the percentage of snapshot reserve space consumed.
func SpaceUsed(vgName string) (float64, error) {
	out, err := exec.Command("vgs", "--noheadings", "-o", "vg_size,vg_free", "--units", "b", vgName).Output()
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return 0, fmt.Errorf("unexpected vgs output")
	}
	total, _ := strconv.ParseFloat(strings.TrimSuffix(fields[0], "B"), 64)
	free, _ := strconv.ParseFloat(strings.TrimSuffix(fields[1], "B"), 64)
	if total == 0 {
		return 0, nil
	}
	return (1 - free/total) * 100, nil
}
