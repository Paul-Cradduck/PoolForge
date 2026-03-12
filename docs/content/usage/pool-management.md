+++
title = "Pool Management"
weight = 1
+++

## Create a Pool

```bash
poolforge pool create --name mypool --disks /dev/sda,/dev/sdb,/dev/sdc --parity parity1
```

Options:
- `--name` — Pool name (used in mount path `/mnt/poolforge/<name>`)
- `--disks` — Comma-separated list of block devices
- `--parity` — `parity1` (RAID1/5) or `parity2` (RAID6)

## List Pools

```bash
poolforge pool list
```

## Pool Status

```bash
poolforge pool status mypool
```

Shows capacity breakdown, tier layout, array health, and disk details.

## Delete a Pool

```bash
poolforge pool delete mypool
```

{{% notice warning %}}
This destroys all data on the pool. Arrays and LVM volumes are removed.
{{% /notice %}}
