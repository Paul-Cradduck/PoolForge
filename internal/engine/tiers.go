package engine

import "sort"

// ComputeCapacityTiers computes capacity tiers from a set of disk capacities.
// Returns tiers with slice sizes and eligible disk counts. Tiers with < 2
// eligible disks are excluded (no RAID possible).
func ComputeCapacityTiers(capacities []uint64) []CapacityTier {
	if len(capacities) < 2 {
		return nil
	}

	// Extract sorted unique capacities
	unique := uniqueSorted(capacities)

	var tiers []CapacityTier
	idx := 0
	for i, u := range unique {
		var sliceSize uint64
		if i == 0 {
			sliceSize = u
		} else {
			sliceSize = u - unique[i-1]
		}
		if sliceSize == 0 {
			continue
		}

		// Eligible disks: those with capacity >= cumulative boundary (unique[i])
		eligible := 0
		for _, c := range capacities {
			if c >= u {
				eligible++
			}
		}

		if eligible < 2 {
			continue
		}

		tiers = append(tiers, CapacityTier{
			Index:             idx,
			SliceSizeBytes:    sliceSize,
			EligibleDiskCount: eligible,
		})
		idx++
	}
	return tiers
}

func uniqueSorted(vals []uint64) []uint64 {
	sorted := make([]uint64, len(vals))
	copy(sorted, vals)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	result := []uint64{sorted[0]}
	for i := 1; i < len(sorted); i++ {
		if sorted[i] != sorted[i-1] {
			result = append(result, sorted[i])
		}
	}
	return result
}
