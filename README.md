# PoolForge

Open-source Synology Hybrid RAID (SHR) clone for Ubuntu LTS. Combines mixed-size disks into a single storage pool with RAID redundancy using mdadm and LVM.

## How It Works

PoolForge implements the same tiered slicing algorithm as Synology's SHR:

1. **Sorts disks by capacity** and computes capacity tiers — each tier represents a slice size where a group of disks can contribute equally
2. **Creates RAID arrays** for each tier (RAID1 for 2 disks, RAID5 for 3+, RAID6 with SHR2)
3. **Combines arrays into a single LVM volume** with an ext4 filesystem
4. **Maximizes usable space** — larger disks contribute their extra capacity to higher tiers that smaller disks can't reach

```
Example: 3GB + 5GB + 5GB + 10GB disks

Tier 0 (3GB slices): [3G][3G][3G][3G] → RAID5 = 9GB usable
Tier 1 (2GB slices):      [2G][2G][2G] → RAID5 = 4GB usable  
Tier 2 (5GB slices):               [5G] → standalone = 5GB usable

Total: 18GB usable from 23GB raw (78% efficiency)
vs RAID5 with uniform 3GB slices: 9GB usable (39% efficiency)
```

## Install

```bash
curl -sSL https://raw.githubusercontent.com/Paul-Cradduck/PoolForge/main/install.sh | sudo bash
```

Installs the binary, dependencies (mdadm, lvm2, smartmontools), and a systemd service.

### Configure

```bash
sudo nano /etc/poolforge.conf
```

```ini
POOLFORGE_USER=admin
POOLFORGE_PASS=yourpassword
POOLFORGE_ADDR=0.0.0.0:8080
```

```bash
sudo systemctl start poolforge
```

## Web Portal

Dark-themed single-page dashboard at `http://your-server:8080`.

**Pool Management:**
- Create pools with disk chip selectors and auto-select
- Live capacity preview during creation with tier breakdown
- Warnings for excluded smaller disks that can never be added later

**Capacity Breakdown:**
- Stacked bar: used, free, parity/redundancy, overhead, expansion potential
- Per-disk slice map showing tier colors and free space

**Disk Operations:**
- Add disk — preview shows projected capacity gain and new tiers
- Remove disk — safety check prevents removal if it would destroy data
- Fail disk — simulate failure for testing
- Replace disk — swap a failed disk with a new one

**Monitoring:**
- Live RAID rebuild progress via SSE (auto-detected, no button click)
- Safety daemon status panel (SMART, scrub, backup, boot config)
- Alerts panel with history
- Scrollable logs with clear button

## CLI

```bash
# Pool operations
poolforge pool create --name mypool --disks /dev/sda,/dev/sdb,/dev/sdc --parity shr1
poolforge pool list
poolforge pool status mypool
poolforge pool delete mypool

# Disk lifecycle
poolforge pool add-disk mypool --disk /dev/sdd
poolforge pool remove-disk mypool --disk /dev/sda
poolforge pool fail-disk mypool --disk /dev/sda
poolforge pool replace-disk mypool --old /dev/sda --new /dev/sdd

# Import pool from disks moved from another system
poolforge pool import

# Web portal with safety daemon
poolforge serve --addr 0.0.0.0:8080 --user admin --pass secret
```

## Pool Import / Disk Migration

Move disks to a new machine and recover your pool:

```bash
# On new machine with disks attached:
sudo apt install mdadm lvm2
sudo poolforge pool import
```

Import automatically:
- Assembles md arrays from superblocks (works regardless of device name changes)
- Finds the PoolForge LVM volume group
- Reads backup metadata from the pool mount point
- Remaps device names by matching disk capacities
- Remaps md device names by matching array members
- Fixes stale superblocks if arrays were modified before migration

Same-size disks are handled correctly — SHR gives identical disks identical slice layouts, making them interchangeable.

## Safety Features

All run automatically via the background safety daemon:

| Feature | Interval | Description |
|---------|----------|-------------|
| SMART Monitoring | 5 min | Checks disk health via smartctl, alerts on failures |
| Scrub Scheduling | Weekly | Triggers mdadm array checks to detect silent corruption |
| Metadata Backup | Hourly | Copies metadata to pool mount point for disaster recovery |
| Boot Config | On backup | Generates mdadm.conf + update-initramfs for auto-assembly |
| Graceful Shutdown | SIGINT/SIGTERM | Backs up metadata and stops scrubs before exit |

Alerts can be sent via webhook or SMTP email.

## Parity Modes

| Mode | Min Disks | Redundancy | Description |
|------|-----------|------------|-------------|
| SHR1 | 2 | 1 disk failure | RAID1 (2 disks) or RAID5 (3+) per tier |
| SHR2 | 3 | 2 disk failures | RAID6 per tier |

## Architecture

```
cmd/poolforge/main.go          CLI + serve command
internal/engine/
  ├── engine.go                 EngineService interface
  ├── engine_impl.go            CreatePool, GetPoolStatus, auto-expand
  ├── lifecycle.go              Add/Remove/Fail/Replace disk, DeletePool
  ├── import.go                 Pool import with device remapping
  ├── tiers.go                  ComputeCapacityTiers
  ├── raid_selection.go         SelectRAIDLevel
  ├── slicing.go                ComputeDiskSlices
  └── downgrade.go              EvaluateDowngrade
internal/storage/               DiskManager, RAIDManager, LVMManager, FilesystemManager
internal/metadata/              JSON metadata store with atomic writes
internal/api/
  ├── server.go                 REST API with basic auth
  └── static/index.html         Embedded SPA dashboard
internal/safety/
  ├── daemon.go                 Safety daemon orchestrator
  ├── smart.go                  SMART monitoring
  ├── scrub.go                  Scrub scheduler
  ├── alerts.go                 Webhook + SMTP alerts
  ├── boot.go                   mdadm.conf + metadata backup
  └── logbuffer.go              Persistent log buffer
```

## Requirements

- Ubuntu LTS (20.04, 22.04, 24.04)
- x86_64 architecture
- mdadm, lvm2, smartmontools
- Root access
- Minimum 2 disks

## Uninstall

```bash
curl -sSL https://raw.githubusercontent.com/Paul-Cradduck/PoolForge/main/uninstall.sh | sudo bash
```

Removes the binary, service, and optionally config/metadata. Never touches your arrays, LVM volumes, or data.

## License

MIT
