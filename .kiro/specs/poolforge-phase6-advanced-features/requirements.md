# Phase 6: Snapshots & Replication — Requirements

## Snapshots

**R1 — LVM Snapshots**
Users can create point-in-time snapshots of a pool's logical volume. Snapshots are copy-on-write using LVM's native snapshot support. Pool creation reserves a configurable percentage of VG space for snapshots (default 10%).

**R2 — Snapshot Lifecycle**
Snapshots can be created on-demand (CLI/UI) or on a schedule. Each snapshot has a configurable max age — auto-deleted after expiry. Snapshots can be manually deleted at any time. If snapshot space fills up, the oldest snapshot is pruned automatically.

**R3 — Snapshot Access**
Snapshots are mountable read-only at `/mnt/poolforge/<pool>/.snapshots/<name>/` for file recovery. Users can browse and copy files from snapshots via the share protocols (exposed as a hidden read-only share).

## Replication

**R4 — Node Pairing**
Two PoolForge nodes pair via a one-time code exchange. Node A generates a pairing code (shown in UI/CLI), user enters it on Node B. SSH keys are exchanged automatically. A node can pair with multiple other nodes.

**R5 — Sync Jobs**
Users create sync jobs between paired nodes. Jobs can target an entire pool or specific shares. Two modes:
- **One-way**: primary → backup (migration/backup use case)
- **Bidirectional**: both sides sync changes (async cluster use case)

Conflict resolution: last-write-wins (newer timestamp).

**R6 — Sync Scheduling**
Sync jobs can run on-demand (CLI/UI), on a schedule (every N minutes/hours/daily), or both. Scheduled jobs are managed by the PoolForge daemon.

**R7 — Sync Transport**
Uses rsync over SSH (leveraging paired keys). Delta transfer for bandwidth efficiency. Supports resuming interrupted syncs.

**R8 — Sync UI**
Dedicated sync tab in the web portal showing: paired nodes, sync jobs, last sync time, bytes transferred, success/failure status, and live progress during active syncs.

## Interface

**R9 — CLI**
All snapshot and replication operations available via CLI.

**R10 — REST API**
All snapshot and replication operations exposed via REST API.

**R11 — Web Portal**
Snapshot management in the pool view. Dedicated sync tab for replication.

## Out of Scope (Future Phases)
- ZFS/Btrfs migration for native snapshots
- Incremental block-level replication (ZFS send/receive)
- Real-time / continuous sync (inotify-based)
- Multi-master conflict resolution beyond last-write-wins
- Snapshot-based backup to cloud (S3, B2)
- Encryption of sync transport beyond SSH
