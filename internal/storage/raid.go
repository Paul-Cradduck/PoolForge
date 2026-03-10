package storage

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type raidManager struct{}

func NewRAIDManager() RAIDManager { return &raidManager{} }

func (r *raidManager) CreateArray(opts RAIDCreateOpts) (*RAIDArrayInfo, error) {
	dev := "/dev/" + opts.Name
	metaVer := opts.MetadataVersion
	if metaVer == "" {
		metaVer = "1.2"
	}

	// Wipe any stale signatures on member devices to prevent "Device or resource busy"
	for _, m := range opts.Members {
		exec.Command("wipefs", "-af", m).Run()
	}
	exec.Command("udevadm", "settle").Run()

	args := []string{
		"--create", dev,
		"--level", strconv.Itoa(opts.Level),
		"--raid-devices", strconv.Itoa(len(opts.Members)),
		"--metadata", metaVer,
		"--run",
	}
	args = append(args, opts.Members...)

	cmd := exec.Command("mdadm", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("mdadm create %s: %w\n%s", dev, err, out)
	}
	return &RAIDArrayInfo{Device: dev, Level: opts.Level, Members: opts.Members, State: "active"}, nil
}

func (r *raidManager) GetArrayDetail(device string) (*RAIDArrayDetail, error) {
	out, err := exec.Command("mdadm", "--detail", device).Output()
	if err != nil {
		return nil, fmt.Errorf("mdadm detail %s: %w", device, err)
	}
	detail := &RAIDArrayDetail{Device: device}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Raid Level :") {
			lvl := strings.TrimSpace(strings.TrimPrefix(line, "Raid Level :"))
			switch lvl {
			case "raid1":
				detail.Level = 1
			case "raid5":
				detail.Level = 5
			case "raid6":
				detail.Level = 6
			}
		}
		if strings.HasPrefix(line, "State :") {
			detail.State = strings.TrimSpace(strings.TrimPrefix(line, "State :"))
		}
		if strings.HasPrefix(line, "Array Size :") {
			re := regexp.MustCompile(`(\d+)`)
			m := re.FindString(strings.TrimPrefix(line, "Array Size :"))
			if kb, err := strconv.ParseUint(m, 10, 64); err == nil {
				detail.CapacityBytes = kb * 1024
			}
		}
	}
	// Parse member devices
	memberRe := regexp.MustCompile(`\d+\s+\d+\s+\d+\s+\d+\s+(\S+)\s+(/dev/\S+)`)
	for _, line := range lines {
		matches := memberRe.FindStringSubmatch(line)
		if len(matches) == 3 {
			detail.Members = append(detail.Members, MemberInfo{
				Device: matches[2],
				State:  matches[1],
			})
		}
	}
	return detail, nil
}

func (r *raidManager) AssembleArray(device string, members []string) error {
	args := []string{"--assemble", device}
	args = append(args, members...)
	if out, err := exec.Command("mdadm", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("mdadm assemble %s: %w\n%s", device, err, out)
	}
	return nil
}

func (r *raidManager) StopArray(device string) error {
	if out, err := exec.Command("mdadm", "--stop", device).CombinedOutput(); err != nil {
		return fmt.Errorf("mdadm stop %s: %w\n%s", device, err, out)
	}
	return nil
}

func (r *raidManager) AddMember(device string, member string) error {
	exec.Command("wipefs", "-af", member).Run()
	if out, err := exec.Command("mdadm", device, "--add", member).CombinedOutput(); err != nil {
		return fmt.Errorf("mdadm add %s to %s: %w\n%s", member, device, err, out)
	}
	return nil
}

func (r *raidManager) RemoveMember(device string, member string) error {
	exec.Command("mdadm", device, "--fail", member).Run()
	if out, err := exec.Command("mdadm", device, "--remove", member).CombinedOutput(); err != nil {
		return fmt.Errorf("mdadm remove %s from %s: %w\n%s", member, device, err, out)
	}
	return nil
}

func (r *raidManager) ReshapeArray(device string, newCount int, newLevel int) error {
	args := []string{
		"--grow", device,
		"--raid-devices", strconv.Itoa(newCount),
		"--level", strconv.Itoa(newLevel),
	}
	if out, err := exec.Command("mdadm", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("mdadm reshape %s: %w\n%s", device, err, out)
	}
	return nil
}

func (r *raidManager) GetSyncStatus(device string) (*SyncStatus, error) {
	out, err := os.ReadFile("/proc/mdstat")
	if err != nil {
		return nil, err
	}
	// Find the device in mdstat
	devName := strings.TrimPrefix(device, "/dev/")
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, devName+" ") {
			continue
		}
		// Check next lines for recovery/reshape info
		for j := i + 1; j < len(lines) && j <= i+3; j++ {
			l := strings.TrimSpace(lines[j])
			if strings.Contains(l, "recovery") || strings.Contains(l, "reshape") || strings.Contains(l, "resync") {
				status := &SyncStatus{InSync: false}
				if strings.Contains(l, "recovery") {
					status.Action = "rebuild"
				} else if strings.Contains(l, "reshape") {
					status.Action = "reshape"
				} else {
					status.Action = "resync"
				}
				// Parse percentage
				re := regexp.MustCompile(`(\d+\.\d+)%`)
				if m := re.FindStringSubmatch(l); len(m) > 1 {
					status.PercentComplete, _ = strconv.ParseFloat(m[1], 64)
				}
				// Parse ETA
				etaRe := regexp.MustCompile(`finish=(\S+)`)
				if m := etaRe.FindStringSubmatch(l); len(m) > 1 {
					status.EstimatedFinish = m[1]
				}
				return status, nil
			}
		}
		return &SyncStatus{InSync: true}, nil
	}
	return nil, fmt.Errorf("device %s not found in /proc/mdstat", device)
}

// Phase 5: External enclosure support methods

func (r *raidManager) GetArrayUUID(device string) (string, error) {
	out, err := exec.Command("mdadm", "--detail", device).Output()
	if err != nil {
		return "", fmt.Errorf("mdadm detail %s: %w", device, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "UUID :") {
			return strings.TrimSpace(strings.TrimPrefix(line, "UUID :")), nil
		}
	}
	return "", fmt.Errorf("UUID not found for %s", device)
}

func (r *raidManager) AssembleArrayBySuperblock(uuid string) (*RAIDArrayInfo, error) {
	out, err := exec.Command("mdadm", "--assemble", "--scan", "--uuid="+uuid).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("mdadm assemble by UUID %s: %w\n%s", uuid, err, out)
	}
	// Parse assembled device from output
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "/dev/md") {
			re := regexp.MustCompile(`(/dev/md\d+)`)
			if m := re.FindString(line); m != "" {
				return &RAIDArrayInfo{Device: m, State: "active"}, nil
			}
		}
	}
	return &RAIDArrayInfo{State: "active"}, nil
}

func (r *raidManager) ReAddMember(arrayDevice string, member string) error {
	out, err := exec.Command("mdadm", arrayDevice, "--re-add", member).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mdadm re-add %s to %s: %w\n%s", member, arrayDevice, err, out)
	}
	return nil
}

func (r *raidManager) ScanSuperblocks(arrayUUID string) ([]SuperblockMatch, error) {
	// List all partitions from /proc/partitions
	data, err := os.ReadFile("/proc/partitions")
	if err != nil {
		return nil, err
	}
	var matches []SuperblockMatch
	partRe := regexp.MustCompile(`\s+\d+\s+\d+\s+\d+\s+(sd[a-z]+\d+|nvme\d+n\d+p\d+)`)
	for _, line := range strings.Split(string(data), "\n") {
		m := partRe.FindStringSubmatch(line)
		if len(m) < 2 {
			continue
		}
		partDev := "/dev/" + m[1]
		out, err := exec.Command("mdadm", "--examine", partDev).Output()
		if err != nil {
			continue
		}
		for _, eline := range strings.Split(string(out), "\n") {
			eline = strings.TrimSpace(eline)
			if strings.HasPrefix(eline, "Array UUID :") {
				uuid := strings.TrimSpace(strings.TrimPrefix(eline, "Array UUID :"))
				if uuid == arrayUUID {
					prev := ""
					// Try to extract previous device from superblock
					for _, dl := range strings.Split(string(out), "\n") {
						dl = strings.TrimSpace(dl)
						if strings.HasPrefix(dl, "Device Role :") || strings.HasPrefix(dl, "Events :") {
							// no direct previous device in examine output
						}
					}
					matches = append(matches, SuperblockMatch{
						PartitionDevice: partDev,
						ArrayUUID:       uuid,
						PreviousDevice:  prev,
					})
				}
				break
			}
		}
	}
	return matches, nil
}
