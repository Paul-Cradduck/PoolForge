# Phase 7: High Availability — Design

## Architecture

```
  ┌──────────────────────────┐              ┌──────────────────────────┐
  │    Node A (Active)       │              │    Node B (Passive)      │
  │                          │   rsync/SSH  │                          │
  │  ┌────────────────────┐  │─────30s─────▶│  ┌────────────────────┐  │
  │  │ Pool: mypool       │  │              │  │ Pool: replicapool  │  │
  │  │ SMB/NFS: serving   │  │◀────1s──────│  │ SMB/NFS: standby   │  │
  │  └────────────────────┘  │  heartbeat   │  └────────────────────┘  │
  │                          │              │                          │
  │  ┌────────────────────┐  │              │  ┌────────────────────┐  │
  │  │ HA Manager         │  │              │  │ HA Manager         │  │
  │  │ - role: active     │  │              │  │ - role: passive    │  │
  │  │ - sync loop        │  │              │  │ - heartbeat loop   │  │
  │  │ - heartbeat resp   │  │              │  │ - failover logic   │  │
  │  └────────────────────┘  │              │  └────────────────────┘  │
  └────────────┬─────────────┘              └────────────┬─────────────┘
               │                                         │
               └──────── Floating IP: x.x.x.x ──────────┘
                         (assigned to active)
```

## Data Model

### Configuration (in `/etc/poolforge.conf`)

```ini
POOLFORGE_HA_ENABLED=true
POOLFORGE_HA_ROLE=active          # active | passive | standalone
POOLFORGE_HA_PARTNER=e336449353ad61f2   # PairedNode ID
POOLFORGE_HA_LOCAL_POOL=3df12052-fb1f-9c18-0000000000000000
POOLFORGE_HA_REMOTE_POOL=/mnt/poolforge/replicapool
POOLFORGE_HA_SYNC_INTERVAL=30     # seconds
POOLFORGE_HA_HEARTBEAT_INTERVAL=1 # seconds
POOLFORGE_HA_FAILURE_THRESHOLD=3  # consecutive misses before failover
POOLFORGE_HA_FLOATING_IP=         # Elastic IP allocation ID or virtual IP CIDR
POOLFORGE_HA_FENCING_METHOD=aws   # aws | command | none
POOLFORGE_HA_FENCING_CMD=         # custom fencing command (if method=command)
POOLFORGE_HA_INSTANCE_ID=         # AWS instance ID of partner (for fencing)
```

### HA State (runtime, in-memory + persisted to `/var/lib/poolforge/ha_state.json`)

```go
type HAState struct {
    Role              string    `json:"role"`               // active, passive, standalone
    PartnerNodeID     string    `json:"partner_node_id"`
    PartnerReachable  bool      `json:"partner_reachable"`
    LastSyncAt        int64     `json:"last_sync_at"`       // unix timestamp
    LastHeartbeatAt   int64     `json:"last_heartbeat_at"`
    ReplicationLagSec int       `json:"replication_lag_sec"`
    InitialSyncDone   bool      `json:"initial_sync_done"`
    FailoverHistory   []Failover `json:"failover_history"`
}

type Failover struct {
    Timestamp   int64  `json:"timestamp"`
    FromRole    string `json:"from_role"`
    ToRole      string `json:"to_role"`
    Reason      string `json:"reason"`    // "heartbeat_timeout", "manual", "failback"
    FencingOK   bool   `json:"fencing_ok"`
}
```

## HA Manager (`internal/replication/ha.go`)

New `HAManager` struct, created in `main.go` alongside existing `PairingManager` and `SyncManager`.

```go
type HAManager struct {
    mu          sync.Mutex
    state       HAState
    pairing     *PairingManager
    syncMgr     *SyncManager
    stopCh      chan struct{}
    engine      engine.Engine
}
```

### Startup Flow

```
main.go starts
    │
    ▼
Read HA config from /etc/poolforge.conf
    │
    ├── HA disabled → do nothing (standalone)
    │
    ├── Role = active
    │   ├── Start continuous sync loop (goroutine)
    │   ├── Start heartbeat responder (via /api/health)
    │   └── Ensure SMB/NFS shares are serving
    │
    └── Role = passive
        ├── Start heartbeat monitor loop (goroutine)
        ├── Ensure SMB/NFS shares are stopped
        └── Wait for failover trigger
```

## Health Endpoint

`GET /api/health` — no auth required

```json
{
    "role": "active",
    "uptime_sec": 86400,
    "pool_state": "healthy",
    "last_sync_at": 1773364116,
    "replication_lag_sec": 12,
    "initial_sync_done": true
}
```

## Continuous Sync Loop (Active Node)

```
every SYNC_INTERVAL seconds:
    │
    ▼
  rsyncWithProgress(localPool → remotePool)
    │
    ├── success → update last_sync_at, reset lag counter
    │
    └── failure → log error, increment failure count
                  if 3 consecutive failures → alert "replication degraded"
```

Uses existing `rsyncWithProgress()` from sync.go. The HA sync is a dedicated internal job, separate from user-created sync jobs.

## Heartbeat Monitor (Passive Node)

```
every HEARTBEAT_INTERVAL seconds:
    │
    ▼
  GET http://<active_host>:8080/api/health
    │
    ├── success (HTTP 200)
    │   ├── reset failure counter
    │   ├── update partner_reachable = true
    │   └── update last_heartbeat_at
    │
    └── failure (timeout / connection refused / non-200)
        ├── increment failure counter
        ├── if failures >= FAILURE_THRESHOLD
        │   └── begin failover evaluation
        └── else → log warning, continue monitoring
```

Heartbeat uses direct HTTP (not SSH) for speed. The passive node connects to the active's private IP on port 8080.

## Failover Sequence

```
Passive detects active unreachable (3 consecutive failures)
    │
    ▼
Split-brain check: can I reach the default gateway?
    │
    ├── NO → I'm isolated, do NOT failover, log critical alert
    │
    └── YES → network is fine, active is truly down
        │
        ▼
    Fencing: attempt to stop old active
        │
        ├── AWS: aws ec2 stop-instances --instance-ids <partner>
        ├── Command: run configured fencing command
        └── None: skip (log warning)
        │
        ▼
    Promote self to active:
        1. Set role = active in conf + state
        2. Start SMB/NFS shares
        3. Reassign floating IP (if configured)
        4. Log failover event to history
        5. Stop heartbeat monitor, start sync loop
        │
        ▼
    Active and serving clients
```

## Floating IP Management

### AWS Elastic IP

```go
func reassignElasticIP(allocationID string) error {
    // Get our instance ID from metadata
    instanceID := getEC2InstanceID()
    // Associate the EIP to ourselves
    exec.Command("aws", "ec2", "associate-address",
        "--allocation-id", allocationID,
        "--instance-id", instanceID,
        "--allow-reassociation").Run()
}
```

### Virtual IP (on-prem)

```go
func addVirtualIP(cidr, iface string) error {
    return exec.Command("ip", "addr", "add", cidr, "dev", iface).Run()
}

func removeVirtualIP(cidr, iface string) error {
    return exec.Command("ip", "addr", "del", cidr, "dev", iface).Run()
}
```

## Failback Sequence (Manual)

```
Admin clicks "Failback" on current active node's UI
    │
    ▼
Verify recovered node is online and synced
    │
    ├── NOT synced → reject, show "partner not ready"
    │
    └── Synced and healthy
        │
        ▼
    1. Current active stops shares
    2. Final sync to ensure partner is fully caught up
    3. Current active sets role = passive
    4. Notify partner to promote to active
    5. Partner sets role = active, starts shares
    6. Floating IP moves to new active
    7. Log failback event
```

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/health` | Health check (no auth) |
| GET | `/api/ha/status` | Full HA status |
| POST | `/api/ha/enable` | Enable HA with config |
| POST | `/api/ha/disable` | Disable HA, revert to standalone |
| POST | `/api/ha/failover` | Manual failover (force promote) |
| POST | `/api/ha/failback` | Manual failback (switchover) |
| GET | `/api/ha/history` | Failover event history |

## UI Layout

### HA Status Card (Dashboard)

```
┌─────────────────────────────────────────────────────┐
│  🔄 High Availability                               │
│                                                     │
│  Role: ● Active                                     │
│  Partner: Poolforge Node B (10.0.48.11) ● Online    │
│  Replication Lag: 12s                               │
│  Last Sync: 12s ago                                 │
│  Floating IP: 3.15.223.87                           │
│                                                     │
│  [Manual Failover]                                  │
└─────────────────────────────────────────────────────┘
```

### HA Settings (Settings Page or Dedicated Nav)

```
┌─────────────────────────────────────────────────────┐
│  ⚡ High Availability Configuration                  │
│                                                     │
│  Enable HA:        [toggle]                         │
│  Role:             [Active ▼]                       │
│  Partner Node:     [Poolforge Node B ▼]             │
│  Local Pool:       [mypool ▼]                       │
│  Remote Pool:      [replicapool ▼]                  │
│                                                     │
│  Sync Interval:    [30] seconds                     │
│  Heartbeat:        [5] seconds                      │
│  Failure Threshold:[3] misses                       │
│                                                     │
│  Floating IP:      [eipalloc-xxxxx]                 │
│  Fencing Method:   [AWS ▼]                          │
│  Partner Instance: [i-021abe4942f0f501c]            │
│                                                     │
│  [Save HA Configuration]                            │
└─────────────────────────────────────────────────────┘
```

## File Changes

| File | Changes |
|------|---------|
| `internal/replication/ha.go` | New — HAManager, sync loop, heartbeat, failover, failback |
| `internal/engine/types.go` | Add HAState, Failover structs |
| `internal/api/server.go` | Add /api/health, /api/ha/* routes and handlers |
| `internal/api/static/index.html` | HA status card on dashboard, HA settings UI, failover controls |
| `cmd/poolforge/main.go` | Initialize HAManager on startup, wire to server |
| `/etc/poolforge.conf` | New POOLFORGE_HA_* keys |
