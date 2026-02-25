package storage

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type diskManager struct{}

func NewDiskManager() DiskManager { return &diskManager{} }

func (d *diskManager) GetDiskInfo(device string) (*DiskInfoResult, error) {
	out, err := exec.Command("blockdev", "--getsize64", device).Output()
	if err != nil {
		return nil, fmt.Errorf("blockdev %s: %w", device, err)
	}
	size, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse size for %s: %w", device, err)
	}
	return &DiskInfoResult{Device: device, CapacityBytes: size}, nil
}

func (d *diskManager) CreateGPTPartitionTable(device string) error {
	if err := exec.Command("sgdisk", "--zap-all", device).Run(); err != nil {
		return fmt.Errorf("sgdisk zap %s: %w", device, err)
	}
	if err := exec.Command("sgdisk", "--clear", device).Run(); err != nil {
		return fmt.Errorf("sgdisk clear %s: %w", device, err)
	}
	return nil
}

func (d *diskManager) CreatePartition(device string, start, size uint64) (*Partition, error) {
	// Convert bytes to 512-byte sectors
	startSector := start / 512
	endSector := (start+size)/512 - 1

	// Find next partition number
	parts, _ := d.ListPartitions(device)
	num := len(parts) + 1

	arg := fmt.Sprintf("%d:%d:%d", num, startSector, endSector)
	if err := exec.Command("sgdisk", "--new", arg, device).Run(); err != nil {
		return nil, fmt.Errorf("sgdisk new partition %s: %w", device, err)
	}

	partDev := partitionDevice(device, num)
	return &Partition{Number: num, Device: partDev, Start: start, Size: size}, nil
}

func (d *diskManager) ListPartitions(device string) ([]Partition, error) {
	out, err := exec.Command("sgdisk", "--print", device).Output()
	if err != nil {
		return nil, fmt.Errorf("sgdisk print %s: %w", device, err)
	}
	var parts []Partition
	inTable := false
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Number") {
			inTable = true
			continue
		}
		if !inTable {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		num, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		startSec, _ := strconv.ParseUint(fields[1], 10, 64)
		endSec, _ := strconv.ParseUint(fields[2], 10, 64)
		size := (endSec - startSec + 1) * 512
		parts = append(parts, Partition{
			Number: num,
			Device: partitionDevice(device, num),
			Start:  startSec * 512,
			Size:   size,
		})
	}
	return parts, nil
}

func partitionDevice(device string, num int) string {
	// /dev/sdb -> /dev/sdb1, /dev/nvme0n1 -> /dev/nvme0n1p1
	if strings.Contains(device, "nvme") || strings.Contains(device, "loop") {
		return fmt.Sprintf("%sp%d", device, num)
	}
	return fmt.Sprintf("%s%d", device, num)
}
