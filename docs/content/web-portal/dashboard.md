+++
title = "Dashboard"
weight = 1
+++

Start the web portal:

```bash
poolforge serve --addr 0.0.0.0:8080 --user admin --pass secret
```

Or via the systemd service with `/etc/poolforge.conf`.

## Features

- **Pool overview** — health status, disk count, parity mode
- **Capacity bar** — used, free, parity/redundancy, overhead
- **Per-disk slice map** — visual tier breakdown per disk
- **Array status** — health of each md array
- **Live rebuild progress** — auto-detected via SSE (no polling)
- **Safety status** — SMART, scrub, backup, boot config indicators
- **Alerts panel** — history of all alerts
- **Scrollable logs** — with clear button

## Disk Operations

All available from the dashboard:
- **Add Disk** — preview shows projected capacity gain and new tiers
- **Remove Disk** — safety check prevents removal if it would destroy data
- **Fail Disk** — simulate failure for testing
- **Replace Disk** — swap a failed disk with a new one

## Pool Creation

- Disk selector with auto-select
- Live capacity preview during creation with tier breakdown
- Warnings for excluded smaller disks that can never be added later
