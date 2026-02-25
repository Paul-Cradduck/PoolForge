package engine

// ComputeDiskSlices returns the slices a disk is eligible for given its capacity
// and the computed capacity tiers. Each slice corresponds to a tier whose
// cumulative boundary does not exceed the disk capacity.
func ComputeDiskSlices(diskCapacity uint64, tiers []CapacityTier) []SliceInfo {
	var slices []SliceInfo
	var cumulative uint64
	for _, t := range tiers {
		cumulative += t.SliceSizeBytes
		if diskCapacity >= cumulative {
			slices = append(slices, SliceInfo{
				TierIndex: t.Index,
				SizeBytes: t.SliceSizeBytes,
			})
		}
	}
	return slices
}
