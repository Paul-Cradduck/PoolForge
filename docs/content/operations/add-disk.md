+++
title = "Add Disk"
weight = 1
+++

Online expansion — add a disk to a running pool with zero downtime.

```bash
poolforge pool add-disk mypool --disk /dev/sdd
```

## What Happens

1. New disk is partitioned into slices matching existing tiers
2. Slices are added to existing md arrays
3. Arrays reshape online (RAID1→RAID5, grow capacity)
4. New tiers are created for remaining space
5. LVM auto-expands when reshape completes
6. Filesystem grows online

```text
Before: 3 disks (3GB + 5GB + 5GB)       After: + 10GB disk added

  Tier 0: [3G][3G][3G] → RAID5 = 6GB    Tier 0: [3G][3G][3G][3G] → RAID5 = 9GB
  Tier 1:     [2G][2G] → RAID1 = 2GB    Tier 1:     [2G][2G][2G] → RAID5 = 4GB
                                         Tier 2:              [5G] → standalone
                          8GB usable                                  18GB usable
```
