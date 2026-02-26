package storage

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type lvmManager struct{}

func NewLVMManager() LVMManager { return &lvmManager{} }

func (l *lvmManager) CreatePhysicalVolume(device string) error {
	if out, err := exec.Command("pvcreate", "-ff", "-y", device).CombinedOutput(); err != nil {
		return fmt.Errorf("pvcreate %s: %w\n%s", device, err, out)
	}
	return nil
}

func (l *lvmManager) CreateVolumeGroup(name string, pvDevices []string) error {
	args := append([]string{name}, pvDevices...)
	if out, err := exec.Command("vgcreate", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("vgcreate %s: %w\n%s", name, err, out)
	}
	return nil
}

func (l *lvmManager) CreateLogicalVolume(vgName, lvName string, sizePercent int) error {
	arg := fmt.Sprintf("%d%%FREE", sizePercent)
	if out, err := exec.Command("lvcreate", "-y", "-l", arg, "-n", lvName, vgName).CombinedOutput(); err != nil {
		return fmt.Errorf("lvcreate %s/%s: %w\n%s", vgName, lvName, err, out)
	}
	return nil
}

func (l *lvmManager) GetVolumeGroupInfo(name string) (*VGInfo, error) {
	out, err := exec.Command("vgs", "--noheadings", "--nosuffix", "--units", "b",
		"-o", "vg_name,vg_size,vg_free,pv_count,lv_count", name).Output()
	if err != nil {
		return nil, fmt.Errorf("vgs %s: %w", name, err)
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 5 {
		return nil, fmt.Errorf("unexpected vgs output for %s", name)
	}
	size, _ := strconv.ParseUint(fields[1], 10, 64)
	free, _ := strconv.ParseUint(fields[2], 10, 64)
	pvCount, _ := strconv.Atoi(fields[3])
	lvCount, _ := strconv.Atoi(fields[4])
	return &VGInfo{
		Name:      fields[0],
		SizeBytes: size,
		FreeBytes: free,
		PVCount:   pvCount,
		LVCount:   lvCount,
	}, nil
}

func (l *lvmManager) ExtendVolumeGroup(name string, pvDevice string) error {
	if out, err := exec.Command("vgextend", name, pvDevice).CombinedOutput(); err != nil {
		return fmt.Errorf("vgextend %s: %w\n%s", name, err, out)
	}
	return nil
}

func (l *lvmManager) ExtendLogicalVolume(lvPath string) error {
	if out, err := exec.Command("lvextend", "-l", "+100%FREE", lvPath).CombinedOutput(); err != nil {
		return fmt.Errorf("lvextend %s: %w\n%s", lvPath, err, out)
	}
	return nil
}

func (l *lvmManager) RemoveLogicalVolume(lvPath string) error {
	if out, err := exec.Command("lvremove", "-f", lvPath).CombinedOutput(); err != nil {
		return fmt.Errorf("lvremove %s: %w\n%s", lvPath, err, out)
	}
	return nil
}

func (l *lvmManager) RemoveVolumeGroup(name string) error {
	if out, err := exec.Command("vgremove", "-f", name).CombinedOutput(); err != nil {
		return fmt.Errorf("vgremove %s: %w\n%s", name, err, out)
	}
	return nil
}

func (l *lvmManager) RemovePhysicalVolume(device string) error {
	if out, err := exec.Command("pvremove", "-ff", "-y", device).CombinedOutput(); err != nil {
		return fmt.Errorf("pvremove %s: %w\n%s", device, err, out)
	}
	return nil
}

func (l *lvmManager) CheckPhysicalVolume(device string) bool {
	return exec.Command("pvs", device).Run() == nil
}

// RestoreMissingPV recreates a PV whose header was destroyed by mdadm reshape.
// It reads the expected UUID from the VG backup, recreates the PV, and restores VG metadata.
func (l *lvmManager) RestoreMissingPV(vgName string, device string) error {
	// Parse the missing PV UUID from vgs output
	out, err := exec.Command("vgs", "--noheadings", "-o", "pv_name,pv_uuid", vgName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("vgs: %w\n%s", err, out)
	}
	var missingUUID string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && (fields[0] == "[unknown]" || fields[0] == "unknown") {
			missingUUID = fields[1]
			break
		}
	}
	if missingUUID == "" {
		return fmt.Errorf("no missing PV found in VG %s", vgName)
	}

	restoreFile := fmt.Sprintf("/etc/lvm/backup/%s", vgName)
	if out, err := exec.Command("pvcreate", "--uuid", missingUUID,
		"--restorefile", restoreFile, "-ff", "-y", device).CombinedOutput(); err != nil {
		return fmt.Errorf("pvcreate restore %s: %w\n%s", device, err, out)
	}
	if out, err := exec.Command("vgcfgrestore", vgName).CombinedOutput(); err != nil {
		return fmt.Errorf("vgcfgrestore %s: %w\n%s", vgName, err, out)
	}
	if out, err := exec.Command("vgchange", "-ay", vgName).CombinedOutput(); err != nil {
		return fmt.Errorf("vgchange %s: %w\n%s", vgName, err, out)
	}
	return nil
}
