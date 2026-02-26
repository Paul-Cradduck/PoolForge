# Phase 5: NAS File Sharing & Monitoring — Design

## Data Model

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

## Directory Layout

```
/mnt/poolforge/mypool/
├── .poolforge/              ← existing metadata backup
├── documents/               ← share
├── media/                   ← share
└── backups/                 ← share
```

## SMB Configuration

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

## NFS Configuration

Marker-delimited block in `/etc/exports`:
```
# BEGIN POOLFORGE
/mnt/poolforge/mypool/documents 192.168.1.0/24(rw,sync,no_subtree_check)
/mnt/poolforge/mypool/media *(ro,sync,no_subtree_check)
# END POOLFORGE
```

Runs `exportfs -ra` after changes.

## Monitoring Architecture

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

## API Endpoints

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

## CLI Commands

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
