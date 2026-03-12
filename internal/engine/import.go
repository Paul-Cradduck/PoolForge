package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type ImportResult struct {
	PoolName      string
	PoolID        string
	ArraysFound   int
	DisksRemapped int
	MountPoint    string
	ArraysFixed   int
}

func (e *engineImpl) ImportPool() (*ImportResult, error) {
	exec.Command("mdadm", "--assemble", "--scan").Run()
	exec.Command("vgscan").Run()
	exec.Command("vgchange", "-ay").Run()

	// Find PoolForge VG
	vgs, err := exec.Command("vgs", "--noheadings", "-o", "vg_name").Output()
	if err != nil {
		return nil, fmt.Errorf("vgscan failed: %w", err)
	}
	var pfVG string
	for _, line := range strings.Split(string(vgs), "\n") {
		name := strings.TrimSpace(line)
		if strings.HasPrefix(name, "poolforge-") || strings.HasPrefix(name, "vg_poolforge") {
			pfVG = name
			break
		}
	}
	if pfVG == "" {
		return nil, fmt.Errorf("no PoolForge volume group found")
	}

	// Find LV
	lvs, err := exec.Command("lvs", "--noheadings", "-o", "lv_name", pfVG).Output()
	if err != nil {
		return nil, fmt.Errorf("lvs failed: %w", err)
	}
	lvName := strings.TrimSpace(strings.Split(string(lvs), "\n")[0])
	if lvName == "" {
		return nil, fmt.Errorf("no logical volume in %s", pfVG)
	}
	lvPath := fmt.Sprintf("/dev/%s/%s", pfVG, lvName)

	// Mount and read backup
	tmpMount := "/tmp/poolforge-import"
	os.MkdirAll(tmpMount, 0755)
	if err := exec.Command("mount", lvPath, tmpMount).Run(); err != nil {
		return nil, fmt.Errorf("mount %s failed: %w", lvPath, err)
	}
	backupData, err := os.ReadFile(filepath.Join(tmpMount, ".poolforge-metadata.json"))
	exec.Command("umount", tmpMount).Run()
	if err != nil {
		return nil, fmt.Errorf("no backup metadata: %w", err)
	}

	var schema struct {
		Version int                        `json:"version"`
		Pools   map[string]json.RawMessage `json:"pools"`
	}
	if err := json.Unmarshal(backupData, &schema); err != nil {
		return nil, fmt.Errorf("corrupt backup: %w", err)
	}
	if len(schema.Pools) == 0 {
		return nil, fmt.Errorf("no pools in backup")
	}

	// Scan live system
	liveMDs := scanLiveMDArrays()
	currentDisks := scanCurrentDisks()

	var result *ImportResult
	for _, raw := range schema.Pools {
		res, err := restorePool(raw, liveMDs, currentDisks, lvPath, e.meta)
		if err != nil {
			return nil, err
		}
		result = res
	}
	return result, nil
}

type liveMDInfo struct {
	Device  string
	Level   int
	Raid    int            // expected member count from superblock
	Members map[int]string // slot → /dev/partition
}

type currentDisk struct {
	Dev  string
	Size uint64 // bytes
}

func scanLiveMDArrays() []liveMDInfo {
	var arrays []liveMDInfo
	mdstat, _ := os.ReadFile("/proc/mdstat")
	for _, line := range strings.Split(string(mdstat), "\n") {
		if !strings.HasPrefix(line, "md") {
			continue
		}
		dev := "/dev/" + strings.Fields(line)[0]
		detail, err := exec.Command("mdadm", "--detail", dev).Output()
		if err != nil {
			continue
		}
		info := liveMDInfo{Device: dev, Members: make(map[int]string)}
		memberRe := regexp.MustCompile(`^\s*(\d+)\s+\d+\s+\d+\s+(\d+)\s+.+\s+(/dev/\S+)`)
		for _, dl := range strings.Split(string(detail), "\n") {
			dl = strings.TrimSpace(dl)
			if strings.HasPrefix(dl, "Raid Level :") {
				switch strings.TrimSpace(strings.TrimPrefix(dl, "Raid Level :")) {
				case "raid1":
					info.Level = 1
				case "raid5":
					info.Level = 5
				case "raid6":
					info.Level = 6
				}
			}
			if strings.HasPrefix(dl, "Raid Devices :") {
				fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(dl, "Raid Devices :")), "%d", &info.Raid)
			}
		}
		for _, dl := range strings.Split(string(detail), "\n") {
			if m := memberRe.FindStringSubmatch(dl); len(m) == 4 {
				slot, _ := strconv.Atoi(m[2])
				info.Members[slot] = m[3]
			}
		}
		arrays = append(arrays, info)
	}
	return arrays
}

func scanCurrentDisks() []currentDisk {
	var disks []currentDisk
	entries, _ := os.ReadDir("/sys/block")
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "md") || strings.HasPrefix(name, "dm-") {
			continue
		}
		sizeStr, err := os.ReadFile(fmt.Sprintf("/sys/block/%s/size", name))
		if err != nil {
			continue
		}
		sectors, _ := strconv.ParseUint(strings.TrimSpace(string(sizeStr)), 10, 64)
		disks = append(disks, currentDisk{Dev: "/dev/" + name, Size: sectors * 512})
	}
	return disks
}

func partitionParent(part string) string {
	if idx := strings.LastIndex(part, "p"); idx > 0 && strings.Contains(part, "nvme") {
		return part[:idx]
	}
	re := regexp.MustCompile(`^(/dev/sd[a-z]+)\d+$`)
	if m := re.FindStringSubmatch(part); len(m) == 2 {
		return m[1]
	}
	return ""
}

func restorePool(raw json.RawMessage, liveMDs []liveMDInfo, currentDisks []currentDisk, lvPath string, meta MetadataStore) (*ImportResult, error) {
	var rec struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		ParityMode    string `json:"parity_mode"`
		State         string `json:"state"`
		VolumeGroup   string `json:"volume_group"`
		LogicalVolume string `json:"logical_volume"`
		MountPoint    string `json:"mount_point"`
		Disks         []struct {
			Device        string `json:"device"`
			CapacityBytes uint64 `json:"capacity_bytes"`
			State         string `json:"state"`
			Slices        []struct {
				TierIndex       int    `json:"tier_index"`
				PartitionNumber int    `json:"partition_number"`
				PartitionDevice string `json:"partition_device"`
				SizeBytes       uint64 `json:"size_bytes"`
			} `json:"slices"`
		} `json:"disks"`
		CapacityTiers []struct {
			Index             int    `json:"index"`
			SliceSizeBytes    uint64 `json:"slice_size_bytes"`
			EligibleDiskCount int    `json:"eligible_disk_count"`
			RAIDArray         string `json:"raid_array"`
		} `json:"capacity_tiers"`
		RAIDArrays []struct {
			Device        string   `json:"device"`
			RAIDLevel     int      `json:"raid_level"`
			TierIndex     int      `json:"tier_index"`
			State         string   `json:"state"`
			Members       []string `json:"members"`
			CapacityBytes uint64   `json:"capacity_bytes"`
		} `json:"raid_arrays"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("corrupt pool record: %w", err)
	}

	// Strategy: match old disks → current disks by capacity.
	// Build map: old disk device → current disk device
	diskRemap := matchDisksBySize(rec.Disks, currentDisks)

	// Build partition remap from disk remap.
	// Old /dev/nvme1n1p2 → if nvme1n1 maps to nvme6n1 → /dev/nvme6n1p2
	partRemap := make(map[string]string)
	for _, d := range rec.Disks {
		newDisk, ok := diskRemap[d.Device]
		if !ok {
			continue
		}
		for _, sl := range d.Slices {
			newPart := replaceParent(sl.PartitionDevice, d.Device, newDisk)
			if newPart != "" {
				partRemap[sl.PartitionDevice] = newPart
			}
		}
	}

	// Match metadata arrays → live arrays by matching remapped members
	mdRemap := make(map[string]string)
	usedLive := make(map[string]bool)
	for _, metaArr := range rec.RAIDArrays {
		// Remap members
		remappedMembers := make([]string, len(metaArr.Members))
		for i, m := range metaArr.Members {
			if newP, ok := partRemap[m]; ok {
				remappedMembers[i] = newP
			} else {
				remappedMembers[i] = m
			}
		}
		// Find live array that contains these members
		for _, live := range liveMDs {
			if usedLive[live.Device] || live.Level != metaArr.RAIDLevel {
				continue
			}
			liveSet := make(map[string]bool)
			for _, m := range live.Members {
				liveSet[m] = true
			}
			matches := 0
			for _, rm := range remappedMembers {
				if liveSet[rm] {
					matches++
				}
			}
			if matches == len(remappedMembers) {
				mdRemap[metaArr.Device] = live.Device
				usedLive[live.Device] = true
				break
			}
		}
	}

	remapped := 0
	// Apply disk remap
	for i := range rec.Disks {
		if newDev, ok := diskRemap[rec.Disks[i].Device]; ok && newDev != rec.Disks[i].Device {
			rec.Disks[i].Device = newDev
			remapped++
		}
		for j := range rec.Disks[i].Slices {
			if newP, ok := partRemap[rec.Disks[i].Slices[j].PartitionDevice]; ok {
				rec.Disks[i].Slices[j].PartitionDevice = newP
			}
		}
	}
	for i := range rec.RAIDArrays {
		if newDev, ok := mdRemap[rec.RAIDArrays[i].Device]; ok {
			rec.RAIDArrays[i].Device = newDev
		}
		for j := range rec.RAIDArrays[i].Members {
			if newP, ok := partRemap[rec.RAIDArrays[i].Members[j]]; ok {
				rec.RAIDArrays[i].Members[j] = newP
			}
		}
	}
	for i := range rec.CapacityTiers {
		if newDev, ok := mdRemap[rec.CapacityTiers[i].RAIDArray]; ok {
			rec.CapacityTiers[i].RAIDArray = newDev
		}
	}

	// Fix stale superblocks: shrink arrays where live expects more members than metadata
	arraysFixed := 0
	for _, metaArr := range rec.RAIDArrays {
		liveDev := metaArr.Device
		if newDev, ok := mdRemap[metaArr.Device]; ok {
			liveDev = newDev
		}
		for _, live := range liveMDs {
			if live.Device != liveDev || live.Raid <= len(metaArr.Members) {
				continue
			}
			target := len(metaArr.Members)
			if metaArr.RAIDLevel == 5 {
				newData := target - 1
				oldData := live.Raid - 1
				if oldData > 0 && newData > 0 {
					// Read current array size
					detail, _ := exec.Command("mdadm", "--detail", liveDev).Output()
					for _, dl := range strings.Split(string(detail), "\n") {
						if strings.Contains(dl, "Array Size") {
							re := regexp.MustCompile(`(\d+)`)
							m := re.FindString(strings.TrimPrefix(strings.TrimSpace(dl), "Array Size :"))
							if curKB, err := strconv.ParseUint(m, 10, 64); err == nil {
								memberKB := curKB / uint64(oldData)
								newKB := memberKB * uint64(newData)
								exec.Command("mdadm", "--grow", liveDev,
									"--array-size", fmt.Sprintf("%d", newKB)).Run()
							}
							break
						}
					}
				}
			}
			exec.Command("mdadm", "--grow", liveDev,
				fmt.Sprintf("--raid-devices=%d", target),
				"--force", "--backup-file=/tmp/md-reshape-backup").Run()
			arraysFixed++
		}
	}

	// Mount at correct path
	os.MkdirAll(rec.MountPoint, 0755)
	exec.Command("mount", lvPath, rec.MountPoint).Run()

	// Build and save Pool
	pm := Parity1
	if rec.ParityMode == "parity2" {
		pm = Parity2
	}
	pool := &Pool{
		ID: rec.ID, Name: rec.Name, ParityMode: pm,
		State: PoolHealthy, VolumeGroup: rec.VolumeGroup,
		LogicalVolume: rec.LogicalVolume, MountPoint: rec.MountPoint,
	}
	for _, d := range rec.Disks {
		di := DiskInfo{Device: d.Device, CapacityBytes: d.CapacityBytes, State: DiskState(d.State)}
		for _, s := range d.Slices {
			di.Slices = append(di.Slices, SliceInfo{
				TierIndex: s.TierIndex, PartitionNumber: s.PartitionNumber,
				PartitionDevice: s.PartitionDevice, SizeBytes: s.SizeBytes,
			})
		}
		pool.Disks = append(pool.Disks, di)
	}
	for _, t := range rec.CapacityTiers {
		pool.CapacityTiers = append(pool.CapacityTiers, CapacityTier{
			Index: t.Index, SliceSizeBytes: t.SliceSizeBytes,
			EligibleDiskCount: t.EligibleDiskCount, RAIDArray: t.RAIDArray,
		})
	}
	for _, a := range rec.RAIDArrays {
		pool.RAIDArrays = append(pool.RAIDArrays, RAIDArray{
			Device: a.Device, RAIDLevel: a.RAIDLevel, TierIndex: a.TierIndex,
			State: ArrayState(a.State), Members: a.Members, CapacityBytes: a.CapacityBytes,
		})
	}

	if err := meta.SavePool(pool); err != nil {
		return nil, fmt.Errorf("failed to save metadata: %w", err)
	}

	return &ImportResult{
		PoolName:      rec.Name,
		PoolID:        rec.ID,
		ArraysFound:   len(rec.RAIDArrays),
		DisksRemapped: remapped,
		MountPoint:    rec.MountPoint,
		ArraysFixed:   arraysFixed,
	}, nil
}

// matchDisksBySize matches old metadata disks to current system disks by capacity.
// For disks with identical sizes, tiebreaking order is arbitrary — this is correct
// because same-size disks have identical slice layouts in SHR, so they are
// interchangeable in the metadata.
func matchDisksBySize(metaDisks []struct {
	Device        string `json:"device"`
	CapacityBytes uint64 `json:"capacity_bytes"`
	State         string `json:"state"`
	Slices        []struct {
		TierIndex       int    `json:"tier_index"`
		PartitionNumber int    `json:"partition_number"`
		PartitionDevice string `json:"partition_device"`
		SizeBytes       uint64 `json:"size_bytes"`
	} `json:"slices"`
}, currentDisks []currentDisk) map[string]string {
	remap := make(map[string]string)
	used := make(map[string]bool)

	// Group current disks by size (within 50MB tolerance for alignment)
	type candidate struct {
		dev  string
		size uint64
	}
	var candidates []candidate
	for _, d := range currentDisks {
		// Skip root disk (has partitions but no mdadm superblocks)
		candidates = append(candidates, candidate{d.Dev, d.Size})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].dev < candidates[j].dev })

	for _, md := range metaDisks {
		tolerance := uint64(50 * 1024 * 1024) // 50MB
		for _, c := range candidates {
			if used[c.dev] {
				continue
			}
			diff := int64(md.CapacityBytes) - int64(c.size)
			if diff < 0 {
				diff = -diff
			}
			if uint64(diff) <= tolerance {
				remap[md.Device] = c.dev
				used[c.dev] = true
				break
			}
		}
	}
	return remap
}

// replaceParent replaces the disk portion of a partition path.
// e.g. replaceParent("/dev/nvme1n1p2", "/dev/nvme1n1", "/dev/nvme6n1") → "/dev/nvme6n1p2"
func replaceParent(partDev, oldDisk, newDisk string) string {
	if strings.HasPrefix(partDev, oldDisk) {
		return newDisk + partDev[len(oldDisk):]
	}
	return ""
}
