# Phase 5: NAS File Sharing

## Requirements

### R1 — Shared Folders
Users must be able to create named shared folders within a pool that are exposed over the network via SMB and/or NFS. Each share is a subdirectory of the pool mount point.

### R2 — SMB Protocol
Shares must be accessible via SMB (Samba) for Windows, macOS, and Linux clients. Shares support authenticated access via PoolForge-managed users or optional guest access.

### R3 — NFS Protocol
Shares must be accessible via NFS for Linux/Unix clients. Each share supports client restrictions by CIDR or hostname. NFSv4 by default.

### R4 — User Management
PoolForge must manage its own user database for SMB authentication. Users map to POSIX UIDs for consistent file ownership. No external directory services (LDAP/AD) in this phase.

### R5 — CLI
All share and user operations must be available via CLI commands.

### R6 — REST API
All share and user operations must be exposed via the existing REST API with basic auth.

### R7 — Web Portal
The dashboard must include panels for managing shares and users, with protocol status indicators.

### R8 — Service Lifecycle
PoolForge must install, configure, start, and stop smbd/nmbd and nfs-kernel-server automatically as shares are created or deleted.

---

## Design

### Data Model

```go
// Added to pool metadata
type Share struct {
    Name       string   `json:"name"`        // "documents"
    Path       string   `json:"path"`        // "/mnt/poolforge/mypool/documents"
    Protocols  []string `json:"protocols"`   // ["smb"], ["nfs"], or ["smb","nfs"]
    NFSClients string   `json:"nfs_clients"` // "192.168.1.0/24" or "*"
    SMBPublic  bool     `json:"smb_public"`  // guest access
    ReadOnly   bool     `json:"read_only"`
}

type NASUser struct {
    Name string `json:"name"`
    UID  int    `json:"uid"`
}

// Added to Pool struct
type Pool struct {
    // ... existing fields ...
    Shares []Share   `json:"shares,omitempty"`
    Users  []NASUser `json:"users,omitempty"`
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

PoolForge writes a dedicated include file rather than editing smb.conf directly:

```
/etc/samba/smb.conf          ← system config, adds: include = /etc/samba/poolforge.conf
/etc/samba/poolforge.conf    ← PoolForge-managed shares
```

Generated per share:
```ini
[documents]
   path = /mnt/poolforge/mypool/documents
   browseable = yes
   read only = no
   guest ok = no
   valid users = @poolforge
```

Users are added to Samba via `smbpasswd` and to a `poolforge` POSIX group for file permissions.

### NFS Configuration

PoolForge appends exports with marker comments to `/etc/exports`:

```
# BEGIN POOLFORGE
/mnt/poolforge/mypool/documents 192.168.1.0/24(rw,sync,no_subtree_check)
/mnt/poolforge/mypool/media *(ro,sync,no_subtree_check)
# END POOLFORGE
```

Runs `exportfs -ra` after changes.

### API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/pools/{id}/shares` | Create share |
| GET | `/api/pools/{id}/shares` | List shares |
| PUT | `/api/pools/{id}/shares/{name}` | Update share |
| DELETE | `/api/pools/{id}/shares/{name}` | Delete share |
| POST | `/api/users` | Create user |
| GET | `/api/users` | List users |
| DELETE | `/api/users/{name}` | Delete user |

### CLI Commands

```bash
# Shares
poolforge share create <pool> --name <name> --protocols smb,nfs [--nfs-clients "192.168.1.0/24"] [--smb-public] [--read-only]
poolforge share list <pool>
poolforge share update <pool> --name <name> [--protocols ...] [--read-only]
poolforge share delete <pool> --name <name>

# Users
poolforge user add --name <name>       # prompts for password
poolforge user delete --name <name>
poolforge user list
```

### Service Management

- `share create` with SMB → ensure samba installed, write config, start smbd/nmbd
- `share create` with NFS → ensure nfs-kernel-server installed, write exports, start nfs-server
- `share delete` (last SMB share removed) → stop smbd/nmbd
- `share delete` (last NFS share removed) → stop nfs-server
- `install.sh` updated to include samba and nfs-kernel-server as optional dependencies

### Web Portal Additions

- **Shares panel**: table of shares with name, protocols, access, actions (edit/delete)
- **Create share dialog**: name, protocol checkboxes, NFS client field, guest toggle, read-only toggle
- **Users panel**: table with add/delete
- **Status bar**: SMB ✓/✗, NFS ✓/✗ indicators next to existing safety status

---

## Tasks

### T1 — Data model & types
- Add `Share` and `NASUser` structs to `internal/engine/types.go`
- Add `Shares` and `Users` fields to `Pool` struct

### T2 — Share manager
- Create `internal/sharing/manager.go` — `ShareManager` interface
- Methods: `CreateShare`, `DeleteShare`, `UpdateShare`, `ListShares`
- Creates subdirectory, sets ownership/permissions

### T3 — SMB backend
- Create `internal/sharing/smb.go`
- Write `/etc/samba/poolforge.conf` from share list
- Add `include` line to `/etc/samba/smb.conf` if missing
- Start/stop smbd/nmbd based on active SMB shares

### T4 — NFS backend
- Create `internal/sharing/nfs.go`
- Write marker-delimited block in `/etc/exports`
- Run `exportfs -ra` after changes
- Start/stop nfs-server based on active NFS shares

### T5 — User management
- Create `internal/sharing/users.go`
- Create POSIX user + add to `poolforge` group
- Set Samba password via `smbpasswd`
- Delete user from both POSIX and Samba
- Store user list in pool metadata

### T6 — Engine integration
- Add share/user methods to `EngineService` interface
- Wire `ShareManager` into engine, delegate calls
- Save/load shares and users via existing metadata store

### T7 — CLI commands
- Add `share create/list/update/delete` subcommands to `cmd/poolforge/main.go`
- Add `user add/delete/list` subcommands
- Password prompt for `user add`

### T8 — REST API
- Register share and user endpoints in `internal/api/server.go`
- JSON request/response matching the data model

### T9 — Web portal
- Shares panel with CRUD
- Users panel with add/delete
- Protocol status indicators
- Create share dialog with protocol/access options

### T10 — Install script update
- Add `samba` and `nfs-kernel-server` to `install.sh` package list

---

## Out of Scope (Future Phases)
- LDAP / Active Directory integration
- iSCSI targets
- FTP / SFTP
- Per-user quotas
- Fine-grained ACLs
- Time Machine backup targets
- Recycle bin / snapshots
