# Phase 7: High Availability — Requirements

## HA Cluster

**R1 — HA Cluster Formation**
Two paired PoolForge nodes can form a high-availability cluster. One node is designated "active" (serves clients), the other is "passive" (standby). HA is enabled and configured entirely via the web UI. A node can only be in one HA cluster at a time.

**R2 — Cluster Roles**
- **Active**: serves SMB/NFS shares, accepts writes, runs safety daemon, continuously replicates data to passive
- **Passive**: receives replicated data, monitors active node health, does not serve shares to clients, ready to promote at any time
- **Standalone**: default state, not part of any HA cluster (current behavior)

**R3 — Role Persistence**
HA role and cluster configuration survive reboots. Stored in `/etc/poolforge.conf`. On startup, a node resumes its last known role and begins heartbeat/replication accordingly.

## Continuous Replication

**R4 — Automatic Continuous Sync**
When HA is enabled, the active node automatically replicates its pool data to the passive node on a configurable interval (default 30 seconds). Uses rsync over SSH (existing infrastructure). No manual sync job creation required — HA creates and manages its own internal sync.

**R5 — Replication Lag Tracking**
The system tracks and displays replication lag — the time since the last successful sync to the passive node. Displayed on both nodes' dashboards. Alerts if lag exceeds a configurable threshold (default 5 minutes).

**R6 — Initial Full Sync**
When HA is first enabled, a full sync runs from active to passive. The UI shows progress. HA failover is not armed until the initial sync completes.

## Health Monitoring

**R7 — Heartbeat**
The passive node sends a health check to the active node every 1 second via HTTP (`/api/health`). The active node also monitors the passive. Both nodes track consecutive heartbeat failures.

**R8 — Health Endpoint**
`GET /api/health` returns node role, uptime, last sync timestamp, pool state, and replication lag. No authentication required (must respond even if the service is degraded).

**R9 — Failure Detection**
If the passive node fails to reach the active node for N consecutive heartbeats (configurable, default 3 = 3 seconds), it begins the failover evaluation process.

## Failover

**R10 — Automatic Failover**
When the passive node detects the active is unreachable and passes split-brain checks, it automatically promotes itself to active: starts serving shares, reassigns the floating IP (if configured), and logs the event.

**R11 — Split-Brain Prevention**
Before promoting, the passive node verifies it has network connectivity beyond the active node (e.g., can reach the default gateway or a configurable external endpoint). If the passive itself is isolated, it does not promote — preventing both nodes from serving simultaneously.

**R12 — Fencing**
Before failover, the passive node attempts to fence (power off) the old active node via cloud API (AWS `stop-instances`) or configurable command. This prevents the old active from coming back online and causing split-brain. Fencing is best-effort — failover proceeds even if fencing fails, but a warning is logged.

**R13 — Floating IP**
HA supports a floating IP that follows the active node. On AWS, this is an Elastic IP reassigned via `aws ec2 associate-address`. On-prem, this is a virtual IP managed via `ip addr add/del`. Clients connect to the floating IP and are automatically directed to whichever node is active.

## Failback

**R14 — Manual Failback**
After the original active node recovers, it does not automatically reclaim the active role. An administrator must manually initiate failback via the UI. This prevents flapping and gives the admin time to investigate the original failure.

**R15 — Failback Process**
1. Recovered node comes online as passive
2. Syncs data from the current active (catches up on changes made during outage)
3. Admin triggers switchover via UI
4. Current active stops serving, new active takes over, floating IP moves

## Interface

**R16 — HA Dashboard**
Both nodes display HA status on the dashboard: current role, partner node status, replication lag, last sync time, failover history. Active node shows "Active — replicating to <passive name>". Passive shows "Passive — monitoring <active name>".

**R17 — HA Configuration UI**
Dedicated HA section in Settings or its own nav page. Enable/disable HA, select partner node (from paired nodes), configure: sync interval, heartbeat interval, failure threshold, floating IP, fencing method.

**R18 — Failover Controls**
Manual failover button (force promote passive to active). Manual failback button. Both require confirmation. Failover history log showing timestamps, reason, and duration.

**R19 — REST API**
All HA operations available via REST: enable/disable HA, get status, trigger failover/failback, get failover history.

## Out of Scope (Future Phases)
- Multi-node clusters (3+ nodes)
- Automatic failback
- Quorum-based voting (requires 3+ nodes)
- Block-level replication (DRBD)
- inotify-based real-time sync
- Load balancing / active-active read scaling
