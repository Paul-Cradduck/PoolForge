# Implementation Plan: Phase 4 — Safety Hardening

## Overview

Phase 4 hardens PoolForge for production safety. Implementation proceeds bottom-up: MetadataStore extensions first (checkpoint, SMART persistence), then AtomicOperationManager (checkpoint/rollback/write-barrier/post-verification), multi-interface disk detection and hot-plug, SMARTMonitor with MockSMARTProvider, HealthMonitor extensions, CLI/API/Web Portal SMART integration, crash recovery wiring, and finally test infrastructure extensions. Each task builds on the previous. Phase 4 MUST NOT modify EngineService or break any Phase 1/Phase 2/Phase 3 functionality.

## Tasks

- [ ] 1. Extend MetadataStore for Phase 4 (`internal/metadata`)
  - [ ] 1.1 Add Checkpoint persistence methods
    - Add `SaveCheckpoint(checkpoint *atomic.Checkpoint) error`, `LoadCheckpoint() (*atomic.Checkpoint, error)`, and `ClearCheckpoint() error` to the MetadataStore interface
    - Implement checkpoint serialization under the `checkpoint` key in the metadata JSON (null when no checkpoint)
    - Ensure backward compatibility: missing `checkpoint` key defaults to nil on load
    - Use existing atomic write mechanism (write-to-temp + rename) for checkpoint persistence
    - _Requirements: 1.2, 1.9_

  - [ ] 1.2 Add SMART data and threshold persistence methods
    - Add `SaveSMARTData(disk string, data *smart.SMARTData) error`, `LoadSMARTData(disk string) (*smart.SMARTData, error)` to the MetadataStore interface
    - Add `SaveSMARTThresholds(thresholds *smart.SMARTThresholds) error`, `LoadSMARTThresholds() (*smart.SMARTThresholds, error)` to the MetadataStore interface
    - Implement SMART data serialization under `smart_data` map keyed by disk path
    - Implement SMART thresholds serialization under `smart_thresholds` key
    - `LoadSMARTThresholds` returns `DefaultSMARTThresholds()` when no custom thresholds are configured
    - Ensure backward compatibility: missing `smart_data` and `smart_thresholds` keys default to empty map and defaults respectively
    - _Requirements: 3.2, 4.1, 4.2_

  - [ ] 1.3 Add Interface_Type to disk metadata in pool entries
    - Extend the disk entry within pool metadata to include `interface_type` field
    - Ensure backward compatibility: missing `interface_type` defaults to empty string on load
    - _Requirements: 2.2_

  - [ ]* 1.4 Write property test P63: Checkpoint persistence round-trip
    - **Property 63: Checkpoint persistence round-trip for crash recovery**
    - Use `rapid` to generate random valid Checkpoint structs (arbitrary operation name, pool ID, pool snapshot JSON, step index, step names list, timestamps), save via `SaveCheckpoint`, load via `LoadCheckpoint`, assert all fields preserved
    - File: `internal/metadata/metadata_phase4_prop_test.go`
    - **Validates: Requirements 1.9**

  - [ ]* 1.5 Write property test P31: SMART data persistence round-trip
    - **Property 31: SMART data persistence round-trip**
    - Use `rapid` to generate random valid SMARTData structs (arbitrary disk identifier, overall health in {"PASSED","FAILED"}, non-negative integer attributes, valid timestamp), save via `SaveSMARTData`, load via `LoadSMARTData`, assert all fields preserved
    - File: `internal/smart/smart_prop_test.go`
    - **Validates: Requirements 3.2, 9.9**

  - [ ]* 1.6 Write property test P34: SMART threshold configuration round-trip
    - **Property 34: SMART threshold configuration round-trip**
    - Use `rapid` to generate random valid SMARTThresholds structs (non-negative integers for reallocated_sectors, pending_sectors, uncorrectable_errors), save via `SaveSMARTThresholds`, load via `LoadSMARTThresholds`, assert same values returned
    - File: `internal/smart/smart_prop_test.go`
    - **Validates: Requirements 4.2, 4.3, 4.6, 9.9**

  - [ ]* 1.7 Write unit tests for MetadataStore Phase 4 extensions
    - Test `SaveCheckpoint` / `LoadCheckpoint` / `ClearCheckpoint` lifecycle
    - Test `LoadCheckpoint` returns nil when no checkpoint exists
    - Test `SaveSMARTData` / `LoadSMARTData` for multiple disks
    - Test `LoadSMARTData` returns error for unknown disk
    - Test `SaveSMARTThresholds` / `LoadSMARTThresholds` round-trip
    - Test `LoadSMARTThresholds` returns defaults when none configured
    - Test backward compatibility: load Phase 3 metadata file without Phase 4 keys
    - File: `internal/metadata/metadata_phase4_test.go`
    - _Requirements: 1.2, 1.9, 3.2, 4.1, 4.2_

- [ ] 2. Checkpoint — MetadataStore extensions
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 3. Implement AtomicOperationManager (`internal/atomic`)
  - [ ] 3.1 Create WriteBarrier
    - Define `WriteBarrier` struct in `internal/atomic/barrier.go`
    - Implement `Sync()` that calls `syscall.Sync()`
    - Implement `FsyncFile(path string)` that opens the file, calls `f.Sync()`, and closes
    - Implement `FsyncDevice(device string)` that opens the block device and calls `f.Sync()` to flush write cache
    - _Requirements: 1.6_

  - [ ] 3.2 Create PostOperationVerifier
    - Define `PostOperationVerifier` struct, `VerifyResult`, and `VerifyIssue` types in `internal/atomic/verifier.go`
    - Implement `Verify(ctx, pool)` that checks: (1) each RAID array via `mdadm --detail` for clean/active state, (2) VG via `vgck`, (3) LV via `lvs` for active state, (4) ext4 via `e2fsck -n` for consistency
    - Return `VerifyResult` with `Consistent=true` if all pass, or `Consistent=false` with list of `VerifyIssue` entries
    - _Requirements: 1.4, 1.5_

  - [ ] 3.3 Create AtomicOperationManager core
    - Define `AtomicOperationManager` struct, `OperationStep`, `AtomicOperation`, and `Checkpoint` types in `internal/atomic/atomic.go`
    - Implement `NewAtomicOperationManager(engine, metadata, logger)` constructor
    - Implement `ExecuteAtomic(ctx, op)`:
      1. Run PreOperationChecks (disk accessibility, PoolForge_Signature, array health, no active rebuild)
      2. Create Checkpoint via `metadata.SaveCheckpoint` with pool snapshot and step=-1
      3. Issue WriteBarrier after checkpoint save
      4. Execute steps sequentially; after each step: WriteBarrier, update checkpoint step index
      5. On step failure: rollback completed steps in reverse order, restore checkpoint pool state, clear checkpoint, log rollback details
      6. On success: run PostOperationVerification; if inconsistent, mark pool degraded and log error; clear checkpoint; log success
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7_

  - [ ] 3.4 Implement rollback failure handling
    - When a rollback step itself fails, mark pool state as "failed" in MetadataStore
    - Log a critical-level entry identifying the partially rolled-back state and the specific step that failed to reverse
    - Do not clear the checkpoint (leave it for manual recovery)
    - _Requirements: 1.8_

  - [ ] 3.5 Implement crash recovery
    - Implement `RecoverFromCheckpoint(ctx)` that calls `metadata.LoadCheckpoint()`
    - If checkpoint exists: execute rollback from the recorded step back to step 0, restore pool to checkpoint snapshot, clear checkpoint, log crash recovery details
    - If no checkpoint: no-op (clean startup)
    - Wire `RecoverFromCheckpoint` into PoolForge startup sequence
    - _Requirements: 1.9_

  - [ ] 3.6 Implement wrapped operation methods
    - Implement `CreatePoolAtomic(ctx, req)`: decompose into steps (PartitionDisks → CreateArrays → CreatePVs → CreateVG → CreateLV → CreateFS → SaveMetadata) with corresponding rollback for each step
    - Implement `AddDiskAtomic(ctx, poolID, disk)`: steps (PartitionNewDisk → AddMembers → ReshapeArrays → CreateNewTiers → ExtendLV → ResizeFS → SaveMetadata)
    - Implement `ReplaceDiskAtomic(ctx, poolID, oldDisk, newDisk)`: steps (PartitionReplacement → AddMembers → SaveMetadata)
    - Implement `RemoveDiskAtomic(ctx, poolID, disk)`: steps (RemoveMembers → ReshapeArrays → ReduceLV → ResizeFS → WipeDisk → SaveMetadata)
    - Implement `DeletePoolAtomic(ctx, poolID)`: steps (Unmount → RemoveLV → RemoveVG → RemovePVs → StopArrays → WipeDisks → DeleteMetadata)
    - Implement `ExpandPoolAtomic(ctx, poolID)`: steps (CreateNewArrays → CreatePVs → ExtendVG → ExtendLV → ResizeFS → SaveMetadata)
    - Each step has a forward `Execute` and a reverse `Rollback` function
    - _Requirements: 1.1, 1.2, 1.3_

  - [ ] 3.7 Implement FailureInjector test hook
    - Define `FailureInjector` struct with `FailAtStep int` and `FailError error` in `internal/atomic/atomic.go`
    - Add `SetFailureInjector(fi *FailureInjector)` method to AtomicOperationManager
    - In `ExecuteAtomic`, check injector before each step; if `FailAtStep == currentStep`, return `FailError` to trigger rollback
    - Support `POOLFORGE_FAIL_AT_STEP` and `POOLFORGE_FAIL_ERROR` environment variables for integration testing
    - _Requirements: 9.1, 10.4_

  - [ ]* 3.8 Write property test P62: Checkpoint/Rollback state equivalence
    - **Property 62: Checkpoint/Rollback state equivalence**
    - Use `rapid` to generate random pool states and random failure step indices (0 ≤ N < total steps), execute an atomic operation with failure injected at step N, assert pool state in MetadataStore after rollback equals the checkpoint's pool_snapshot
    - File: `internal/atomic/atomic_prop_test.go`
    - **Validates: Requirements 1.1, 1.3, 9.9**

  - [ ]* 3.9 Write property test P64: Pre-operation check enforcement
    - **Property 64: Pre-operation check enforcement**
    - Use `rapid` to generate random precondition violation scenarios (disk inaccessible, signature mismatch, array unhealthy, rebuild in progress), attempt an atomic operation, assert operation rejected before checkpoint creation, assert `LoadCheckpoint` returns nil
    - File: `internal/atomic/atomic_prop_test.go`
    - **Validates: Requirements 1.7**

  - [ ]* 3.10 Write property test P65: Atomic operation all-or-nothing guarantee
    - **Property 65: Atomic operation all-or-nothing guarantee**
    - Use `rapid` to generate random multi-step operations with random success/failure outcomes, assert outcome is exactly one of: (a) all changes applied and pool reflects new state, or (b) operation failed and pool state identical to pre-operation state
    - File: `internal/atomic/atomic_prop_test.go`
    - **Validates: Requirements 1.1, 1.2, 1.3**

  - [ ]* 3.11 Write property test P66: Write barrier ordering
    - **Property 66: Write barrier ordering**
    - Use `rapid` to generate random N-step operations, instrument WriteBarrier with a call counter, execute the operation, assert total barrier calls ≥ N + 1 (one for checkpoint + one per step)
    - File: `internal/atomic/atomic_prop_test.go`
    - **Validates: Requirements 1.6**

  - [ ]* 3.12 Write property test P76: Rollback failure marks pool as failed
    - **Property 76: Rollback failure marks pool as failed**
    - Use `rapid` to generate random operations where both a forward step and its rollback step fail, assert pool state is marked "failed" in MetadataStore and a critical-level log entry is created
    - File: `internal/atomic/atomic_prop_test.go`
    - **Validates: Requirements 1.8**

  - [ ]* 3.13 Write unit tests for AtomicOperationManager
    - Test checkpoint creation: verify checkpoint saved before first step executes
    - Test step execution order: verify steps execute sequentially with write barriers between them
    - Test rollback on failure: inject failure at step 2 of 5, verify steps 2 and 1 rolled back in reverse order
    - Test crash recovery: save checkpoint to metadata, call RecoverFromCheckpoint, verify rollback executes
    - Test post-operation verification: mock mdadm/LVM/ext4 check results, verify correct VerifyResult
    - Test pre-operation check rejection: verify no checkpoint created when preconditions fail
    - Test rollback failure handling: simulate rollback step failure, verify pool marked as failed with critical log
    - Test FailureInjector: set injector at step N, verify failure at that step triggers rollback
    - File: `internal/atomic/atomic_test.go`
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9_

  - [ ]* 3.14 Write unit tests for WriteBarrier
    - Test `Sync()` calls system sync
    - Test `FsyncFile()` opens file and calls fsync
    - Test `FsyncDevice()` opens device and calls fsync
    - File: `internal/atomic/barrier_test.go`
    - _Requirements: 1.6_

  - [ ]* 3.15 Write unit tests for PostOperationVerifier
    - Test verify with all components healthy returns Consistent=true
    - Test verify with degraded mdadm array returns Consistent=false with mdadm issue
    - Test verify with LVM inconsistency returns Consistent=false with lvm issue
    - Test verify with ext4 inconsistency returns Consistent=false with ext4 issue
    - File: `internal/atomic/verifier_test.go`
    - _Requirements: 1.4, 1.5_

- [ ] 4. Checkpoint — AtomicOperationManager
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 5. Implement Multi-Interface Disk Detection (`internal/storage`)
  - [ ] 5.1 Add InterfaceType detection to DiskManager
    - Define `InterfaceType` constants (SATA, eSATA, USB, DAS) and `DiskWithInterface` struct in `internal/storage/interface.go`
    - Implement `GetInterfaceType(device string)` that reads sysfs attributes: USB driver path → USB, SATA transport + removable=1 → eSATA, SATA transport + removable=0 → SATA, otherwise → DAS
    - Implement `ListAllDisks()` that enumerates all block devices via `/sys/block/`, filters out non-disk devices (loop, ram, etc.), calls `GetInterfaceType` for each, returns `[]DiskWithInterface`
    - Log warning for USB-connected disks advising about connection instability
    - _Requirements: 2.1, 2.2, 2.3, 2.7_

  - [ ] 5.2 Implement HotPlugListener
    - Define `HotPlugEvent`, `HotPlugAction` types and `HotPlugListener` struct in `internal/storage/hotplug.go`
    - Implement `Start(ctx)` that monitors udev events for block device add/remove actions
    - Implement `Events()` returning the event channel
    - On disk disconnection: resolve pool ownership, emit disconnect event
    - On disk connection: determine interface type, emit connect event with device and interface type
    - _Requirements: 2.4, 2.5, 2.6_

  - [ ]* 5.3 Write property test P67: Multi-interface detection completeness
    - **Property 67: Multi-interface detection completeness**
    - Use `rapid` to generate random sets of block devices with known sysfs attributes (transport, driver, removable), mock sysfs reads, call `ListAllDisks`, assert every device returned and `GetInterfaceType` classifies each into exactly one of {SATA, eSATA, USB, DAS} per the decision tree
    - File: `internal/storage/interface_prop_test.go`
    - **Validates: Requirements 2.1, 2.2**

  - [ ]* 5.4 Write property test P68: Interface type does not affect operation behavior
    - **Property 68: Interface type does not affect operation behavior**
    - Use `rapid` to generate random pool operations with disks of varying interface types, assert operation result (success/failure, resulting pool state) is identical regardless of interface type classification
    - File: `internal/storage/interface_prop_test.go`
    - **Validates: Requirements 2.7**

  - [ ]* 5.5 Write property test P69: Hot-plug disconnection triggers disk failure workflow
    - **Property 69: Hot-plug disconnection triggers disk failure workflow**
    - Use `rapid` to generate random pool configurations and random disconnect events for pool member disks, assert HealthMonitor invokes HandleDiskFailure, disk marked failed, affected arrays marked degraded
    - File: `internal/storage/hotplug_prop_test.go`
    - **Validates: Requirements 2.5**

  - [ ]* 5.6 Write unit tests for multi-interface detection
    - Test SATA detection: mock sysfs with transport=sata, removable=0 → SATA
    - Test eSATA detection: mock sysfs with transport=sata, removable=1 → eSATA
    - Test USB detection: mock sysfs with usb-storage driver → USB
    - Test DAS detection: mock sysfs with SAS transport → DAS
    - Test unknown sysfs: missing attributes → default to DAS
    - Test ListAllDisks: mock multiple devices with different interfaces, verify all returned with correct types
    - Test USB disk warning log emitted
    - File: `internal/storage/interface_test.go`
    - _Requirements: 2.1, 2.2, 2.3_

  - [ ]* 5.7 Write unit tests for HotPlugListener
    - Test disk disconnection event for pool member triggers HandleDiskFailure
    - Test disk connection event logs info with device and interface type
    - Test non-pool disk disconnection does not trigger failure workflow
    - Test listener graceful shutdown on context cancellation
    - File: `internal/storage/hotplug_test.go`
    - _Requirements: 2.4, 2.5, 2.6_

- [ ] 6. Implement SMARTMonitor (`internal/smart`)
  - [ ] 6.1 Create SMARTProvider interface and SmartctlProvider
    - Define `SMARTProvider` interface, `SMARTData`, `SMARTThresholds`, `SMARTEvent`, `SMARTEventType` types in `internal/smart/smart.go`
    - Implement `DefaultSMARTThresholds()` returning defaults (reallocated: 100, pending: 50, uncorrectable: 10)
    - Implement `SmartctlProvider` in `internal/smart/smartctl.go`: `GetSMARTData(device)` runs `smartctl -a <device> --json`, parses JSON output into `SMARTData`
    - Implement `IsAvailable()` that checks if `smartctl` binary exists in PATH
    - _Requirements: 3.1, 8.1, 8.2_

  - [ ] 6.2 Create MockSMARTProvider
    - Implement `MockSMARTProvider` in `internal/smart/mock/mock.go` with thread-safe `diskData` map
    - Implement `NewMockSMARTProvider()` with default healthy data
    - Implement `SetDiskData(device, data)` to configure per-disk SMART data
    - Implement `SimulateThresholdBreach(device, attribute, value)` to set a specific attribute above threshold
    - Implement `SimulateFailure(device)` to set overall_health to "FAILED"
    - Implement `GetSMARTData(device)` returning configured data
    - _Requirements: 10.1, 10.2, 10.3_

  - [ ] 6.3 Create SMARTMonitor core
    - Implement `SMARTMonitor` struct in `internal/smart/monitor.go`
    - Implement `NewSMARTMonitor(provider, metadata, logger)` constructor with default 1-hour interval
    - Implement `Start(ctx)` that runs periodic SMART checks on all managed disks: call provider.GetSMARTData for each disk, save to metadata, evaluate thresholds, emit events
    - Implement `Stop()` for graceful shutdown
    - Implement `SetCheckInterval(interval)` to update check frequency without restart
    - Implement `GetSMARTData(disk)` to retrieve latest data from metadata
    - Implement `GetSMARTHistory(disk)` to retrieve SMART event history
    - Implement `SetThresholds(thresholds)` to update and persist thresholds
    - Implement `Events()` returning the SMART event channel for HealthMonitor integration
    - Implement threshold evaluation: compare each attribute against configured threshold, generate warning event on breach; check overall_health, generate failure event on "FAILED"
    - Handle smartctl errors gracefully: mark disk SMART status as "unavailable", log info
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.7, 4.1, 4.2, 4.3_

  - [ ] 6.4 Wire smartmontools dependency check into startup
    - On PoolForge startup, check `SmartctlProvider.IsAvailable()`
    - If not available: log warning, disable SMART monitoring, mark all disks "unavailable"
    - If available: start SMARTMonitor with SmartctlProvider (or MockSMARTProvider if `--mock-smart` flag)
    - _Requirements: 8.2, 8.3_

  - [ ]* 6.5 Write property test P32: SMART threshold breach generates warning event
    - **Property 32: SMART threshold breach generates warning event and log**
    - Use `rapid` to generate random SMARTData where at least one attribute exceeds its threshold, run threshold evaluation, assert a SMART_Event with event_type "warning" is produced and a warning-level log entry identifies the disk, attribute, and threshold
    - File: `internal/smart/smart_prop_test.go`
    - **Validates: Requirements 3.3**

  - [ ]* 6.6 Write property test P33: SMART overall failure generates error event
    - **Property 33: SMART overall failure generates error event and log**
    - Use `rapid` to generate random SMARTData with overall_health="FAILED" and arbitrary attribute values, run threshold evaluation, assert a SMART_Event with event_type "failure" is produced and an error-level log entry identifies the disk
    - File: `internal/smart/smart_prop_test.go`
    - **Validates: Requirements 3.4**

  - [ ]* 6.7 Write unit tests for SMARTMonitor
    - Test periodic check calls GetSMARTData for each managed disk on tick
    - Test threshold evaluation: specific examples of breach/no-breach for each attribute
    - Test SMART failure detection: overall_health="FAILED" → error event
    - Test SMART unavailable: provider returns error → status "unavailable", info log
    - Test threshold update: set new thresholds, verify next check uses them
    - Test SetCheckInterval updates interval
    - Test graceful shutdown via Stop()
    - File: `internal/smart/smart_test.go`
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.7, 4.3_

  - [ ]* 6.8 Write unit tests for MockSMARTProvider
    - Test NewMockSMARTProvider returns healthy defaults
    - Test SetDiskData configures per-disk data
    - Test SimulateThresholdBreach sets attribute above threshold
    - Test SimulateFailure sets overall_health to "FAILED"
    - Test GetSMARTData returns configured data, error for unknown disk
    - Test thread safety with concurrent access
    - File: `internal/smart/mock_test.go`
    - _Requirements: 10.1, 10.2, 10.3_

- [ ] 7. Extend HealthMonitor for Phase 4 (`internal/monitor`)
  - [ ] 7.1 Add Hot-Plug and SMART event handlers to HealthMonitor
    - Add `OnHotPlug(handler func(HotPlugEvent))` to HealthMonitor interface
    - Add `OnSMARTEvent(handler func(SMARTEvent))` to HealthMonitor interface
    - Wire HotPlugListener events into HealthMonitor: on disconnect → resolve pool ownership → call HandleDiskFailure; on connect → log info
    - Wire SMARTMonitor events into HealthMonitor: forward SMART_Events to registered handlers
    - HealthMonitor now serves as unified event pipeline for mdadm events (Phase 2), Hot_Plug_Events (Phase 4), and SMART_Events (Phase 4)
    - _Requirements: 2.4, 2.5, 2.6, 3.8_

  - [ ]* 7.2 Write unit tests for HealthMonitor Phase 4 extensions
    - Test OnHotPlug handler receives disconnect events
    - Test OnHotPlug handler receives connect events
    - Test OnSMARTEvent handler receives warning events
    - Test OnSMARTEvent handler receives failure events
    - Test disconnect event for pool member triggers HandleDiskFailure
    - Test existing mdadm event handling still works (regression)
    - File: `internal/monitor/monitor_phase4_test.go`
    - _Requirements: 2.4, 2.5, 3.8_

- [ ] 8. Checkpoint — SMARTMonitor and HealthMonitor
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 9. Wire AtomicOperationManager into CLI and API
  - [ ] 9.1 Update CLI pool commands to use AtomicOperationManager
    - Modify CLI `pool create`, `pool add-disk`, `pool replace-disk`, `pool remove-disk`, `pool delete`, and `pool expand` commands to call the corresponding `*Atomic` methods on AtomicOperationManager instead of calling EngineService directly
    - All existing CLI command interfaces, flags, and output formats remain unchanged
    - _Requirements: 1.1_

  - [ ] 9.2 Update API Server pool handlers to use AtomicOperationManager
    - Modify API handlers for `POST /api/pools`, `POST /api/pools/:id/disks`, `POST /api/pools/:id/replace-disk`, `DELETE /api/pools/:id/disks/:dev`, `DELETE /api/pools/:id`, and `POST /api/pools/:id/expand` to call AtomicOperationManager wrapper methods
    - All existing API endpoint paths, request/response formats, and status codes remain unchanged
    - _Requirements: 1.1_

  - [ ] 9.3 Wire crash recovery into PoolForge startup
    - During PoolForge startup (both CLI and serve modes), call `AtomicOperationManager.RecoverFromCheckpoint(ctx)` before accepting any commands or requests
    - Log recovery actions if a checkpoint was found and rolled back
    - _Requirements: 1.9_

- [ ] 10. Implement SMART CLI Commands (`cmd/poolforge`)
  - [ ] 10.1 Implement `smart status <disk>` command
    - Add `smart status` subcommand that accepts a disk path argument
    - Call `SMARTMonitor.GetSMARTData(disk)` and display: overall health, temperature, reallocated sectors, pending sectors, uncorrectable errors, power-on hours, last check time
    - If disk not found in any pool: display error message, exit code 1
    - If SMART unavailable: display "SMART status: unavailable", exit code 0
    - _Requirements: 5.1, 5.4, 5.5_

  - [ ] 10.2 Implement `smart history <disk>` command
    - Add `smart history` subcommand that accepts a disk path argument
    - Call `SMARTMonitor.GetSMARTHistory(disk)` and display table: timestamp, attribute, threshold breached
    - If no events: display empty table
    - If disk not found: display error message, exit code 1
    - _Requirements: 5.2, 5.4, 5.5_

  - [ ] 10.3 Implement `smart thresholds set` command
    - Add `smart thresholds set` subcommand with flags: `--reallocated-sectors <N>`, `--pending-sectors <N>`, `--uncorrectable-errors <N>`
    - Call `SMARTMonitor.SetThresholds()` with provided values
    - At least one flag must be provided; missing flags retain current values
    - Display "Thresholds updated" on success, exit code 0
    - Display error for invalid values (negative, non-integer) or no flags, exit code 1
    - _Requirements: 5.3, 5.4, 5.5_

  - [ ]* 10.4 Write property test P75: CLI exit code correctness for SMART commands
    - **Property 75: CLI exit code correctness for SMART commands**
    - Use `rapid` to generate random valid and invalid SMART CLI invocations (valid disk → exit 0, unknown disk → exit 1, invalid threshold values → exit 1, no flags → exit 1), assert exit codes match expected values and error messages are descriptive
    - File: `cmd/poolforge/smart_cmd_prop_test.go`
    - **Validates: Requirements 5.4, 5.5**

  - [ ]* 10.5 Write unit tests for SMART CLI commands
    - Test `smart status` with valid disk: output contains all SMART fields, exit 0
    - Test `smart status` with unknown disk: error message, exit 1
    - Test `smart status` with SMART unavailable: "unavailable" output, exit 0
    - Test `smart history` with events: table output with timestamps
    - Test `smart history` with no events: empty table
    - Test `smart thresholds set` with valid flags: success message, exit 0
    - Test `smart thresholds set` with no flags: error, exit 1
    - Test `smart thresholds set` with invalid values: error, exit 1
    - File: `cmd/poolforge/smart_cmd_test.go`
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5_

- [ ] 11. Implement SMART API Endpoints (`internal/api`)
  - [ ] 11.1 Add SMART API handlers to API Server
    - Implement `GET /api/smart/:disk` handler: validate session, resolve disk, call `SMARTMonitor.GetSMARTData(disk)`, return `SMARTDataResponse` JSON with 200; return 404 for unmanaged disk; return 401 for missing/invalid session
    - Implement `PUT /api/smart/thresholds` handler: validate session, parse JSON body, validate non-negative integers, call `SMARTMonitor.SetThresholds()`, return `SMARTThresholdsResponse` JSON with 200; return 400 for invalid body; return 401 for no auth
    - Implement `GET /api/smart/:disk/history` handler: validate session, resolve disk, call `SMARTMonitor.GetSMARTHistory(disk)`, return `SMARTHistoryResponse` JSON with 200; return 404 for unmanaged disk; return 401 for no auth
    - Register routes on the existing API Server mux
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6_

  - [ ] 11.2 Extend pool detail API response with interface_type and smart_summary
    - Modify `GET /api/pools/:id` response to include `interface_type` and `smart_summary` (overall_health, temperature, smart_status) for each disk in the pool
    - _Requirements: 2.8, 3.5_

  - [ ]* 11.3 Write property test P74: SMART API returns 404 for unmanaged disks
    - **Property 74: SMART API returns 404 for unmanaged disks**
    - Use `rapid` to generate random disk identifiers not in any pool, send GET /api/smart/:disk and GET /api/smart/:disk/history, assert HTTP 404 with JSON body identifying the unknown disk
    - File: `internal/api/smart_handler_prop_test.go`
    - **Validates: Requirements 6.5**

  - [ ]* 11.4 Write unit tests for SMART API endpoints
    - Test GET /api/smart/:disk with valid managed disk → 200 with SMART data JSON
    - Test GET /api/smart/:disk with unmanaged disk → 404
    - Test GET /api/smart/:disk without auth → 401
    - Test PUT /api/smart/thresholds with valid body → 200 with updated thresholds
    - Test PUT /api/smart/thresholds with invalid body (negative values) → 400
    - Test PUT /api/smart/thresholds without auth → 401
    - Test GET /api/smart/:disk/history with valid disk → 200 with events
    - Test GET /api/smart/:disk/history with unmanaged disk → 404
    - Test pool detail response includes interface_type and smart_summary per disk
    - File: `internal/api/smart_handler_test.go`
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6_

- [ ] 12. Checkpoint — CLI, API, and wiring
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 13. Implement Web Portal SMART Extensions (`web/`)
  - [ ] 13.1 Add SMART API client functions and types
    - Add TypeScript interfaces for `SMARTData`, `SMARTThresholds`, `SMARTEvent`, `SMARTHistoryResponse` to `web/src/api/types.ts`
    - Add API client functions: `getSMARTData(disk)`, `setSMARTThresholds(thresholds)`, `getSMARTHistory(disk)` to `web/src/api/client.ts`
    - Add `SMARTStatus` type (`'healthy' | 'warning' | 'failure' | 'unavailable'`) to types
    - _Requirements: 6.1, 6.6_

  - [ ] 13.2 Extend DiskIcon with SMART health indicator
    - Extend `mapDiskHealthColor` in `web/src/utils/healthColor.ts` to accept `smartStatus` parameter: failed disk → red, SMART failure → red, SMART warning → amber, degraded/rebuilding → amber, otherwise → green
    - Update `DiskIcon` component to fetch or receive SMART status and apply extended Health_Color mapping
    - _Requirements: 3.5, 7.1_

  - [ ] 13.3 Create SMARTHealthSection component
    - Create `web/src/components/SMARTHealthSection.tsx` displaying: overall health, temperature, reallocated sectors (with threshold), pending sectors (with threshold), uncorrectable errors (with threshold), power-on hours, last check time
    - Fetch data from `GET /api/smart/:disk`
    - _Requirements: 3.6, 7.2_

  - [ ] 13.4 Create SMARTEventHistory component
    - Create `web/src/components/SMARTEventHistory.tsx` displaying SMART event history table: timestamp, event type, attribute, value, threshold
    - Fetch data from `GET /api/smart/:disk/history`
    - _Requirements: 3.6, 7.2_

  - [ ] 13.5 Integrate SMART section into DiskDetailContent
    - Add `SMARTHealthSection` and `SMARTEventHistory` components to the existing `DiskDetailContent` component
    - Display interface_type in the disk detail header
    - _Requirements: 2.8, 3.6, 7.2_

  - [ ] 13.6 Create SMARTThresholdForm and add to ConfigPage
    - Create `web/src/components/SMARTThresholdForm.tsx` with input fields for reallocated sectors, pending sectors, uncorrectable errors
    - Fetch current thresholds on mount, Save button calls `PUT /api/smart/thresholds`, Reset button restores defaults
    - Add SMART threshold configuration section to the existing `ConfigPage`
    - _Requirements: 4.5, 7.3_

  - [ ] 13.7 Create SMART NotificationBanner
    - Extend the existing `NotificationBanner` component to handle SMART events: display warning/error banner with disk identifier, attribute name, and threshold breached
    - Wire SMART event notifications into the global notification system (poll or WebSocket)
    - _Requirements: 7.4_

  - [ ]* 13.8 Write property test P71: SMART health indicator color mapping
    - **Property 71: SMART health indicator color mapping**
    - Use `fast-check` to generate random combinations of disk hardware state (healthy, degraded, failed) and SMART status (healthy, warning, failure, unavailable), assert `mapDiskHealthColor` returns: failed → red, SMART failure → red, SMART warning → amber, degraded/rebuilding → amber, otherwise → green; hardware failure takes priority
    - File: `web/src/components/DiskIcon.phase4.prop.test.tsx`
    - **Validates: Requirements 3.5, 7.1**

  - [ ]* 13.9 Write property test P70: Disk display includes interface type
    - **Property 70: Disk display includes interface type**
    - Use `fast-check` to generate random disk data with various interface types, render DiskDetailContent, assert interface_type field is present in rendered output
    - File: `web/src/components/DiskDetailContent.phase4.prop.test.tsx`
    - **Validates: Requirements 2.8**

  - [ ]* 13.10 Write property test P72: SMART Detail_Panel completeness
    - **Property 72: SMART Detail_Panel field completeness**
    - Use `fast-check` to generate random SMARTData with all fields populated, render SMARTHealthSection, assert all fields present: overall health, temperature, reallocated sectors with threshold, pending sectors with threshold, uncorrectable errors with threshold, power-on hours, last check time
    - File: `web/src/components/SMARTHealthSection.prop.test.tsx`
    - **Validates: Requirements 3.6, 7.2**

  - [ ]* 13.11 Write property test P73: SMART Notification_Banner on event
    - **Property 73: SMART Notification_Banner on event**
    - Use `fast-check` to generate random SMART_Events (warning and failure types), render NotificationBanner, assert banner contains disk identifier, attribute name, and threshold value
    - File: `web/src/components/NotificationBanner.phase4.prop.test.tsx`
    - **Validates: Requirements 7.4**

  - [ ]* 13.12 Write unit tests for Web Portal SMART components
    - Test DiskIcon renders correct color for each SMART status combination
    - Test SMARTHealthSection renders all data fields with thresholds
    - Test SMARTHealthSection handles "unavailable" SMART status
    - Test SMARTEventHistory renders event list with timestamps and attributes
    - Test SMARTEventHistory renders empty state when no events
    - Test SMARTThresholdForm renders current values, save calls API, reset restores defaults
    - Test SMARTThresholdForm validates non-negative integer inputs
    - Test NotificationBanner renders SMART warning with disk, attribute, threshold
    - Test NotificationBanner renders SMART failure with disk identifier
    - Test NotificationBanner dismiss button hides the banner
    - Files: `web/src/components/DiskIcon.phase4.test.tsx`, `web/src/components/SMARTHealthSection.test.tsx`, `web/src/components/SMARTEventHistory.test.tsx`, `web/src/components/SMARTThresholdForm.test.tsx`, `web/src/components/NotificationBanner.phase4.test.tsx`
    - _Requirements: 3.5, 3.6, 4.5, 7.1, 7.2, 7.3, 7.4_

- [ ] 14. Checkpoint — Web Portal SMART integration
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 15. Extend Test Infrastructure for Phase 4
  - [ ] 15.1 Add `--mock-smart` flag to `poolforge serve`
    - When `--mock-smart` is passed, use MockSMARTProvider instead of SmartctlProvider
    - Load mock SMART data from `/etc/poolforge/mock_smart.json` if present
    - _Requirements: 10.1, 10.6_

  - [ ] 15.2 Extend IaC template for Phase 4 test scenarios
    - Add `null_resource.mock_smart_setup` to `test/infra/main.tf` that deploys mock SMART configuration JSON to EC2 instance
    - Add `var.mock_smart_config` variable with default healthy data for all provisioned EBS volumes
    - _Requirements: 10.6_

  - [ ] 15.3 Extend Test_Runner for Phase 4 scenarios
    - Add failure injection test scenarios: create pool with failure at each step, verify rollback
    - Add crash recovery test: kill process mid-operation, restart, verify rollback
    - Add SMART monitoring tests: configure mock data, trigger checks, verify events
    - Add SMART threshold breach test: configure mock to exceed threshold, verify event logged
    - Collect rollback logs, post-verification results, and SMART monitoring logs from EC2
    - Run Phase 1, 2, and 3 regression tests
    - _Requirements: 10.4, 10.5, 10.7_

- [ ] 16. Integration and Regression Tests
  - [ ]* 16.1 Write failure injection integration tests
    - Test CreatePool with failure injected after mdadm creation but before LVM: verify rollback restores clean state (no orphaned arrays)
    - Test AddDisk with failure injected after reshape but before LV extend: verify rollback restores original pool state
    - Test DeletePool with failure injected after LV removal but before array stop: verify rollback restores pool
    - Files: `test/integration/atomic_create_test.go`, `test/integration/atomic_adddisk_test.go`, `test/integration/atomic_delete_test.go`
    - _Requirements: 9.1, 9.2_

  - [ ]* 16.2 Write crash recovery integration test
    - Create checkpoint in metadata simulating mid-operation crash, restart PoolForge, verify rollback executes automatically and pool restored to pre-operation state
    - File: `test/integration/crash_recovery_test.go`
    - _Requirements: 9.2_

  - [ ]* 16.3 Write post-operation verification integration test
    - Create pool successfully with atomic semantics, verify mdadm array consistency, LVM metadata consistency, and ext4 filesystem consistency checks all pass
    - File: `test/integration/post_verify_test.go`
    - _Requirements: 9.3_

  - [ ]* 16.4 Write multi-interface detection integration test
    - Attach EBS volumes (simulating SATA), verify interface type detection and enumeration returns all volumes with correct types
    - File: `test/integration/interface_detect_test.go`
    - _Requirements: 9.4_

  - [ ]* 16.5 Write SMART monitoring integration tests
    - Configure MockSMARTProvider with test data, trigger periodic check, verify SMART data persisted in metadata
    - Configure mock to exceed threshold, verify SMART_Event generated and logged
    - Configure mock with overall_health="FAILED", verify error event and log
    - File: `test/integration/smart_monitor_test.go`
    - _Requirements: 9.6_

  - [ ]* 16.6 Write SMART API integration tests
    - Test GET /api/smart/:disk → PUT /api/smart/thresholds → GET /api/smart/:disk/history round-trip via REST API with authentication
    - Test 404 for unmanaged disks
    - Test 401 for unauthenticated requests
    - File: `test/integration/smart_api_test.go`
    - _Requirements: 9.7_

  - [ ]* 16.7 Write full safety scenario integration test
    - Create pool with atomic semantics → inject failure mid-operation → verify rollback → retry operation successfully → verify post-operation verification passes → run SMART check with MockSMARTProvider → trigger threshold breach → verify SMART_Event logged and displayed
    - File: `test/integration/safety_scenario_test.go`
    - _Requirements: 10.5_

  - [ ]* 16.8 Write Phase 1 regression tests
    - Verify pool creation, status, list, metadata persistence, tier computation all work correctly via CLI with Phase 4 code
    - File: `test/integration/regression_phase1_test.go`
    - _Requirements: 9.8_

  - [ ]* 16.9 Write Phase 2 regression tests
    - Verify add-disk, replace-disk, remove-disk, delete-pool, self-healing rebuild, expansion, export/import all work correctly via CLI with Phase 4 code
    - File: `test/integration/regression_phase2_test.go`
    - _Requirements: 9.8_

  - [ ]* 16.10 Write Phase 3 regression tests
    - Verify API endpoints (pool CRUD, auth, logs), WebSocket Live_Tail, Web Portal rendering, authentication flow all work correctly with Phase 4 code
    - File: `test/integration/regression_phase3_test.go`
    - _Requirements: 9.8_

- [ ] 17. Final checkpoint — Phase 4 complete
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation at logical boundaries
- Property tests validate universal correctness properties (P31-P34, P62-P76) from the design document
- Unit tests validate specific examples and edge cases
- Phase 4 wraps existing operations — EngineService interface is unchanged
- All Phase 1, 2, and 3 functionality must remain working throughout implementation
