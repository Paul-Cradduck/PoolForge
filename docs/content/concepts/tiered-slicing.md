+++
title = "Tiered Slicing"
weight = 1
+++

PoolForge's core innovation is the tiered slicing algorithm. It maximizes usable space from mixed-size disks by computing capacity tiers.

## How It Works

1. **Sort disks by capacity** and compute tiers from the differences between consecutive sizes
2. **Create RAID arrays** for each tier — all disks large enough participate
3. **Combine arrays** into a single LVM volume with an ext4 filesystem

## Example

Given 4 disks: 3GB + 5GB + 5GB + 10GB (sorted):

```text
     3GB    5GB    5GB    10GB
      │      │      │      │
      ├──────┼──────┼──────┤  Tier 0: all 4 disks × 3GB slice
      │      ├──────┼──────┤  Tier 1: 3 disks × 2GB slice (5-3=2)
      │      │      │      │  Tier 2: 1 disk × 5GB slice (10-5=5)
      ▼      ▼      ▼      ▼
```

### RAID Arrays Per Tier

| Tier | Disks | Slice | RAID Level | Usable |
|------|-------|-------|------------|--------|
| 0 | 4 | 3 GB | RAID5 | 9 GB |
| 1 | 3 | 2 GB | RAID5 | 4 GB |
| 2 | 1 | 5 GB | Standalone | 5 GB |
| | | | **Total** | **18 GB** |

18 GB usable from 23 GB raw = **78% efficiency**, compared to 39% with traditional RAID5 (limited to smallest disk).

## Per-Disk Slice Map

```text
  Disk 1 (3GB)   [████ Tier 0 ████]
  Disk 2 (5GB)   [████ Tier 0 ████][██ Tier 1 ██]
  Disk 3 (5GB)   [████ Tier 0 ████][██ Tier 1 ██]
  Disk 4 (10GB)  [████ Tier 0 ████][██ Tier 1 ██][████ Tier 2 ████]
```
