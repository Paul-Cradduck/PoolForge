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
