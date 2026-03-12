# Implementation Plan: Phase 5 — External Enclosure Support

## Overview

Phase 5 adds external enclosure awareness to PoolForge. Implementation proceeds bottom-up: metadata extensions and types first, then RAIDManager extensions, BootConfigManager rewrite, core engine start/stop/set-autostart, CLI commands, API endpoints, Web Portal UI, installer updates, daemon boot/migration logic, and finally integration/regression tests. Each task builds on the previous, ensuring no orphaned code.

## Tasks

- [x] 1. Pool metadata extensions and new types
  - [x] 1.1 Add Phase 5 fields to Pool struct and new types in `internal/engine/types.go`
    - Add `IsExternal`, `RequiresManualStart`, `OperationalStatus`, `LastShutdown`, `LastStartup` fields to Pool struct with JSON tags
    - Add `PoolOperationalStatus` type with constants `PoolRunning`, `PoolOffline`, `PoolSafeToShutdown`
    - Add `UUID` field to `RAIDArray` struct
    - Add `StartPoolResult`, `ArrayStartResult`, `SuperblockMatch` structs
    - Add `External` field to `CreatePoolRequest`
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.7_

  - [x] 1.2 Implement backward-compatible metadata deserialization defaults
    - When loading pool metadata missing Phase 5 fields, apply defaults: `is_external=false`, `requires_manual_start=false`, `operational_status="running"`, `last_shutdown=nil`, `last_startup=nil`, `uuid=""`
    - Ensure `SavePool` persists all Phase 5 fields
    - _Requirements: 5.5, 5.7, 13.1_

  - [x]* 1.3 Write property test for pool metadata round-trip (P84)
    - **Property 84: Pool metadata round-trip with Phase 5 fields**
    - Generate arbitrary Pool structs with all Phase 5 field combinations, save via SavePool, load via LoadPool, assert equivalence
    - File: `internal/metadata/metadata_phase5_prop_test.go`
    - **Validates: Requirements 5.1, 5.2, 5.3, 5.4, 5.7**

  - [x]* 1.4 Write property test for backward-compatible metadata defaults (P85)
    - **Property 85: Backward-compatible metadata defaults**
    - Generate Phase 1–4 metadata JSON (no Phase 5 fields), load with Phase 5 code, assert defaults applied
    - File: `internal/metadata/metadata_phase5_prop_test.go`
    - **Validates: Requirements 5.5, 10.5**

  - [x]* 1.5 Write unit tests for metadata Phase 5 extensions
    - Test save/load round-trip with all Phase 5 fields
    - Test loading Phase 4 metadata without Phase 5 fields → defaults applied
    - Test pool status output for external pool includes Phase 5 fields
    - File: `internal/metadata/metadata_phase5_test.go`
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7_

- [x] 2. RAIDManager extensions
  - [x] 2.1 Implement `GetArrayUUID` in `internal/storage`
    - Parse UUID from `mdadm --detail <device>` output
    - Return error if device not found or UUID not parseable
    - _Requirements: 6.1_

  - [x] 2.2 Implement `AssembleArrayBySuperblock` in `internal/storage`
    - Execute `mdadm --assemble --uuid=<uuid> --scan`
    - Return `RAIDArrayInfo` with assembled device path
    - Handle device name changes transparently (UUID-based, no path dependency)
    - _Requirements: 2.4, 6.7_

  - [x] 2.3 Implement `ReAddMember` in `internal/storage`
    - Execute `mdadm --re-add <array-device> <partition-device>`
    - Return error if bitmap recovery not possible
    - _Requirements: 2.7, 6.3_

  - [x] 2.4 Implement `ScanSuperblocks` in `internal/storage`
    - Scan all partitions on large-capacity drives
    - Execute `mdadm --examine <partition>` for each candidate
    - Return `[]SuperblockMatch` with current device path, UUID, and previous device path
    - _Requirements: 6.2_

  - [x]* 2.5 Write property test for superblock scan completeness (P88)
    - **Property 88: Superblock scan completeness**
    - Generate arbitrary partition sets with known UUIDs, mock `mdadm --examine`, assert ScanSuperblocks returns all and only matching partitions
    - File: `internal/storage/raid_phase5_prop_test.go`
    - **Validates: Requirements 6.2**

  - [x]* 2.6 Write unit tests for RAIDManager Phase 5 extensions
    - Test GetArrayUUID parses UUID from mdadm --detail output
    - Test AssembleArrayBySuperblock calls mdadm --assemble --uuid=...
    - Test ReAddMember calls mdadm --re-add
    - Test ScanSuperblocks examines partitions and returns matches
    - File: `internal/storage/raid_phase5_test.go`
    - _Requirements: 2.4, 2.7, 6.1, 6.2, 6.3, 6.7_

- [x] 3. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. BootConfigManager rewrite
  - [x] 4.1 Replace `GenerateMdadmConf` with `GenerateBootConfig` in `internal/safety/boot.go`
    - Add `PoolBootInfo` and `ArrayBootInfo` structs
    - Implement `GenerateBootConfig(pools []PoolBootInfo)`: write `AUTO -all` directive, then ARRAY definitions only for pools where `RequiresManualStart == false`
    - Atomic write to `/etc/mdadm/mdadm.conf`
    - Execute `update-initramfs -u` after write
    - Handle missing `/etc/mdadm/` directory (create it)
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.8_

  - [x] 4.2 Implement `GenerateBootConfigFromMetadata` convenience function
    - Load all pools from MetadataStore, extract PoolBootInfo, call GenerateBootConfig
    - _Requirements: 1.5, 1.6, 1.7_

  - [x]* 4.3 Write property test for Boot_Config ARRAY definitions (P77)
    - **Property 77: Boot_Config ARRAY definitions match auto-start pools only**
    - Generate arbitrary pool sets with random `requires_manual_start` flags, generate Boot_Config, parse output, assert ARRAY defs present iff `requires_manual_start == false`, assert `AUTO -all` always present
    - File: `internal/safety/boot_prop_test.go`
    - **Validates: Requirements 1.1, 1.2, 1.3**

  - [x]* 4.4 Write property test for Boot_Config idempotence (P78)
    - **Property 78: Boot_Config generation is idempotent**
    - Generate arbitrary pool sets, call GenerateBootConfig twice, assert identical output
    - File: `internal/safety/boot_prop_test.go`
    - **Validates: Requirements 1.5, 1.6, 1.7, 11.10**

  - [x]* 4.5 Write property test for Boot_Config consistency after mutation (P79)
    - **Property 79: Boot_Config consistency after mutation**
    - Generate pool set, apply mutation (create/delete/set-autostart), regenerate Boot_Config, assert ARRAY definitions match current metadata state
    - File: `internal/safety/boot_prop_test.go`
    - **Validates: Requirements 1.5, 1.6, 1.7, 4.2, 4.3**

  - [x]* 4.6 Write unit tests for BootConfigManager
    - Test generate with zero pools → AUTO -all only
    - Test generate with one auto-start pool → AUTO -all + ARRAY defs
    - Test generate with one manual-start pool → AUTO -all, no ARRAY defs
    - Test generate with mixed pools → only auto-start pool arrays appear
    - Test missing /etc/mdadm/ directory → created
    - Test mdadm --detail failure for one array → array skipped
    - Test update-initramfs failure → warning logged, function succeeds
    - File: `internal/safety/boot_test.go`
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.8_

- [x] 5. Core engine: StartPool implementation
  - [x] 5.1 Implement `StartPool` method on EngineService in `internal/engine`
    - Load pool metadata, verify OperationalStatus is Offline or Safe_To_Power_Down
    - Reject if already Running (return error)
    - Drive verification: scan block devices, match capacities to metadata, warn if fewer detected
    - Array assembly in ascending tier order using AssembleArrayBySuperblock(UUID)
    - Abort on assembly failure (no superblock matches)
    - Degraded array auto-repair: ScanSuperblocks → ReAddMember → fallback to AddMember
    - Activate VG via `vgchange -ay`
    - Mount filesystem
    - Start HealthMonitor for pool
    - Full device name reconciliation: query `mdadm --detail` for each array, update ALL Disk_Descriptors and partition paths in metadata
    - Set OperationalStatus=Running, LastStartup=now, save metadata
    - Regenerate Boot_Config (device names may have changed)
    - Return StartPoolResult with per-array status
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8, 2.9, 2.10, 2.11, 2.12, 2.13, 2.14, 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.7, 6.8, 6.9_

  - [x]* 5.2 Write property test for tier ordering (P80)
    - **Property 80: Pool start/stop tier ordering**
    - Generate pools with N arrays across M tiers, mock RAIDManager, assert start calls ascending, stop calls descending, sequences are mirrors
    - File: `internal/engine/start_stop_prop_test.go`
    - **Validates: Requirements 2.5, 3.6**

  - [x]* 5.3 Write property test for UUID-based assembly (P81)
    - **Property 81: UUID-based assembly handles device name changes**
    - Generate arrays with known UUIDs and changed device names, mock AssembleArrayBySuperblock, assert assembly succeeds regardless of device paths
    - File: `internal/engine/start_stop_prop_test.go`
    - **Validates: Requirements 2.4, 6.7**

  - [x]* 5.4 Write property test for re-add preference (P82)
    - **Property 82: Re-add preference over full rebuild**
    - Generate degraded arrays with matching partitions, mock ReAddMember (sometimes fail), assert re-add attempted first, fallback to AddMember only on failure
    - File: `internal/engine/start_stop_prop_test.go`
    - **Validates: Requirements 2.7, 6.3, 6.5**

  - [x]* 5.5 Write property test for drive verification accuracy (P86)
    - **Property 86: Drive verification accuracy**
    - Generate pool configs with N expected drives and M detected devices, assert correct expected/detected counts and warning generation when M < N
    - File: `internal/engine/start_stop_prop_test.go`
    - **Validates: Requirements 2.2, 2.3**

  - [x]* 5.6 Write property test for full device name reconciliation (P87)
    - **Property 87: Full device name reconciliation in metadata after pool start**
    - Generate pools where drives have changed names, mock mdadm --detail with new paths, assert ALL Disk_Descriptors and partition paths updated (not just re-added drives)
    - File: `internal/engine/start_stop_prop_test.go`
    - **Validates: Requirements 6.8, 6.9**

  - [x]* 5.7 Write property test for pool start metadata update (P89 — start portion)
    - **Property 89: Pool start metadata update**
    - After successful StartPool, assert `operational_status == "running"` and `last_startup` is within the operation's time window
    - File: `internal/engine/start_stop_prop_test.go`
    - **Validates: Requirements 2.11**

  - [x]* 5.8 Write unit tests for StartPool
    - Start Offline pool with all drives → Running, all arrays healthy
    - Start Safe_To_Power_Down pool → Running
    - Start already Running pool → error
    - Start with fewer drives, no --force → warning returned
    - Start with fewer drives, --force → proceeds
    - Start with degraded array, matching partition → re-add attempted
    - Start with degraded array, re-add fails → fallback to full rebuild
    - Start with assembly failure → abort, pool remains Offline
    - Verify ascending tier order via mock call order
    - Verify full device name reconciliation updates all paths
    - File: `internal/engine/start_stop_test.go`
    - _Requirements: 2.1–2.14, 6.1–6.9_

- [x] 6. Core engine: StopPool implementation
  - [x] 6.1 Implement `StopPool` method on EngineService in `internal/engine`
    - Load pool metadata, verify OperationalStatus is Running
    - Reject if Offline or Safe_To_Power_Down (return error)
    - Stop HealthMonitor for pool
    - Call sync() to flush pending writes
    - Unmount filesystem (abort if busy, re-start HealthMonitor)
    - Deactivate LV via `lvchange -an`, then VG via `vgchange -an`
    - Stop arrays in descending tier order: sync before each, `mdadm --stop`, wait configurable delay (default 1s)
    - On array stop failure: log error, attempt force-stop, continue
    - Verify no pool arrays remain in /proc/mdstat
    - Verify AUTO -all present in mdadm.conf, log warning if missing
    - Set OperationalStatus=Safe_To_Power_Down, LastShutdown=now, save metadata
    - Display "It is now SAFE to power down the external enclosure."
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 3.10, 3.11, 3.12, 3.13_

  - [x]* 6.2 Write property test for post-stop array verification (P90)
    - **Property 90: Post-stop array verification**
    - After successful StopPool, mock /proc/mdstat, assert none of the pool's array devices appear
    - File: `internal/engine/start_stop_prop_test.go`
    - **Validates: Requirements 3.8**

  - [x]* 6.3 Write property test for pool stop metadata update (P89 — stop portion)
    - **Property 89: Pool stop metadata update**
    - After successful StopPool, assert `operational_status == "safe_to_power_down"` and `last_shutdown` is within the operation's time window
    - File: `internal/engine/start_stop_prop_test.go`
    - **Validates: Requirements 3.10**

  - [x]* 6.4 Write unit tests for StopPool
    - Stop Running pool → Safe_To_Power_Down, confirmation message
    - Stop Offline pool → error
    - Stop Safe_To_Power_Down pool → error
    - Verify descending tier order via mock call order
    - Unmount failure → stop aborted, pool remains Running
    - Array stop failure → force-stop attempted, error logged
    - /proc/mdstat verification after stop
    - File: `internal/engine/start_stop_test.go`
    - _Requirements: 3.1–3.13_

- [x] 7. Core engine: SetAutoStart and CreatePool extension
  - [x] 7.1 Implement `SetAutoStart` method on EngineService in `internal/engine`
    - Load pool metadata, reject if pool not found
    - Set `requires_manual_start` based on autoStart parameter (true→false, false→true)
    - Regenerate Boot_Config via GenerateBootConfigFromMetadata
    - Update initramfs
    - Save metadata, display confirmation
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.7_

  - [x] 7.2 Extend `CreatePool` to support `--external` flag
    - When `External == true`: set `IsExternal=true`, `RequiresManualStart=true`, regenerate Boot_Config (arrays excluded)
    - When `External == false` (default): set `IsExternal=false`, `RequiresManualStart=false`, regenerate Boot_Config (arrays included)
    - Set `OperationalStatus=PoolRunning` in both cases
    - _Requirements: 4.5, 4.6_

  - [x]* 7.3 Write property test for operational status state machine (P83)
    - **Property 83: Pool operational status state machine**
    - Generate arbitrary status transitions, assert: Offline→Running (StartPool), Running→Safe_To_Power_Down (StopPool), Safe_To_Power_Down→Running (StartPool), reject start on Running, reject stop on non-Running
    - File: `internal/engine/start_stop_prop_test.go`
    - **Validates: Requirements 2.13, 3.12, 9.5, 9.6**

  - [x]* 7.4 Write property test for auto-start defaults on new pools (P91)
    - **Property 91: Auto-start default for new pools**
    - Generate CreatePoolRequest with/without External flag, assert: without → `requires_manual_start=false, is_external=false`; with → `requires_manual_start=true, is_external=true`
    - File: `internal/engine/start_stop_prop_test.go`
    - **Validates: Requirements 4.5, 4.6**

  - [x]* 7.5 Write unit tests for SetAutoStart and CreatePool extension
    - Set auto-start false → requires_manual_start=true, Boot_Config regenerated
    - Set auto-start true → requires_manual_start=false, Boot_Config regenerated
    - Unknown pool name → error
    - CreatePool with --external → IsExternal=true, RequiresManualStart=true
    - CreatePool without --external → IsExternal=false, RequiresManualStart=false
    - File: `internal/engine/start_stop_test.go`
    - _Requirements: 4.1–4.7_

- [x] 8. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 9. CLI commands for pool start, stop, set-autostart
  - [x] 9.1 Add `pool start <pool-name> [--force]` CLI command in `cmd/poolforge`
    - Parse pool name and --force flag
    - Call EngineService.StartPool via AtomicOperationManager
    - Display drive verification warning and prompt for confirmation if fewer drives detected (unless --force)
    - Display per-array status summary and mount point on success
    - Exit code 0 on success, 1 on error
    - _Requirements: 2.1, 2.2, 2.3, 2.12_

  - [x] 9.2 Add `pool stop <pool-name>` CLI command in `cmd/poolforge`
    - Parse pool name
    - Call EngineService.StopPool via AtomicOperationManager
    - Display "It is now SAFE to power down the external enclosure." on success
    - Exit code 0 on success, 1 on error
    - _Requirements: 3.1, 3.11_

  - [x] 9.3 Add `pool set-autostart <pool-name> <true|false>` CLI command in `cmd/poolforge`
    - Parse pool name and boolean value
    - Call EngineService.SetAutoStart
    - Display confirmation message with pool name and new setting
    - Exit code 0 on success, 1 on error
    - _Requirements: 4.1, 4.4_

- [x] 10. API server extensions
  - [x] 10.1 Implement `POST /api/pools/:name/start` endpoint in `internal/api`
    - Require valid Session_Token (401 if missing/invalid)
    - Return 404 if pool not found
    - Return 409 if pool already Running
    - Return 200 with StartPoolResponse on success
    - Return 200 with warnings field if fewer drives detected (client decides to proceed with `?force=true`)
    - _Requirements: 9.1, 9.4, 9.5, 9.7, 9.8, 9.9_

  - [x] 10.2 Implement `POST /api/pools/:name/stop` endpoint in `internal/api`
    - Require valid Session_Token (401 if missing/invalid)
    - Return 404 if pool not found
    - Return 409 if pool not Running
    - Return 200 with StopPoolResponse on success
    - _Requirements: 9.2, 9.4, 9.6, 9.7, 9.8_

  - [x] 10.3 Implement `PUT /api/pools/:name/autostart` endpoint in `internal/api`
    - Require valid Session_Token (401 if missing/invalid)
    - Accept JSON body `{"auto_start": true|false}`, return 400 on invalid body
    - Return 404 if pool not found
    - Return 200 with AutoStartResponse on success
    - _Requirements: 9.3, 9.4, 9.7, 9.8_

  - [x] 10.4 Extend pool detail and pool list API responses with Phase 5 fields
    - Add `is_external`, `requires_manual_start`, `operational_status`, `last_startup`, `last_shutdown` to GET /api/pools/:id response
    - Add `operational_status`, `is_external` to GET /api/pools list response
    - Add `uuid` to RAID array entries in detail response
    - _Requirements: 5.6, 9.8_

  - [x]* 10.5 Write property test for API authentication enforcement (P93)
    - **Property 93: Authentication enforcement on Phase 5 endpoints**
    - Generate requests to all three Phase 5 endpoints without valid Session_Token, assert HTTP 401
    - File: `internal/api/phase5_handlers_prop_test.go`
    - **Validates: Requirements 9.4**

  - [x]* 10.6 Write property test for API 404 on non-existent pools (P94)
    - **Property 94: 404 for non-existent pools**
    - Generate arbitrary non-existent pool names, send to all three Phase 5 endpoints, assert HTTP 404 with JSON body identifying unknown pool
    - File: `internal/api/phase5_handlers_prop_test.go`
    - **Validates: Requirements 9.7**

  - [x]* 10.7 Write unit tests for Phase 5 API endpoints
    - POST /start: valid pool → 200, running pool → 409, unknown → 404, no auth → 401
    - POST /stop: valid pool → 200, not running → 409, unknown → 404, no auth → 401
    - PUT /autostart: valid body → 200, invalid body → 400, unknown → 404, no auth → 401
    - File: `internal/api/phase5_handlers_test.go`
    - _Requirements: 9.1–9.9_

- [x] 11. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 12. Web Portal extensions
  - [x] 12.1 Create `ExternalWarningBanner` component
    - Display warning banner for pools with `is_external == true`: "External Enclosure — requires manual start/stop"
    - Do not render for internal pools
    - _Requirements: 8.1_

  - [x] 12.2 Create `StartStopButton` component
    - Show "Start Pool" button when operational_status is `offline` or `safe_to_power_down`
    - Show "Stop Pool" button when operational_status is `running`
    - Never show both simultaneously
    - Start button calls POST /api/pools/:name/start
    - Stop button calls POST /api/pools/:name/stop
    - Display operation result
    - _Requirements: 8.2, 8.3, 8.4, 8.5_

  - [x] 12.3 Add Pool_Operational_Status indicators to PoolCard and PoolDetailPage
    - Green "Running" label for running pools
    - Grey "Offline" label for offline pools
    - Blue "Safe to Power Down" label for safe_to_power_down pools
    - Apply grey Health_Color to offline pools in Storage_Map and Dashboard
    - _Requirements: 8.6, 8.10_

  - [x] 12.4 Create `AutoStartToggle` component
    - Toggle control in Detail_Panel for `requires_manual_start` setting
    - Calls PUT /api/pools/:name/autostart on change
    - _Requirements: 8.8_

  - [x] 12.5 Create `SafePowerDownConfirmation` component
    - Display prominent "Safe to Power Down" confirmation after successful pool stop
    - Show pool name and dismissible message
    - _Requirements: 8.9_

  - [x] 12.6 Create `ExternalTimestamps` component and integrate into PoolDetailPage
    - Display `last_startup` and `last_shutdown` timestamps for external pools
    - Integrate ExternalWarningBanner, StartStopButton, AutoStartToggle, SafePowerDownConfirmation, ExternalTimestamps into PoolDetailPage and PoolCard
    - _Requirements: 8.7_

  - [ ]* 12.7 Write property test for operational status color mapping (P95)
    - **Property 95: Operational status color mapping**
    - Generate arbitrary PoolOperationalStatus values, assert correct color: running→green, offline→grey, safe_to_power_down→blue; assert offline→grey in Health_Color mapping
    - File: `web/src/components/phase5_prop.test.tsx`
    - **Validates: Requirements 8.6, 8.10**

  - [ ]* 12.8 Write property test for start/stop button visibility (P96)
    - **Property 96: Start/Stop button visibility**
    - Generate arbitrary operational status, assert Start button visible iff offline/safe_to_power_down, Stop button visible iff running, never both
    - File: `web/src/components/phase5_prop.test.tsx`
    - **Validates: Requirements 8.2, 8.3**

  - [ ]* 12.9 Write property test for external pool UI indicators (P97)
    - **Property 97: External pool UI indicators**
    - Generate pools with arbitrary is_external values, assert: external→warning banner + timestamps + auto-start toggle; internal→no warning banner
    - File: `web/src/components/phase5_prop.test.tsx`
    - **Validates: Requirements 8.1, 8.7, 8.8**

  - [ ]* 12.10 Write unit tests for Web Portal Phase 5 components
    - PoolCard with operational status badge: correct color for each status
    - StartStopButton: shows Start when offline, Stop when running
    - ExternalWarningBanner: renders for external pools, hidden for internal
    - AutoStartToggle: renders with current value, calls API on change
    - SafePowerDownConfirmation: renders after successful stop
    - ExternalTimestamps: renders last_startup and last_shutdown
    - Storage_Map: grey color for offline pools
    - Files: `web/src/components/PoolCard.test.tsx`, `web/src/components/StartStopButton.test.tsx`, `web/src/components/ExternalWarningBanner.test.tsx`, `web/src/components/AutoStartToggle.test.tsx`
    - _Requirements: 8.1–8.10_

- [x] 13. Installer updates
  - [x] 13.1 Extend `install.sh` with mdadm systemd service disablement
    - Disable mdmonitor.service, mdadm.service, mdadm-waitidle.service via systemctl stop/disable/mask
    - Skip missing services without error, log informational message
    - Log confirmation that mdadm auto-assembly is disabled
    - Ensure Boot_Config contains AUTO -all and update initramfs after installation
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6_

  - [x] 13.2 Add metadata backup step to installer for safe upgrade
    - Before Phase 5 service starts, back up existing metadata to `/var/lib/poolforge/metadata.json.pre-phase5-backup`
    - Do not overwrite existing backup if it already exists
    - _Requirements: 13.8, 13.9_

  - [x]* 13.3 Write unit tests for installer updates
    - All three mdadm services exist → all disabled
    - One service missing → skipped without error, others disabled
    - All services missing → all skipped, no errors
    - Metadata backup created at expected path
    - Metadata backup already exists → not overwritten
    - _Requirements: 7.1–7.6, 13.8, 13.9_

- [x] 14. Daemon boot sequence and Phase 5 migration
  - [x] 14.1 Modify `updateBootConfig` in `internal/safety/daemon.go` to call `GenerateBootConfigFromMetadata`
    - Replace old `GenerateMdadmConf(arrays)` call with `GenerateBootConfigFromMetadata(store)`
    - _Requirements: 1.5, 1.6, 1.7_

  - [x] 14.2 Implement `bootPools` method in Daemon for per-pool auto-start at boot
    - For each pool: if `requires_manual_start == false`, call StartPool (auto-start); if true, set OperationalStatus=Offline and skip
    - Log auto-start decision for each pool
    - Failure to start one pool must not prevent other pools from starting
    - _Requirements: 10.1, 10.2, 10.3, 10.4_

  - [x] 14.3 Implement `migrateToPhase5` method in Daemon for one-time upgrade migration
    - Detect first run after upgrade: check if any pool is missing `operational_status` field
    - Apply Phase 5 defaults to all pools: `is_external=false`, `requires_manual_start=false`, `operational_status="running"`
    - Populate Array UUIDs from live `mdadm --detail` for each assembled array
    - Regenerate Boot_Config with AUTO -all and ARRAY definitions for all existing pools
    - Log "PoolForge upgraded to Phase 5. Metadata migrated. Boot config updated."
    - Do NOT modify any RAID arrays, LVM, or filesystems during migration
    - _Requirements: 13.1, 13.2, 13.3, 13.4, 13.5, 13.6, 13.7_

  - [x]* 14.4 Write property test for boot behavior (P92)
    - **Property 92: Boot behavior — auto-start pools start, manual-start pools skip**
    - Generate pool sets with mixed requires_manual_start flags, mock StartPool, assert auto-start pools started, manual-start pools set to Offline, one failure doesn't block others
    - File: `internal/engine/start_stop_prop_test.go`
    - **Validates: Requirements 10.1, 10.2, 10.3**

  - [x]* 14.5 Write property test for safe software upgrade (P98)
    - **Property 98: Safe software upgrade preserves data and configuration**
    - Generate Phase 1–4 metadata, run migration, assert: all Phase 5 defaults applied, UUIDs populated, all pre-existing fields preserved, Boot_Config regenerated with AUTO -all, no RAID/LVM/FS modifications
    - File: `internal/safety/migration_prop_test.go`
    - **Validates: Requirements 13.1, 13.2, 13.3, 13.4, 13.6, 13.7**

  - [x]* 14.6 Write unit tests for daemon boot and migration
    - Migration detects missing operational_status → triggers one-time migration
    - Migration populates Array UUIDs from mdadm --detail
    - Migration preserves all existing metadata fields
    - Migration regenerates Boot_Config with AUTO -all
    - Migration logs informational message
    - Already-migrated metadata → migration skipped
    - mdadm --detail fails for one array → UUID skipped, others populated
    - bootPools: auto-start pool started, manual-start pool set to Offline
    - bootPools: one pool failure doesn't block others
    - Files: `internal/safety/migration_test.go`, `internal/engine/start_stop_test.go`
    - _Requirements: 10.1–10.4, 13.1–13.7_

- [x] 15. Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 16. Test infrastructure and integration tests
  - [x] 16.1 Extend Terraform IaC template for Phase 5 test scenarios
    - Provision sufficient EBS volumes for partial detach (degraded) and full detach (power cycle) scenarios
    - Support enclosure power cycle simulation: detach all pool EBS volumes and reattach
    - Support degraded array simulation: detach subset of pool EBS volumes
    - Support device name change simulation: reattach EBS volume to different device slot
    - _Requirements: 12.1, 12.2, 12.3, 12.6_

  - [x] 16.2 Implement pool start/stop sequencing integration test
    - Create pool → stop pool → verify arrays stopped → start pool → verify arrays assembled in correct tier order → verify data integrity
    - File: `test/integration/pool_start_stop_test.go`
    - _Requirements: 11.1, 12.4_

  - [x] 16.3 Implement device name change integration test
    - Create pool → stop pool → detach EBS volumes → reattach to different device slots → start pool → verify UUID-based assembly → verify metadata device names updated
    - File: `test/integration/device_name_change_test.go`
    - _Requirements: 11.2, 12.3_

  - [x] 16.4 Implement degraded array repair integration test
    - Create pool → detach one EBS volume → start pool → verify degraded array detected → reattach volume → verify re-add attempted (not full rebuild) → verify recovery
    - File: `test/integration/degraded_repair_test.go`
    - _Requirements: 11.3, 12.2_

  - [x] 16.5 Implement full power cycle simulation integration test
    - Create pool → write data → stop pool → detach all EBS volumes → reattach with different device names → start pool → verify data integrity → verify no full rebuild triggered → verify device name updates in metadata
    - File: `test/integration/power_cycle_test.go`
    - _Requirements: 11.6, 11.11, 12.4_

  - [x] 16.6 Implement Boot_Config integration test
    - Create two pools (one auto-start, one manual-start) → verify Boot_Config contains AUTO -all → verify only auto-start pool has ARRAY definitions
    - File: `test/integration/boot_config_test.go`
    - _Requirements: 11.4, 11.5_

  - [x] 16.7 Implement Phase 5 API endpoint integration test
    - POST /start → 200 → POST /stop → 200 → verify 409 on duplicate start/stop → verify 404 on unknown pool → verify 401 without auth
    - File: `test/integration/phase5_api_test.go`
    - _Requirements: 11.7_

  - [x] 16.8 Implement safe software upgrade integration test
    - Install Phase 4 build → create pools with data → upgrade to Phase 5 build → verify all pools remain running → verify all data intact → verify metadata contains Phase 5 defaults with correct UUIDs → verify Boot_Config regenerated with AUTO -all → verify no RAID arrays modified during upgrade
    - File: `test/integration/upgrade_test.go`
    - _Requirements: 11.9, 13.10_

  - [x] 16.9 Implement Web Portal component integration tests
    - Validate start/stop button rendering, Pool_Operational_Status indicators, external enclosure warning banners, auto-start toggle behavior, Safe_To_Power_Down confirmation display
    - _Requirements: 11.8_

  - [x] 16.10 Implement regression tests for Phase 1–4 functionality
    - Phase 1: pool creation, status, list, metadata persistence, tier computation, RAID level selection
    - Phase 2: add-disk, replace-disk, remove-disk, delete-pool, self-healing rebuild, expansion, export/import
    - Phase 3: all existing API endpoints, Web Portal (Dashboard, StorageMap, DetailPanel, LogViewer), authentication
    - Phase 4: atomic operations, rollback, crash recovery, multi-interface detection, SMART monitoring
    - File: `test/integration/regression_test.go`
    - _Requirements: 11.9_

  - [x] 16.11 Extend test runner to collect Phase 5 logs
    - Collect pool start/stop logs, Re_Add operation logs, and Boot_Config contents from EC2 instance for each Phase 5 test scenario
    - _Requirements: 12.5_

- [x] 17. Final checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties (P77–P98) from the design document
- Unit tests validate specific examples and edge cases
- The PoolForge daemon stays running during pool stop/start — only the pool's storage stack is managed
- Device name reconciliation after pool start updates ALL paths (not just re-added drives) from mdadm --detail
- Re-add (fast bitmap recovery) is always preferred over full rebuild; fallback only on re-add failure
- Data loss must NEVER happen — sync calls before every array stop, unmount before LVM deactivation
