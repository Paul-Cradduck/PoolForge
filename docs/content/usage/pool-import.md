+++
title = "Pool Import"
weight = 2
+++

Move disks to a new machine and recover your pool:

```bash
sudo poolforge pool import
```

Import automatically:
- Assembles md arrays from superblocks (works regardless of device name changes)
- Finds the PoolForge LVM volume group
- Reads backup metadata from the pool mount point
- Remaps device names by matching disk capacities (50MB tolerance)
- Remaps md device names by matching array members
- Fixes stale superblocks if arrays were modified before migration

```text
  Source Machine                    Destination Machine
  ┌──────────────────┐             ┌──────────────────┐
  │ /dev/sda (3GB)   │             │ /dev/nvme1n1 (5GB)│
  │ /dev/sdb (5GB)   │  move       │ /dev/nvme2n1 (3GB)│
  │ /dev/sdc (5GB)   │  disks →    │ /dev/nvme3n1 (10G)│
  │ /dev/sdd (10GB)  │             │ /dev/nvme4n1 (5GB)│
  └──────────────────┘             └──────────────────┘
```

{{% notice tip %}}
Same-size disks are handled correctly — PoolForge gives identical disks identical slice layouts, making them interchangeable.
{{% /notice %}}
