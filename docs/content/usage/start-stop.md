+++
title = "Start & Stop"
weight = 3
+++

## Start a Pool

```bash
poolforge pool start mypool
```

Assembles RAID arrays in ascending tier order, activates LVM, and mounts the filesystem. Detects degraded arrays and attempts re-add of missing members.

## Stop a Pool

```bash
poolforge pool stop mypool
```

Unmounts the filesystem, deactivates LVM, and stops arrays in descending tier order.

## Auto-Start

```bash
poolforge pool auto-start mypool --enable
poolforge pool auto-start mypool --disable
```

When enabled, the pool starts automatically on boot via the systemd service.
