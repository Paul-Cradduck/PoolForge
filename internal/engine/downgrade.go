package engine

// EvaluateDowngrade assesses the impact of removing a disk from a pool.
func EvaluateDowngrade(pool *Pool, disk string) *DowngradeReport {
	report := &DowngradeReport{Safe: true}

	var target *DiskInfo
	for i := range pool.Disks {
		if pool.Disks[i].Device == disk {
			target = &pool.Disks[i]
			break
		}
	}
	if target == nil {
		report.Safe = false
		return report
	}

	for _, sl := range target.Slices {
		for _, a := range pool.RAIDArrays {
			if a.TierIndex != sl.TierIndex {
				continue
			}
			newCount := len(a.Members) - 1
			change := ArrayChange{
				Device:     a.Device,
				OldLevel:   a.RAIDLevel,
				OldMembers: len(a.Members),
				NewMembers: newCount,
			}
			if newCount < 2 {
				change.Destroyed = true
				report.Safe = false
				report.TiersRemoved = append(report.TiersRemoved, a.TierIndex)
			} else {
				newLevel, _ := SelectRAIDLevel(pool.ParityMode, newCount)
				change.NewLevel = newLevel
			}
			report.ArrayChanges = append(report.ArrayChanges, change)
		}
	}
	return report
}
