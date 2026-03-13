# Phase 7: High Availability — Tasks

## Core HA Infrastructure

**T1 — HA data model**
Add `HAState` and `Failover` structs to `internal/engine/types.go`. Add HA config keys to `/etc/poolforge.conf` read/write helpers.

**T2 — HA Manager skeleton**
Create `internal/replication/ha.go` with `HAManager` struct. Load/save state from `/var/lib/poolforge/ha_state.json`. Methods: `Enable()`, `Disable()`, `GetStatus()`, `IsActive()`, `IsPassive()`. Initialize in `main.go`, wire to server.

**T3 — Health endpoint**
Add `GET /api/health` — returns role, uptime, pool state, last sync, replication lag. No auth required (add to auth bypass list). Must respond fast even under load.

## Continuous Replication

**T4 — Continuous sync loop**
Add `startSyncLoop()` to HAManager. Goroutine runs rsync every `SYNC_INTERVAL` seconds using existing `rsyncWithProgress()`. Tracks `last_sync_at` and replication lag. Logs errors, alerts after 3 consecutive failures. Only runs on active node.

**T5 — Initial full sync**
When HA is first enabled, run a full sync before arming failover. Set `initial_sync_done = false` until first sync completes. UI shows sync progress. Passive node does not attempt failover until initial sync is done.

**T6 — Replication lag tracking**
Calculate lag as `time.Now() - last_sync_at`. Expose in `/api/health` and `/api/ha/status`. Alert if lag exceeds configurable threshold (default 5 minutes). Display on dashboard.

## Health Monitoring

**T7 — Heartbeat monitor**
Add `startHeartbeatLoop()` to HAManager. Passive node polls `GET http://<active_host>:8080/api/health` every `HEARTBEAT_INTERVAL` seconds (default 1s). Track consecutive failures. Update `partner_reachable` and `last_heartbeat_at`. Only runs on passive node.

**T8 — Active monitors passive**
Active node also polls passive's `/api/health` to know if passive is available. Display partner status on dashboard. No failover action — informational only.

## Failover

**T9 — Split-brain check**
Before failover, passive verifies network connectivity by pinging the default gateway (`ip route | grep default` → ping gateway IP). If gateway unreachable, abort failover and log critical alert. Configurable external check endpoint as alternative.

**T10 — Fencing**
Implement fencing methods:
- `aws`: call `aws ec2 stop-instances --instance-ids <partner_instance_id>`
- `command`: execute configured shell command
- `none`: skip fencing (log warning)
Fencing is best-effort — failover proceeds regardless of fencing result.

**T11 — Failover execution**
Implement `Promote()` on HAManager:
1. Run fencing
2. Set role to active in conf and state
3. Start SMB/NFS shares for the pool
4. Reassign floating IP (T13)
5. Stop heartbeat loop, start sync loop
6. Record failover event in history
7. Log to safety log buffer

**T12 — Manual failover**
Add `POST /api/ha/failover` endpoint. Triggers `Promote()` regardless of heartbeat state. Requires confirmation parameter. Available on passive node only.

## Floating IP

**T13 — Floating IP management**
Implement IP reassignment:
- AWS: `aws ec2 associate-address --allocation-id <id> --instance-id <self> --allow-reassociation`
- Get own instance ID from EC2 metadata (`http://169.254.169.254/latest/meta-data/instance-id`)
- Virtual IP (on-prem): `ip addr add/del` on primary interface
Called during failover (T11) and failback (T14).

## Failback

**T14 — Failback process**
Add `POST /api/ha/failback` endpoint (available on current active only):
1. Verify partner is online and synced (query partner's `/api/health`)
2. Run final sync to partner
3. Set self to passive, notify partner to promote
4. Partner calls its own `Promote()`
5. Floating IP moves to new active
6. Record failback event in history

**T15 — Recovered node joins as passive**
When a previously-failed node starts up with `role=active` but detects the partner is already active (via health check), it automatically demotes itself to passive. Prevents dual-active on recovery.

## API

**T16 — HA API endpoints**
Register routes in `server.go`:
- `GET /api/health` — health check (no auth)
- `GET /api/ha/status` — full HA state
- `POST /api/ha/enable` — enable HA with config body
- `POST /api/ha/disable` — disable HA, revert to standalone
- `POST /api/ha/failover` — manual failover
- `POST /api/ha/failback` — manual failback
- `GET /api/ha/history` — failover event history

## UI

**T17 — HA status card on dashboard**
Add HA status card to dashboard page. Shows: role badge (Active/Passive/Standalone), partner name and status (Online/Offline), replication lag, last sync time, floating IP. Manual failover button (passive only). Refreshes with dashboard polling.

**T18 — HA configuration UI**
Add HA section to Settings page (or dedicated nav page). Controls:
- Enable/disable toggle
- Role selector (Active/Passive)
- Partner node dropdown (from paired nodes, fetches remote name)
- Local pool dropdown, remote pool dropdown (auto-populated like sync jobs)
- Sync interval, heartbeat interval, failure threshold inputs
- Floating IP input (EIP allocation ID or virtual IP CIDR)
- Fencing method dropdown (AWS/Command/None), partner instance ID input
- Save button

**T19 — Failover history UI**
Failover history table on HA status card or dedicated section. Columns: timestamp, event type (failover/failback/manual), reason, fencing result, duration. Scrollable, most recent first.

## Manual Validation Tests

**T20 — HA enable and continuous sync**
1. Set node names on both nodes via Settings
2. Enable HA on Node A (active) and Node B (passive) via UI
3. Verify initial full sync completes (progress shown)
4. Write test files to Node A pool
5. Wait 30 seconds, verify files appear on Node B
6. Check replication lag on both dashboards shows < 30s

**T21 — Automatic failover test**
1. With HA running, stop PoolForge service on Node A: `sudo systemctl stop poolforge`
2. Wait 3 seconds (3 × 1s heartbeat)
3. Verify Node B promotes to active (dashboard shows "Active")
4. Verify floating IP moved to Node B (if configured)
5. Verify SMB/NFS shares accessible via Node B
6. Verify failover event in history

**T22 — Split-brain prevention test**
1. On Node B (passive), block traffic to Node A: `sudo iptables -A OUTPUT -d <node_a_ip> -j DROP`
2. Also block gateway: `sudo iptables -A OUTPUT -d <gateway_ip> -j DROP`
3. Wait 3 seconds — verify Node B does NOT failover (it's isolated)
4. Remove iptables rules, verify Node B resumes normal monitoring

**T23 — Manual failback test**
1. After T21 failover, start Node A back up
2. Verify Node A comes online as passive (auto-demotion)
3. Verify Node A syncs from Node B (catches up)
4. Click "Failback" on Node B UI
5. Verify Node A becomes active, Node B becomes passive
6. Verify floating IP moves back to Node A
7. Verify failback event in history

**T24 — Replication lag alert test**
1. On Node A (active), block SSH to Node B to prevent sync
2. Wait for lag to exceed threshold (5 minutes)
3. Verify alert appears in alerts panel
4. Unblock SSH, verify lag recovers and alert clears
