package engine

import "fmt"

// SelectRAIDLevel returns the RAID level for a given parity mode and eligible disk count.
func SelectRAIDLevel(parity ParityMode, eligibleDisks int) (int, error) {
	if eligibleDisks < 2 {
		return 0, fmt.Errorf("need at least 2 disks, got %d", eligibleDisks)
	}
	switch parity {
	case Parity1:
		if eligibleDisks >= 3 {
			return 5, nil
		}
		return 1, nil
	case Parity2:
		if eligibleDisks >= 4 {
			return 6, nil
		}
		if eligibleDisks == 3 {
			return 5, nil
		}
		return 1, nil
	default:
		return 0, fmt.Errorf("unsupported parity mode: %d", parity)
	}
}
