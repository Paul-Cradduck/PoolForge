# Phase 5: NAS File Sharing & Monitoring

## Requirements

### Sharing

**R1 — Shared Folders**
Users must be able to create named shared folders within a pool exposed over SMB and/or NFS. Each share is a subdirectory of the pool mount point. Deleting a share deletes the underlying data after a confirmation check (CLI: `--force` flag, UI: confirmation dialog showing directory size).

**R2 — SMB Protocol**
Shares accessible via Samba for Windows, macOS, and Linux clients. Per-share toggles for: guest access, read-only, and network browsing visibility. Samba workgroup and server name configurable via `/etc/poolforge.conf` and the web UI.

**R3 — NFS Protocol**
Shares accessible via NFS (v4 default) for Linux/Unix clients. Per-share client restriction by CIDR or hostname.

**R4 — User Management**
Pool-scoped users with optional global access flag. All-or-nothing access — every user in a pool can access every share in that pool. Users map to POSIX UIDs (poolforge group) and Samba passwords. User creation available in both CLI (password prompt) and web UI (password field). Packages (samba, nfs-kernel-server) pre-installed by install.sh.

**R5 — Service Lifecycle**
PoolForge installs, configures, starts, and stops smbd/nmbd and nfs-kernel-server automatically. Services start when the first share using that protocol is created and stop when the last share using it is deleted.

### Monitoring

**R6 — Disk Performance**
Real-time per-disk and per-array IOPS and throughput (read/write MB/s) from iostat data. Displayed as gauges on a dedicated monitoring tab.

**R7 — Network IO**
Real-time per-interface throughput (in/out) and per-protocol breakdown (SMB vs NFS throughput). Displayed as gauges.

**R8 — Client Connections**
Live list of connected clients: username, IP address, share name, protocol, connection duration. Read-only (no kick/disconnect).

**R9 — Data Retention**
In-memory rolling buffer (5 minutes, 1-second resolution) for live gauges. On-disk log file with 30-day historical data (sampled at lower resolution for storage efficiency). Monitoring tab shows live gauges with recent history on page load.

### Interface

**R10 — CLI**
All share and user operations available via CLI commands.

**R11 — REST API**
All share, user, and monitoring operations exposed via REST API.

**R12 — Web Portal**
Shares panel with CRUD, users panel with add/delete (including password field), protocol status indicators. Dedicated monitoring tab with gauges for disk IO, network IO, and client connection list.

---

## Design

### Data Model

```go
type Share struct {
    Name        string   `json:"name"`
    Path        string   `json:"path"`
    Protocols   []string `json:"protocols"`    // ["smb"], ["nfs"], or ["smb","nfs"]
    NFSClients  string   `json:"nfs_clients"`  // "192.168.1.0/24" or "*"
    SMBPublic   bool     `json:"smb_public"`   // guest access
    SMBBrowsable bool   `json:"smb_browsable"` // visible in network browsing
    ReadOnly    bool     `json:"read_only"`
}

type NASUser struct {
    Name       string `json:"name"`
    UID        int    `json:"uid"`
    PoolID     string `json:"pool_id"`     // scoped to pool
    GlobalAccess bool `json:"global_access"` // access all pools
}

// Added to Pool struct
type Pool struct {
    // ... existing fields ...
    Shares []Share   `json:"shares,omitempty"`
    Users  []NASUser `json:"users,omitempty"`
}

// Monitoring types
type DiskStats struct {
    Device       string  `json:"device"`
    ReadMBps     float64 `json:"read_mbps"`
    WriteMBps    float64 `json:"write_mbps"`
    ReadIOPS     float64 `json:"read_iops"`
    WriteIOPS    float64 `json:"write_iops"`
    Timestamp    int64   `json:"ts"`
}

type NetStats struct {
    Interface    string  `json:"interface"`
    RxMBps       float64 `json:"rx_mbps"`
    TxMBps       float64 `json:"tx_mbps"`
    Protocol     string  `json:"protocol,omitempty"` // "smb", "nfs", or "" for interface
    Timestamp    int64   `json:"ts"`
}

type ClientConnection struct {
    User       string `json:"user"`
    IP         string `json:"ip"`
    Share      string `json:"share"`
    Protocol   string `json:"protocol"`
    ConnectedAt int64 `json:"connected_at"`
}
```

### Directory Layout

```
/mnt/poolforge/mypool/
├── .poolforge/              ← existing metadata backup
├── documents/               ← share
├── media/                   ← share
└── backups/                 ← share
```

### SMB Configuration

PoolForge writes a dedicated include file:

```
/etc/samba/smb.conf          ← adds: include = /etc/samba/poolforge.conf
/etc/samba/poolforge.conf    ← PoolForge-managed
```

Global section in poolforge.conf:
```ini
[global]
   workgroup = WORKGROUP
   server string = PoolForge NAS
```

Per-share section:
```ini
[documents]
   path = /mnt/poolforge/mypool/documents
   browseable = yes
   read only = no
   guest ok = no
   valid users = @poolforge
```

Configurable in `/etc/poolforge.conf`:
```ini
POOLFORGE_SMB_WORKGROUP=WORKGROUP
POOLFORGE_SMB_SERVER_NAME=PoolForge NAS
```

### NFS Configuration

Marker-delimited block in `/etc/exports`:
```
# BEGIN POOLFORGE
/mnt/poolforge/mypool/documents 192.168.1.0/24(rw,sync,no_subtree_check)
/mnt/poolforge/mypool/media *(ro,sync,no_subtree_check)
# END POOLFORGE
```

Runs `exportfs -ra` after changes.

### Monitoring Architecture

```
  ┌─────────────────────────────────────────────────────────┐
  │                  Monitoring Collector                     │
  │                (1-second sample loop)                     │
  │                                                          │
  │  ┌────────────┐  ┌────────────┐  ┌───────────────────┐  │
  │  │  Disk IO   │  │ Network IO │  │    Connections     │  │
  │  │ /proc/     │  │ /proc/net/ │  │ smbstatus          │  │
  │  │ diskstats  │  │ dev + ss   │  │ showmount/nfsstat  │  │
  │  └─────┬──────┘  └─────┬──────┘  └────────┬──────────┘  │
  │        │               │                   │             │
  │        ▼               ▼                   ▼             │
  │  ┌──────────────────────────────────────────────────┐    │
  │  │         Ring Buffer (5 min, 1s resolution)       │    │
  │  │              300 samples per metric               │    │
  │  └──────────────────────┬───────────────────────────┘    │
  │                         │                                │
  │                    every 60s                              │
  │                         ▼                                │
  │  ┌──────────────────────────────────────────────────┐    │
  │  │     Disk Log (30 days, 1-min resolution)         │    │
  │  │     /var/lib/poolforge/metrics.log                │    │
  │  │     Auto-rotated, old entries pruned              │    │
  │  └──────────────────────────────────────────────────┘    │
  └─────────────────────────────────────────────────────────┘
                            │
                       SSE stream
                            ▼
  ┌─────────────────────────────────────────────────────────┐
  │                   Monitoring Tab                         │
  │                                                          │
  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐   │
  │  │ Disk R/W │ │ Disk IOPS│ │ Net In/  │ │ SMB/NFS  │   │
  │  │  Gauge   │ │  Gauge   │ │ Out Gauge│ │  Gauge   │   │
  │  └──────────┘ └──────────┘ └──────────┘ └──────────┘   │
  │                                                          │
  │  Connected Clients ─────────────────────────────────     │
  │  bob    192.168.1.10   documents   SMB   2h 15m         │
  │  alice  192.168.1.22   media       NFS   45m            │
  └─────────────────────────────────────────────────────────┘
```

Data sources:
- Disk IO: `/proc/diskstats` (parsed, delta between samples)
- Network IO: `/proc/net/dev` (parsed, delta between samples)
- Protocol breakdown: `ss` filtered by port 445 (SMB) and 2049 (NFS) byte counters
- SMB clients: `smbstatus --json` or parsed text output
- NFS clients: `/proc/fs/nfsd/clients/` or `ss` on port 2049

### API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/pools/{id}/shares` | Create share |
| GET | `/api/pools/{id}/shares` | List shares |
| PUT | `/api/pools/{id}/shares/{name}` | Update share |
| DELETE | `/api/pools/{id}/shares/{name}` | Delete share (requires `?force=true`) |
| POST | `/api/users` | Create user `{"name","password","pool_id","global_access"}` |
| GET | `/api/users` | List users |
| DELETE | `/api/users/{name}` | Delete user |
| GET | `/api/monitoring/live` | SSE stream of real-time metrics |
| GET | `/api/monitoring/history?range=1h` | Historical metrics from disk log |
| GET | `/api/monitoring/clients` | Current client connections |

### CLI Commands

```bash
# Shares
poolforge share create <pool> --name <name> --protocols smb,nfs \
    [--nfs-clients "192.168.1.0/24"] [--smb-public] [--smb-hidden] [--read-only]
poolforge share list <pool>
poolforge share update <pool> --name <name> [--protocols ...] [--read-only] [--smb-hidden]
poolforge share delete <pool> --name <name> [--force]

# Users
poolforge user add --name <name> --pool <pool> [--global]   # prompts for password
poolforge user delete --name <name>
poolforge user list [--pool <pool>]
```

---

## Tasks

### Sharing

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

### Monitoring

**T7 — Metrics collector**
Create `internal/monitoring/collector.go`. 1-second sample loop reading `/proc/diskstats` and `/proc/net/dev`. Computes deltas for rates. Stores in ring buffer (5 min / 300 samples).

**T8 — Protocol & connection tracking**
Parse `smbstatus` for SMB clients. Parse `ss` on port 2049 for NFS clients. Use `ss` byte counters filtered by port for per-protocol throughput.

**T9 — Disk log**
Create `internal/monitoring/disklog.go`. Every 60s, append averaged sample to `/var/lib/poolforge/metrics.log`. Prune entries older than 30 days on startup and daily.

**T10 — Monitoring SSE endpoint**
Add `/api/monitoring/live` SSE stream pushing latest metrics every second. Add `/api/monitoring/history` returning disk log data for requested time range. Add `/api/monitoring/clients` returning current connections.

### Integration

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

---

## Out of Scope (Future Phases)
- LDAP / Active Directory integration
- iSCSI targets
- FTP / SFTP
- Per-user quotas
- Per-share user ACLs (currently all-or-nothing per pool)
- Time Machine backup targets
- Recycle bin / snapshots
- Client kick/disconnect from UI
- Historical graphs (only gauges + raw log in this phase)
