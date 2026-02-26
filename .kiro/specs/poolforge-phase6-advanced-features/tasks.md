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

## Manual Validation Tests

**T13 — Snapshot lifecycle testing**
Environment: `./test/aws-up.sh --disks 3x10`
1. Create pool with `--snapshot-reserve 10`
2. Verify LV is 90% of VG, 10% free for snapshots
3. Write test files with known checksums
4. Create snapshot, verify appears in `snapshot list`
5. Modify/delete original files
6. Mount snapshot read-only, verify original files intact with correct checksums
7. Delete snapshot, verify mount removed and LV space reclaimed

**T14 — Snapshot scheduling testing**
1. Set snapshot schedule: interval 5m, max-age 15m, max-count 3
2. Wait 20 minutes, verify snapshots created on schedule
3. Verify expired snapshots auto-deleted
4. Verify max-count enforced (never more than 3)

**T15 — Snapshot space exhaustion**
1. Create pool with small snapshot reserve (5%)
2. Create snapshot
3. Write large amount of data to original LV to fill COW space
4. Verify auto-pruning kicks in and logs warning
5. Verify pool remains healthy after pruning

**T16 — Snapshot via shares**
1. Create SMB/NFS share
2. Create snapshot
3. Verify snapshot browsable as hidden read-only share
4. Copy file from snapshot share to verify recovery workflow

**T17 — Node pairing testing**
Environment: `./test/aws-up.sh --nodes 2 --disks 3x10`
1. Run `poolforge pair init` on Node A, note pairing code
2. Run `poolforge pair join <code>@<nodeA-ip>:8080` on Node B
3. Verify both nodes show each other in `pair list`
4. Verify SSH key exchange worked (Node B can SSH to Node A as poolforge user)
5. Pair Node A with a third node (if available), verify multi-node works

**T18 — One-way sync testing**
1. Create pool and share on both nodes
2. Write test files with checksums on Node A
3. Create push sync job: Node A → Node B, targeting the share
4. Run sync manually, verify files appear on Node B with correct checksums
5. Add new file on Node A, run sync again, verify only new file transferred (delta)
6. Delete file on Node A, run sync, verify deleted on Node B (`--delete`)
7. Verify sync history shows correct bytes/files/status

**T19 — Bidirectional sync testing**
1. Create bidirectional sync job between Node A and Node B
2. Write file-A on Node A, write file-B on Node B
3. Run sync, verify both nodes have both files
4. Modify same file on both nodes (different content, different timestamps)
5. Run sync, verify last-write-wins (newer timestamp kept on both sides)
6. Verify sync history reflects bidirectional transfer

**T20 — Scheduled sync testing**
1. Create sync job with 2-minute schedule
2. Write file on source node
3. Wait for scheduled sync to run
4. Verify file appeared on destination
5. Disable job, write another file, verify it does NOT sync
6. Re-enable, verify next scheduled run picks it up

**T21 — Sync failure and recovery**
1. Create sync job, run successfully
2. Stop Node B (simulate network failure)
3. Trigger sync, verify it fails with error logged
4. Start Node B
5. Trigger sync again, verify it succeeds and recovers

**T22 — Sync UI testing**
1. Verify paired nodes table shows correct info
2. Pair/unpair via UI, verify works
3. Create sync job via UI, verify appears in job list
4. Trigger sync via UI, verify live progress updates
5. Verify recent activity log shows sync history with status/bytes/duration

**T23 — Full integration test**
1. Two-node setup with pools, shares, users, snapshots, and sync
2. Write data via SMB from external client to Node A share
3. Verify sync replicates to Node B
4. Create snapshot on Node A
5. Delete data on Node A
6. Recover from snapshot
7. Verify monitoring shows all activity
8. Tear down: `./test/aws-down.sh`
