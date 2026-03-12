+++
title = "Scrub & Backup"
weight = 3
+++

## Scrub Scheduling

Weekly mdadm array checks detect silent data corruption (bit rot). The scrub scheduler runs automatically and reports results to the alert engine.

## Metadata Backup

Every hour, pool metadata is backed up to the pool mount point at:

```
/mnt/poolforge/<poolname>/.poolforge/
```

This backup is used by `poolforge pool import` to recover pools on a new machine. The boot config (`mdadm.conf` + initramfs) is regenerated on each backup.
