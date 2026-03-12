+++
title = "Storage Stack"
weight = 3
+++

PoolForge builds on standard Linux storage primitives:

```text
  ┌────────────────────────────────┐
  │     /mnt/poolforge/mypool      │  ext4 filesystem
  ├────────────────────────────────┤
  │     LVM Logical Volume         │  lv_pool
  ├────────────────────────────────┤
  │     LVM Volume Group           │  vg_poolforge_mypool
  ├──────────┬──────────┬──────────┤
  │   md0    │   md1    │   md2    │  mdadm arrays (one per tier)
  ├──────────┴──────────┴──────────┤
  │     GPT partitions (slices)    │  one partition per tier per disk
  ├────────────────────────────────┤
  │     Physical disks             │  /dev/sd*, /dev/nvme*
  └────────────────────────────────┘
```

Each layer uses battle-tested Linux tools:

| Layer | Tool | Purpose |
|-------|------|---------|
| Partitioning | `gdisk` | GPT partition tables, one partition per tier slice |
| RAID | `mdadm` | Software RAID arrays per tier |
| Volume Management | `lvm2` | Combines all arrays into one volume group |
| Filesystem | `mkfs.ext4` / `resize2fs` | Single ext4 mount point |
