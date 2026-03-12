+++
title = "Code Structure"
weight = 1
+++

```text
cmd/poolforge/main.go          CLI entry point + cobra commands
internal/api/server.go         REST API + embedded web dashboard
internal/engine/               Core pool logic
  ├── engine_impl.go           CreatePool, Status, List
  ├── lifecycle.go             Add/Remove/Replace disk
  ├── import.go                Pool import & device remapping
  ├── start_stop.go            Pool start/stop & auto-start
  ├── tiers.go                 Capacity tier computation
  ├── slicing.go               Disk slice calculation
  ├── raid_selection.go        RAID level picker
  └── downgrade.go             Evaluate tier downgrade
internal/storage/              Linux storage tool wrappers
  ├── disk.go                  gdisk, blockdev
  ├── raid.go                  mdadm
  ├── lvm.go                   pvcreate, lvcreate, etc.
  └── fs.go                    mkfs, resize2fs
internal/safety/               Background safety daemon
  ├── daemon.go                Main daemon loop
  ├── smart.go                 SMART health checks
  ├── scrub.go                 Scrub scheduling
  ├── alerts.go                Webhook + SMTP alerts
  ├── boot.go                  mdadm.conf + initramfs
  └── logbuffer.go             Persistent log ring buffer
internal/metadata/             Atomic JSON metadata store
internal/monitoring/           Disk I/O + network stats
internal/sharing/              SMB/NFS share management
internal/replication/          Rsync sync jobs + node pairing
internal/snapshots/            LVM snapshot management
```
