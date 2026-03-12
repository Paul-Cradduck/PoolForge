+++
title = "Replace Disk"
weight = 4
+++

Swap a failed disk with a new one:

```bash
poolforge pool replace-disk mypool --old /dev/sda --new /dev/sde
```

## Process

1. Failed disk is removed from all arrays
2. New disk is partitioned with matching slice layout
3. Slices are added to each array
4. Rebuild starts automatically

```text
  Failed disk              New disk (same or larger)
  ┌──────────┐             ┌──────────┐
  │ /dev/sda │──── swap ──▶│ /dev/sde │
  │  FAILED  │             │  HEALTHY │
  └──────────┘             └──────────┘
```

Monitor rebuild progress via the web portal (live SSE updates) or:

```bash
poolforge pool status mypool
```
