# Phase 6: Advanced Features — Snapshots & Replication

## Requirements

### Snapshots

**R1 — LVM Snapshots**
Users can create point-in-time snapshots of a pool's logical volume. Snapshots are copy-on-write using LVM's native snapshot support. Pool creation reserves a configurable percentage of VG space for snapshots (default 10%).

**R2 — Snapshot Lifecycle**
Snapshots can be created on-demand (CLI/UI) or on a schedule. Each snapshot has a configurable max age — auto-deleted after expiry. Snapshots can be manually deleted at any time. If snapshot space fills up, the oldest snapshot is pruned automatically.

**R3 — Snapshot Access**
Snapshots are mountable read-only at `/mnt/poolforge/<pool>/.snapshots/<name>/` for file recovery. Users can browse and copy files from snapshots via the share protocols (exposed as a hidden read-only share).

### Replication

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

### Interface

**R9 — CLI**
All snapshot and replication operations available via CLI.

**R10 — REST API**
All snapshot and replication operations exposed via REST API.

**R11 — Web Portal**
Snapshot management in the pool view. Dedicated sync tab for replication.

---

## Design

### Snapshots

#### Data Model

```go
type SnapshotConfig struct {
    ReservePercent int `json:"reserve_percent"` // % of VG for snapshots (default 10)
}

type Snapshot struct {
    Name      string    `json:"name"`
    CreatedAt time.Time `json:"created_at"`
    ExpiresAt time.Time `json:"expires_at,omitempty"`
    SizeBytes uint64    `json:"size_bytes"` // space consumed by COW blocks
    MountPath string    `json:"mount_path"`
}

type SnapshotSchedule struct {
    Interval string `json:"interval"` // "15m", "1h", "24h"
    MaxAge   string `json:"max_age"`  // "24h", "7d", "30d"
    MaxCount int    `json:"max_count"`
}
```

#### Pool Creation Change

```
Current:   lvcreate -l 100%FREE
With snap: lvcreate -l 90%FREE   (reserves 10% for snapshot COW space)
```

#### Snapshot Flow

```
  Create snapshot
       │
       ▼
  lvcreate --snapshot -L <size> -n snap_<timestamp> /dev/vg/lv
       │
       ▼
  mount -o ro /dev/vg/snap_<timestamp> /mnt/poolforge/<pool>/.snapshots/<name>/
       │
       ▼
  Available for file browsing / recovery
       │
       ▼
  On expiry or manual delete: umount → lvremove
```

#### Auto-Pruning

When snapshot space runs low (>80% of reserved space consumed):
1. Delete oldest expired snapshot
2. If still full, delete oldest snapshot regardless of expiry
3. Log warning via alert engine

### Replication

#### Data Model

```go
type PairedNode struct {
    ID        string `json:"id"`         // unique node identifier
    Name      string `json:"name"`       // friendly name
    Host      string `json:"host"`       // IP or hostname
    Port      int    `json:"port"`       // SSH port (default 22)
    PairedAt  int64  `json:"paired_at"`
    PublicKey string `json:"public_key"` // their SSH public key
}

type SyncJob struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    RemoteNode  string `json:"remote_node"`  // PairedNode.ID
    LocalPool   string `json:"local_pool"`   // pool ID
    RemotePool  string `json:"remote_pool"`  // pool ID on remote
    Shares      []string `json:"shares"`     // empty = entire pool
    Mode        string `json:"mode"`         // "push", "pull", "bidirectional"
    Schedule    string `json:"schedule"`     // cron-like or interval: "*/15 * * * *", "1h", ""
    Enabled     bool   `json:"enabled"`
}

type SyncRun struct {
    JobID        string `json:"job_id"`
    StartedAt    int64  `json:"started_at"`
    FinishedAt   int64  `json:"finished_at,omitempty"`
    BytesSent    uint64 `json:"bytes_sent"`
    BytesRecv    uint64 `json:"bytes_recv"`
    FilesChanged int    `json:"files_changed"`
    Status       string `json:"status"` // "running", "success", "failed"
    Error        string `json:"error,omitempty"`
}
```

#### Pairing Flow

```
  Node A                                    Node B
  ──────                                    ──────
  poolforge pair init
       │
       ├─▶ Generate SSH keypair (if needed)
       ├─▶ Generate one-time code (6-digit + node address)
       ├─▶ Display: "Pairing code: 482916@192.168.1.10:8080"
       │
       │                              poolforge pair join 482916@192.168.1.10:8080
       │                                    │
       │◀── POST /api/pair/exchange ────────┤
       │    {code, public_key, name, host}  │
       │                                    │
       ├──▶ Verify code                     │
       ├──▶ Store Node B's public key       │
       ├──▶ Return Node A's public key ────▶├──▶ Store Node A's public key
       │                                    │
       ▼                                    ▼
  Paired ✓                            Paired ✓
  (SSH keys installed in              (SSH keys installed in
   ~/.poolforge/authorized_keys)       ~/.poolforge/authorized_keys)
```

#### Sync Execution

```
  One-way (push):
    rsync -avz --delete -e "ssh -i <key>" \
      /mnt/poolforge/<pool>/<share>/ \
      poolforge@<remote>:/mnt/poolforge/<pool>/<share>/

  Bidirectional:
    Step 1: rsync pull (remote → local) with --update (skip newer local)
    Step 2: rsync push (local → remote) with --update (skip newer remote)
    Result: both sides have the newest version of every file
```

#### Sync Tab UI

```
┌─────────────────────────────────────────────────────────────────┐
│  Sync                                                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  Paired Nodes ──────────────────────────────────────────        │
│  ┌──────────────┬─────────────────┬──────────┬────────┐        │
│  │ Name         │ Host            │ Paired   │        │        │
│  ├──────────────┼─────────────────┼──────────┼────────┤        │
│  │ office-nas   │ 192.168.1.10    │ 2d ago   │ [Unpair]│       │
│  │ backup-site  │ 10.0.0.50       │ 14d ago  │ [Unpair]│       │
│  └──────────────┴─────────────────┴──────────┴────────┘        │
│  [Pair New Node]                                                │
│                                                                 │
│  Sync Jobs ─────────────────────────────────────────────        │
│  ┌────────────┬───────────┬──────┬──────────┬──────────┐       │
│  │ Name       │ Remote    │ Mode │ Schedule │ Last Run │       │
│  ├────────────┼───────────┼──────┼──────────┼──────────┤       │
│  │ docs-sync  │ office-nas│ bidir│ 15m      │ ✓ 3m ago │       │
│  │ full-backup│ backup-sit│ push │ daily    │ ✓ 8h ago │       │
│  └────────────┴───────────┴──────┴──────────┴──────────┘       │
│  [Create Sync Job]                                              │
│                                                                 │
│  Recent Activity ───────────────────────────────────────        │
│  09:15 docs-sync     → office-nas   ✓ 142 files, 28 MB  12s   │
│  09:00 docs-sync     → office-nas   ✓ 3 files, 1.2 MB   2s   │
│  01:00 full-backup   → backup-site  ✓ 8,412 files, 12 GB 4m   │
│  00:45 full-backup   → backup-site  ✗ Connection refused       │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/pools/{id}/snapshots` | Create snapshot |
| GET | `/api/pools/{id}/snapshots` | List snapshots |
| DELETE | `/api/pools/{id}/snapshots/{name}` | Delete snapshot |
| PUT | `/api/pools/{id}/snapshots/schedule` | Set snapshot schedule |
| POST | `/api/pair/init` | Generate pairing code |
| POST | `/api/pair/exchange` | Complete pairing (called by remote) |
| GET | `/api/pair/nodes` | List paired nodes |
| DELETE | `/api/pair/nodes/{id}` | Unpair node |
| POST | `/api/sync/jobs` | Create sync job |
| GET | `/api/sync/jobs` | List sync jobs |
| PUT | `/api/sync/jobs/{id}` | Update sync job |
| DELETE | `/api/sync/jobs/{id}` | Delete sync job |
| POST | `/api/sync/jobs/{id}/run` | Trigger sync now |
| GET | `/api/sync/jobs/{id}/history` | Get sync run history |

### CLI Commands

```bash
# Snapshots
poolforge snapshot create <pool> --name <name> [--expires 24h]
poolforge snapshot list <pool>
poolforge snapshot delete <pool> --name <name>
poolforge snapshot mount <pool> --name <name>
poolforge snapshot schedule <pool> --interval 1h --max-age 7d --max-count 24

# Pairing
poolforge pair init                          # shows pairing code
poolforge pair join <code>@<host:port>       # pair with remote node
poolforge pair list                          # list paired nodes
poolforge pair remove <node-id>              # unpair

# Sync
poolforge sync create --name <name> --remote <node> --local-pool <pool> \
    --remote-pool <pool> --mode push|pull|bidirectional \
    [--shares docs,media] [--schedule 1h]
poolforge sync list
poolforge sync run <job-id>                  # trigger now
poolforge sync history <job-id>
poolforge sync delete <job-id>
```

---

## Tasks

### Snapshots

**T1 — Pool creation change**
Modify `CreatePool` to accept `--snapshot-reserve` (default 10%). Create LV at `(100 - reserve)%FREE` instead of `100%FREE`. Store reserve config in pool metadata.

**T2 — Snapshot manager**
Create `internal/snapshots/manager.go`. Create/delete/list/mount LVM snapshots. Calculate snapshot space usage from `lvs` output.

**T3 — Snapshot scheduling & pruning**
Add snapshot scheduler to safety daemon. Create snapshots on interval, delete expired ones. Auto-prune oldest when space runs low.

**T4 — Snapshot access**
Mount snapshots read-only. Optionally expose as hidden read-only SMB/NFS share for file recovery.

### Replication

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

---

## Out of Scope (Future Phases)
- ZFS/Btrfs migration for native snapshots
- Incremental block-level replication (ZFS send/receive)
- Real-time / continuous sync (inotify-based)
- Multi-master conflict resolution beyond last-write-wins
- Snapshot-based backup to cloud (S3, B2)
- Encryption of sync transport beyond SSH
