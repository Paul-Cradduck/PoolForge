# Phase 5: NAS File Sharing & Monitoring — Tasks

## Sharing

**T1 — Data model & types**
Add `Share`, `NASUser` structs and monitoring types to `internal/engine/types.go`. Add `Shares` and `Users` fields to `Pool`.

**T2 — Share manager**
Create `internal/sharing/manager.go` with `ShareManager` interface. Methods: `CreateShare`, `DeleteShare`, `UpdateShare`, `ListShares`. Creates/removes subdirectories, sets ownership to poolforge group.

**T3 — SMB backend**
Create `internal/sharing/smb.go`. Generates `/etc/samba/poolforge.conf` from share list. Adds include line to smb.conf. Reads workgroup/server name from poolforge.conf. Manages smbd/nmbd lifecycle.

**T4 — NFS backend**
Create `internal/sharing/nfs.go`. Writes marker-delimited block in `/etc/exports`. Runs `exportfs -ra`. Manages nfs-server lifecycle.

**T5 — User management**
Create `internal/sharing/users.go`. Creates POSIX user + poolforge group membership. Sets Samba password via `smbpasswd`. Handles pool-scoped and global access. Stores in pool metadata.

**T6 — Share deletion safety**
Confirmation check: calculate directory size, require `--force` in CLI, confirmation dialog in UI showing size. Then `rm -rf` the directory.

## Monitoring

**T7 — Metrics collector**
Create `internal/monitoring/collector.go`. 1-second sample loop reading `/proc/diskstats` and `/proc/net/dev`. Computes deltas for rates. Stores in ring buffer (5 min / 300 samples).

**T8 — Protocol & connection tracking**
Parse `smbstatus` for SMB clients. Parse `ss` on port 2049 for NFS clients. Use `ss` byte counters filtered by port for per-protocol throughput.

**T9 — Disk log**
Create `internal/monitoring/disklog.go`. Every 60s, append averaged sample to `/var/lib/poolforge/metrics.log`. Prune entries older than 30 days on startup and daily.

**T10 — Monitoring SSE endpoint**
Add `/api/monitoring/live` SSE stream pushing latest metrics every second. Add `/api/monitoring/history` returning disk log data for requested time range. Add `/api/monitoring/clients` returning current connections.

## Integration

**T11 — Engine integration**
Add share/user/monitoring methods to `EngineService` interface. Wire `ShareManager` and `MetricsCollector` into engine.

**T12 — CLI commands**
Add `share create/list/update/delete` and `user add/delete/list` subcommands. Password prompt for user add. `--force` flag for share delete.

**T13 — REST API**
Register share, user, and monitoring endpoints in `internal/api/server.go`.

**T14 — Web portal — Shares & Users**
Shares panel with create dialog (name, protocols, NFS clients, guest toggle, browsable toggle, read-only toggle). Users panel with add (name + password field) and delete. Protocol status indicators (SMB ✓/✗, NFS ✓/✗).

**T15 — Web portal — Monitoring tab**
Dedicated tab with gauges for: disk read/write MB/s, disk IOPS, network in/out, SMB/NFS throughput. Client connection table. SSE-driven live updates. Historical view from disk log.

**T16 — Install script update**
Add `samba` and `nfs-kernel-server` to `install.sh` package list.
