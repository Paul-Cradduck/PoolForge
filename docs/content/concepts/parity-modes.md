+++
title = "Parity Modes"
weight = 2
+++

PoolForge supports two parity modes that determine fault tolerance per tier.

## parity1 — Single Disk Fault Tolerance

| Disks in Tier | RAID Level | Tolerance |
|---------------|------------|-----------|
| 2 | RAID1 (mirror) | 1 disk |
| 3+ | RAID5 | 1 disk |

```text
  [D1] [D2] [P ]
  [D3] [P ] [D4]
  [P ] [D5] [D6]
```

## parity2 — Dual Disk Fault Tolerance

| Disks in Tier | RAID Level | Tolerance |
|---------------|------------|-----------|
| 3+ | RAID6 | 2 disks |

```text
  [D1] [D2] [P ] [Q ]
  [D3] [P ] [Q ] [D4]
  [P ] [Q ] [D5] [D6]
```

{{% notice warning %}}
`parity2` requires a minimum of 3 disks. Tiers with fewer than 3 members cannot provide dual parity.
{{% /notice %}}
