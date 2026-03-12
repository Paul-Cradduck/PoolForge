+++
title = "Safety Daemon"
weight = 1
+++

The safety daemon runs automatically in the background when PoolForge is started via `poolforge serve` or the systemd service.

| Feature | Interval | Description |
|---------|----------|-------------|
| SMART Monitoring | 5 min | Checks disk health via `smartctl`, alerts on failures |
| Scrub Scheduling | Weekly | Triggers mdadm array checks to detect silent corruption |
| Metadata Backup | Hourly | Copies metadata to pool mount point for disaster recovery |
| Boot Config | On backup | Generates `mdadm.conf` + `update-initramfs` for auto-assembly |
| Graceful Shutdown | SIGINT/SIGTERM | Backs up metadata and stops scrubs before exit |

## Alerts

The daemon sends alerts via:
- **Webhook** — POST to a configured URL
- **SMTP Email** — Send to configured recipients

Configure alerts in `/etc/poolforge.conf`.
