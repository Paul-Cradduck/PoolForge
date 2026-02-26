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

## Test Infrastructure

**T17 — Test environment scripts**
Create `test/aws-up.sh`, `test/aws-down.sh`, `test/aws-ssh.sh`, `test/aws-deploy.sh`. Automate EC2+EBS provisioning, binary deployment, and teardown. Write connection info to `/tmp/pf-test-env.json`.

## Manual Validation Tests

**T18 — Share lifecycle testing**
Environment: `./test/aws-up.sh --disks 3x10`
1. Create pool with 3 disks
2. Create SMB share, verify accessible from Windows/macOS/Linux client
3. Create NFS share, verify mountable from Linux client with `mount -t nfs`
4. Create dual-protocol share (SMB+NFS), verify both work
5. Toggle read-only, verify writes blocked
6. Toggle SMB browsing visibility, verify in network discovery
7. Toggle guest access, verify unauthenticated access
8. Update share protocols (remove SMB, keep NFS), verify SMB stops working
9. Delete share with `--force`, verify directory and data removed
10. Delete share without `--force`, verify confirmation prompt

**T19 — User management testing**
1. Create user via CLI, verify SMB login works
2. Create user via web UI, verify SMB login works
3. Create pool-scoped user, verify access limited to that pool
4. Create global user, verify access to shares across multiple pools
5. Delete user, verify SMB login rejected
6. Verify POSIX file ownership matches user UID

**T20 — NFS client restriction testing**
1. Create NFS share with client restriction `192.168.x.0/24`
2. Verify access from allowed IP
3. Verify access denied from disallowed IP (use second instance or different subnet)
4. Update restriction to `*`, verify open access

**T21 — Service lifecycle testing**
1. Verify smbd/nmbd not running before any SMB shares exist
2. Create first SMB share, verify smbd/nmbd started
3. Delete last SMB share, verify smbd/nmbd stopped
4. Same for nfs-server with NFS shares
5. Verify services survive instance reboot

**T22 — SMB configuration testing**
1. Set custom workgroup in poolforge.conf, verify `smbclient -L` shows it
2. Set custom server name, verify visible in network browsing
3. Verify poolforge.conf include line in smb.conf
4. Verify manual smb.conf edits outside poolforge section preserved

**T23 — Monitoring testing**
Environment: `./test/aws-up.sh --disks 4x10,4x5,4x3`
1. Create pool, create shares, connect SMB/NFS clients
2. Open monitoring tab, verify disk IO gauges update in real-time
3. Run `dd` write workload, verify write throughput gauge responds
4. Verify network IO gauges show per-interface stats
5. Verify SMB/NFS protocol breakdown gauges
6. Verify client connection list shows connected users with correct IP/share/protocol
7. Disconnect client, verify removed from list
8. Wait 5+ minutes, verify ring buffer provides history on page reload
9. Wait 2+ minutes, verify disk log file at `/var/lib/poolforge/metrics.log` has entries
10. Query `/api/monitoring/history?range=5m`, verify returns data

**T24 — Performance baseline**
1. Run fio sequential read/write benchmarks on pool
2. Verify monitoring gauges match fio reported throughput (within 10%)
3. Run SMB file copy, verify network gauges reflect transfer
4. Run NFS file copy, verify protocol breakdown is accurate

**T25 — Web portal integration**
1. Verify shares panel shows all shares with correct protocols/settings
2. Create share via UI, verify appears and is accessible
3. Edit share via UI, verify changes take effect
4. Delete share via UI, verify confirmation dialog shows size
5. Create user via UI with password field, verify login works
6. Verify protocol status indicators (SMB ✓/✗, NFS ✓/✗) are accurate
7. Verify monitoring tab gauges render and update via SSE
