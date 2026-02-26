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

## Manual Validation Tests (via Web Portal)

All manual testing is performed through the PoolForge web portal at `http://<instance-ip>:8080`. SSH is only used for initial setup and to generate client traffic for monitoring validation.

**T18 — Share lifecycle testing**
Environment: `./test/aws-up.sh --disks 3x10`
1. Create pool via portal
2. Create SMB share via shares panel, connect from Windows/macOS client to verify
3. Create NFS share via shares panel, mount from Linux client to verify
4. Create dual-protocol share (SMB+NFS), verify both accessible
5. Edit share: toggle read-only via portal, verify writes blocked from client
6. Edit share: toggle SMB browsing visibility, verify in network discovery
7. Edit share: toggle guest access, verify unauthenticated client access
8. Edit share: remove SMB protocol (keep NFS), verify SMB stops working
9. Delete share via portal, verify confirmation dialog shows directory size
10. Confirm deletion, verify directory and data removed

**T19 — User management testing**
1. Create user via users panel (name + password field), verify SMB login works from client
2. Create second user, verify both can access shares
3. Create pool-scoped user, verify access limited to that pool's shares
4. Create global-access user, verify access to shares across multiple pools
5. Delete user via portal, verify SMB login rejected from client

**T20 — NFS client restriction testing**
1. Create NFS share via portal with client restriction field set to specific CIDR
2. Verify access from allowed IP
3. Verify access denied from disallowed IP
4. Edit share via portal, change restriction to `*`, verify open access

**T21 — Service lifecycle testing**
1. Verify protocol status indicators show SMB ✗ / NFS ✗ before any shares
2. Create first SMB share via portal, verify SMB indicator changes to ✓
3. Delete last SMB share via portal, verify SMB indicator changes to ✗
4. Same for NFS shares and NFS indicator
5. Reboot instance, verify portal comes back with correct indicators

**T22 — SMB configuration testing**
1. Set custom workgroup via portal settings, verify reflected in SMB clients
2. Set custom server name via portal settings, verify visible in network browsing
3. Verify changes persist after portal refresh

**T23 — Monitoring testing**
Environment: `./test/aws-up.sh --disks 4x10,4x5,4x3`
1. Create pool and shares via portal, connect SMB/NFS clients
2. Open monitoring tab, verify disk IO gauges show real-time activity
3. Generate write workload from client (large file copy), verify write throughput gauge responds
4. Verify network IO gauges show per-interface in/out stats
5. Verify SMB/NFS protocol breakdown gauges reflect active protocol traffic
6. Verify client connection list shows connected users with correct IP, share, protocol, duration
7. Disconnect client, verify removed from connection list
8. Close and reopen monitoring tab, verify gauges show recent history (ring buffer)
9. Verify historical data available after 2+ minutes of activity

**T24 — Performance validation**
1. Copy large file via SMB to pool, observe monitoring gauges during transfer
2. Copy large file via NFS to pool, verify protocol gauge shows NFS traffic
3. Verify disk IO gauges correlate with network transfer rates
4. Verify gauges return to idle after transfer completes

**T25 — Portal end-to-end**
1. Full workflow: create pool → create users → create shares → connect clients → monitor
2. Verify all panels update consistently (shares panel, users panel, monitoring tab, pool status)
3. Verify portal remains responsive during active file transfers
4. Verify SSE streams reconnect after brief network interruption (refresh page)
