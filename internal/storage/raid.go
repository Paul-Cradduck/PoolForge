package storage

import (
	"fmt"
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
