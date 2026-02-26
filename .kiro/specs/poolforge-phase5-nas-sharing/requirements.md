# Phase 5: NAS File Sharing & Monitoring — Requirements

## Sharing

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

## Monitoring

**R6 — Disk Performance**
Real-time per-disk and per-array IOPS and throughput (read/write MB/s) from iostat data. Displayed as gauges on a dedicated monitoring tab.

**R7 — Network IO**
Real-time per-interface throughput (in/out) and per-protocol breakdown (SMB vs NFS throughput). Displayed as gauges.

**R8 — Client Connections**
Live list of connected clients: username, IP address, share name, protocol, connection duration. Read-only (no kick/disconnect).

**R9 — Data Retention**
In-memory rolling buffer (5 minutes, 1-second resolution) for live gauges. On-disk log file with 30-day historical data (sampled at lower resolution for storage efficiency). Monitoring tab shows live gauges with recent history on page load.

## Interface

**R10 — CLI**
All share and user operations available via CLI commands.

**R11 — REST API**
All share, user, and monitoring operations exposed via REST API.

**R12 — Web Portal**
Shares panel with CRUD, users panel with add/delete (including password field), protocol status indicators. Dedicated monitoring tab with gauges for disk IO, network IO, and client connection list.

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
