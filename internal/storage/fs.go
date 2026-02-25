package storage

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

type fsManager struct{}

func NewFilesystemManager() FilesystemManager { return &fsManager{} }

func (f *fsManager) CreateFilesystem(device string) error {
	if out, err := exec.Command("mkfs.ext4", "-F", device).CombinedOutput(); err != nil {
		return fmt.Errorf("mkfs.ext4 %s: %w\n%s", device, err, out)
	}
	return nil
}

func (f *fsManager) MountFilesystem(device, mountPoint string) error {
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", mountPoint, err)
	}
	if out, err := exec.Command("mount", device, mountPoint).CombinedOutput(); err != nil {
		return fmt.Errorf("mount %s on %s: %w\n%s", device, mountPoint, err, out)
	}
	return nil
}

func (f *fsManager) UnmountFilesystem(mountPoint string) error {
	if out, err := exec.Command("umount", mountPoint).CombinedOutput(); err != nil {
		return fmt.Errorf("umount %s: %w\n%s", mountPoint, err, out)
	}
	return nil
}

func (f *fsManager) GetUsage(mountPoint string) (*FSUsage, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(mountPoint, &stat); err != nil {
		// Fallback to df
		return f.getUsageDF(mountPoint)
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	return &FSUsage{TotalBytes: total, UsedBytes: total - free, FreeBytes: free}, nil
}

func (f *fsManager) getUsageDF(mountPoint string) (*FSUsage, error) {
	out, err := exec.Command("df", "--output=size,used,avail", "-B1", mountPoint).Output()
	if err != nil {
		return nil, fmt.Errorf("df %s: %w", mountPoint, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("unexpected df output for %s", mountPoint)
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 3 {
		return nil, fmt.Errorf("unexpected df fields for %s", mountPoint)
	}
	total, _ := strconv.ParseUint(fields[0], 10, 64)
	used, _ := strconv.ParseUint(fields[1], 10, 64)
	free, _ := strconv.ParseUint(fields[2], 10, 64)
	return &FSUsage{TotalBytes: total, UsedBytes: used, FreeBytes: free}, nil
}
