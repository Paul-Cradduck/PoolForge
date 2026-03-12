+++
archetype = "home"
title = "PoolForge"
+++

Open-source hybrid RAID storage manager for Ubuntu LTS. Combines mixed-size disks into a single storage pool with RAID redundancy using mdadm and LVM.

```text
┌─────────────────────────────────────────────────────────────┐
│                     /mnt/poolforge/mypool                    │
│                      Single ext4 Volume                      │
├─────────────────────────────────────────────────────────────┤
│                        LVM (lv_pool)                         │
├───────────────────┬──────────────────┬──────────────────────┤
│    md0 (RAID5)    │   md1 (RAID5)    │    md2 (standalone)  │
│     Tier 0        │    Tier 1        │      Tier 2          │
├────┬────┬────┬────┼────┬────┬────┬───┼────┬───────┬────┬───┤
│ 3G │ 3G │ 3G │ 3G │    │ 2G │ 2G │2G│    │       │    │5G │
├────┴────┴────┴────┼────┴────┴────┴───┼────┴───────┴────┴───┤
│   3 GB disk       │   5 GB disk      │   5 GB disk         │
│                   │                  │   10 GB disk ────────┤
└───────────────────┴──────────────────┴──────────────────────┘
```

## Why PoolForge?

Traditional RAID wastes capacity when disks are different sizes — everything gets sized to the smallest disk. PoolForge's tiered slicing algorithm maximizes usable space by letting larger disks contribute their extra capacity to higher tiers.

{{% children containerstyle="div" style="h2" description="true" %}}
