# Phase 6: Snapshots & Replication — Tasks

## Snapshots

**T1 — Pool creation change**
Modify `CreatePool` to accept `--snapshot-reserve` (default 10%). Create LV at `(100 - reserve)%FREE` instead of `100%FREE`. Store reserve config in pool metadata.

**T2 — Snapshot manager**
Create `internal/snapshots/manager.go`. Create/delete/list/mount LVM snapshots. Calculate snapshot space usage from `lvs` output.

**T3 — Snapshot scheduling & pruning**
Add snapshot scheduler to safety daemon. Create snapshots on interval, delete expired ones. Auto-prune oldest when space runs low.

**T4 — Snapshot access**
Mount snapshots read-only. Optionally expose as hidden read-only SMB/NFS share for file recovery.

## Replication

**T5 — SSH key management**
Create `internal/replication/keys.go`. Generate/store SSH keypair at `~/.poolforge/`. Manage authorized_keys for paired nodes.

**T6 — Pairing protocol**
Create `internal/replication/pairing.go`. Generate one-time codes, handle key exchange via REST. Store paired nodes in metadata.

**T7 — Sync engine**
Create `internal/replication/sync.go`. Execute rsync over SSH for push/pull/bidirectional. Parse rsync output for progress and stats. Store run history.

**T8 — Sync scheduler**
Add sync job scheduler to daemon. Run jobs on configured intervals. Retry failed jobs with backoff.

**T9 — Sync CLI**
Add `pair` and `sync` subcommands to CLI.

**T10 — Sync API**
Register pairing, sync job, and sync history endpoints.

**T11 — Snapshot UI**
Snapshot panel in pool view: create, list, delete, mount. Schedule configuration dialog.

**T12 — Sync UI**
Dedicated sync tab: paired nodes, sync jobs, create dialogs, run history, live progress.

## Manual Validation Tests (via Web Portal)

All manual testing is performed through the PoolForge web portal. SSH is only used for initial `aws-up.sh` setup.

**T13 — Snapshot lifecycle testing**
Environment: `./test/aws-up.sh --disks 3x10`
1. Create pool via portal with snapshot reserve set to 10%
2. Verify pool status shows LV at 90% of VG, 10% reserved
3. Create share, upload test files via SMB/NFS client, note checksums
4. Create snapshot via portal snapshot panel
5. Verify snapshot appears in snapshot list with timestamp and size
6. Modify/delete original files via client
7. Browse snapshot via hidden read-only share from client, verify original files intact
8. Delete snapshot via portal, verify removed from list

**T14 — Snapshot scheduling testing**
1. Set snapshot schedule via portal: interval 5m, max-age 15m, max-count 3
2. Wait 20 minutes, verify snapshots appear in list on schedule
3. Verify expired snapshots auto-deleted (disappear from list)
4. Verify max-count enforced (never more than 3 visible)

**T15 — Snapshot space exhaustion**
1. Create pool with small snapshot reserve (5%) via portal
2. Create snapshot
3. Upload large amount of data via client to fill COW space
4. Verify portal shows warning/alert about snapshot space
5. Verify auto-pruning kicks in (oldest snapshot removed)
6. Verify pool remains healthy in portal status

**T16 — Snapshot via shares**
1. Create share and upload files via client
2. Create snapshot via portal
3. Browse snapshot as hidden read-only share from client
4. Copy file from snapshot share to local machine to verify recovery workflow

**T17 — Node pairing testing**
Environment: `./test/aws-up.sh --nodes 2 --disks 3x10`
1. Open portal on Node A, go to sync tab, click "Pair New Node"
2. Note the pairing code displayed
3. Open portal on Node B, go to sync tab, click "Pair New Node" → "Join"
4. Enter pairing code from Node A
5. Verify both portals show each other in paired nodes table
6. Verify pairing persists after page refresh

**T18 — One-way sync testing**
1. Create pool and share on both nodes via their portals
2. Upload test files with known content to Node A share via client
3. Create push sync job via Node A portal: Node A → Node B, targeting the share
4. Click "Sync Now", verify progress shown in portal
5. Verify files appear on Node B share (browse via client)
6. Add new file on Node A, sync again, verify only new file transferred
7. Delete file on Node A, sync again, verify deleted on Node B
8. Verify sync history in portal shows correct bytes/files/status/duration

**T19 — Bidirectional sync testing**
1. Create bidirectional sync job via portal
2. Upload file-A to Node A share, upload file-B to Node B share via clients
3. Trigger sync via portal, verify both nodes have both files
4. Modify same file on both nodes (different content, different timestamps)
5. Trigger sync, verify last-write-wins (newer timestamp kept on both sides)
6. Verify sync history reflects bidirectional transfer stats

**T20 — Scheduled sync testing**
1. Create sync job with 2-minute schedule via portal
2. Upload file to source node share via client
3. Wait for scheduled sync (watch sync tab for activity)
4. Verify file appeared on destination node share
5. Disable job via portal toggle, upload another file, verify it does NOT sync
6. Re-enable via portal, verify next scheduled run picks it up

**T21 — Sync failure and recovery**
1. Create sync job, run successfully via portal
2. Stop Node B instance (via AWS console or `aws ec2 stop-instances`)
3. Trigger sync via Node A portal, verify failure shown with error message
4. Restart Node B
5. Trigger sync again via portal, verify success and recovery

**T22 — Sync UI testing**
1. Verify paired nodes table shows name, host, paired date, unpair button
2. Unpair node via portal, verify removed from both sides
3. Re-pair, create sync job via create dialog
4. Verify job list shows name, remote, mode, schedule, last run status
5. Trigger sync, verify live progress bar/indicator updates
6. Verify recent activity log shows history with ✓/✗ status, bytes, duration

**T23 — Full integration test**
1. Two-node setup: create pools, shares, users, snapshots, and sync jobs — all via portal
2. Upload data via SMB client to Node A share
3. Verify sync replicates to Node B (check via Node B portal or client)
4. Create snapshot on Node A via portal
5. Delete data on Node A via client
6. Recover files from snapshot via hidden share
7. Verify monitoring tab shows all activity throughout
8. Tear down: `./test/aws-down.sh`
