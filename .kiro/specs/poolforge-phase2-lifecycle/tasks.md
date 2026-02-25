# Implementation Plan: PoolForge Phase 2 — Lifecycle Operations

## Overview

Incremental build-up of Phase 2 lifecycle operations on top of the Phase 1 foundation. Starts with storage abstraction extensions, then metadata extensions, then safety primitives (signature, pre-operation checks), then the new HealthMonitor component, followed by engine lifecycle methods (add-disk, replace-disk, remove-disk, delete-pool, self-healing, expansion, serialization), CLI commands, and finally test infrastructure extensions with integration tests. Each task builds on the previous, with no orphaned code. Phase 2 MUST NOT break any Phase 1 functionality.

## Tasks

- [ ] 1. Storage abstraction extensions
  - [ ] 1.1 Extend DiskManager interface and implementation with Phase 2 methods
    - Add `WipePartitionTable`, `WritePoolForgeSignature`, `ReadPoolForgeSignature`, `HasExistingData` to `internal/storage/interfaces.go`
    - Implement in `internal/storage/disk.go`: `WipePartitionTable` wraps `sgdisk --zap-all`, `WritePoolForgeSignature` writes GPT partition entry #128 with type GUID `504F4F4C-464F-5247-4500-000000000000` and name `PoolForge:<pool-id>`, `ReadPoolForgeSignature` reads partition #128 and extracts pool ID, `HasExistingData` checks for existing filesystems/partition tables via `blkid`/`sgdisk`
    - _Requirements: 11.1, 11.2, 11.3, 11.4_

  - [ ] 1.2 Extend RAIDManager interface and implementation with Phase 2 methods
    - Add `AddMember`, `RemoveMember`, `ReshapeArray`, `GetSyncStatus` to `internal/storage/interfaces.go`
    - Implement in `internal/storage/raid.go`: `AddMember` wraps `mdadm --add`, `RemoveMember` wraps `mdadm --fail` + `mdadm --remove`, `ReshapeArray` wraps `mdadm --grow --raid-devices=N --level=L`, `GetSyncStatus` parses `/proc/mdstat` for sync state, percentage, and ETA
    - Add `SyncStatus` struct to interfaces
    - _Requirements: 2.2, 2.5, 4.5, 7.4_

  - [ ] 1.3 Extend LVMManager interface and implementation with Phase 2 methods
    - Add `ExtendVolumeGroup`, `ReduceVolumeGroup`, `ExtendLogicalVolume`, `ReduceLogicalVolume`, `RemovePhysicalVolume`, `RemoveVolumeGroup`, `RemoveLogicalVolume` to `internal/storage/interfaces.go`
    - Implement in `internal/storage/lvm.go`: each method wraps the corresponding LVM2 command (`vgextend`, `vgreduce`, `lvextend -l +100%FREE`, `lvreduce -L`, `pvremove`, `vgremove`, `lvremove -f`)
    - _Requirements: 2.4, 4.6, 5.2_

  - [ ] 1.4 Extend FilesystemManager interface and implementation with Phase 2 method
    - Add `ResizeFilesystem` to `internal/storage/interfaces.go`
    - Implement in `internal/storage/fs.go`: wraps `resize2fs <device>` for ext4 online/offline resize
    - _Requirements: 2.4, 4.6_

  - [ ]* 1.5 Write unit tests for Phase 2 storage abstraction extensions
    - Extend `internal/storage/disk_test.go`: test `WipePartitionTable` command construction, `WritePoolForgeSignature`/`ReadPoolForgeSignature` round-trip with mocked exec, `HasExistingData` parsing
    - Extend `internal/storage/raid_test.go`: test `AddMember`, `RemoveMember`, `ReshapeArray` command construction, `GetSyncStatus` parsing of `/proc/mdstat` output samples
    - Extend `internal/storage/lvm_test.go`: test all new LVM methods command construction and error handling
    - Extend `internal/storage/fs_test.go`: test `ResizeFilesystem` command construction
    - _Requirements: 12.1_

- [ ] 2. Metadata store extensions
  - [ ] 2.1 Extend MetadataStore interface and Pool schema with Phase 2 fields
    - Add `DeletePool(poolID string) error` to `internal/metadata/store.go` interface
    - Extend `DiskInfo` struct with `Signature string` and `FailedAt *time.Time` fields
    - Extend `RAIDArray` struct with `RebuildProgress *RebuildProgress` field
    - Add `RebuildProgress` map field to Pool struct for top-level rebuild tracking
    - Implement `DeletePool` in `internal/metadata/json_store.go`: remove pool entry from metadata, atomic write
    - Ensure backward compatibility — Phase 1 metadata files load correctly with zero-value Phase 2 fields
    - _Requirements: 1.1, 1.3, 1.9, 5.3_

  - [ ]* 2.2 Write unit tests for metadata store Phase 2 extensions
    - Extend `internal/metadata/metadata_test.go`: test `DeletePool` removes correct pool, test loading Phase 1 metadata with Phase 2 code (backward compat), test disk failure state persistence, test rebuild progress persistence
    - _Requirements: 12.1_

  - [ ]* 2.3 Write property test for rebuild progress persistence round-trip (P39)
    - **Property 39: Rebuild progress persistence round-trip**
    - Generate random rebuild progress states (array device, percentage 0-100, ETA, target disk, timestamps), save pool with rebuild progress to metadata, load it back, verify all rebuild progress fields are equivalent
    - File: `internal/metadata/metadata_prop_test.go`
    - **Validates: Requirements 1.9**

- [ ] 3. Checkpoint — Verify storage and metadata extensions
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 4. Safety primitives — PoolForge_Signature and pre-operation checks
  - [ ] 4.1 Implement PoolForge_Signature logic in engine
    - Create or extend `internal/engine/signature.go`
    - Implement signature write on pool creation (extend `CreatePool` to call `WritePoolForgeSignature` for each disk)
    - Implement signature verification helper: given a disk, read signature and verify it matches expected pool ID
    - Implement signature wipe helper: used by delete-pool and remove-disk
    - _Requirements: 11.3, 11.4_

  - [ ] 4.2 Implement pre-operation check framework
    - Create `internal/engine/checks.go`
    - Implement `PreOperationCheck` functions: `CheckArraysHealthy(pool)` — verify all arrays in healthy state, `CheckNoRebuildInProgress(pool)` — verify no active rebuilds, `CheckDiskNotInPool(disk, pools)` — verify disk not in any pool, `CheckDiskIsValidBlockDevice(disk)` — verify disk exists and is a block device, `CheckDiskIsFailed(pool, disk)` — verify disk is in failed state, `CheckMinimumDisksAfterRemoval(pool, disk)` — verify ≥2 disks remain, `CheckSignatureMatch(disk, poolID)` — verify signature matches pool, `CheckArrayConsistency(arrayDevice)` — verify array state is compatible with operation
    - Each check returns a descriptive error message on failure per the error table in the design
    - _Requirements: 2.7, 2.8, 2.9, 3.5, 3.6, 3.8, 4.1, 4.8, 11.1, 11.2, 11.4, 11.5, 11.6_

  - [ ]* 4.3 Write property test for PoolForge_Signature invariant (P50)
    - **Property 50: PoolForge_Signature invariant**
    - Generate random pool configurations with disk sets, verify: every disk in a pool has a signature matching the pool ID, every disk not in any pool has no signature. Simulate add-disk (signature written), remove-disk (signature wiped), delete-pool (all signatures wiped).
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 11.3, 11.4**

  - [ ]* 4.4 Write unit tests for pre-operation checks
    - Create or extend `internal/engine/engine_test.go`: test each pre-operation check with passing and failing inputs, verify correct error messages match the design error table
    - _Requirements: 12.1_

- [ ] 5. HealthMonitor implementation
  - [ ] 5.1 Implement HealthMonitor component
    - Create `internal/monitor/monitor.go` implementing the `HealthMonitor` interface
    - Implement `Start`: spawn goroutine running `mdadm --monitor --scan` in follow mode, parse stdout for `Fail`, `SpareActive`, `RebuildFinished` events
    - Implement `Stop`: cancel context, wait for goroutine shutdown
    - Implement `OnDiskFailure`/`OnRebuildComplete`: register handler callbacks
    - Implement event parsing: extract disk descriptor, array device, event type from mdadm output
    - Implement pool resolution: look up which pool owns the affected array via metadata
    - Guarantee: failure events processed within 10 seconds of receipt
    - _Requirements: 1.7, 1.8_

  - [ ] 5.2 Implement rebuild progress poller
    - Add progress polling goroutine to HealthMonitor: reads `/proc/mdstat` every 5 seconds during active rebuilds
    - Parse rebuild percentage and estimated finish time from mdstat output
    - Update rebuild progress in metadata store via `SavePool`
    - _Requirements: 1.9, 7.2_

  - [ ]* 5.3 Write unit tests for HealthMonitor
    - Create `internal/monitor/monitor_test.go`
    - Test event parsing with sample mdadm monitor output lines (Fail, SpareActive, RebuildFinished)
    - Test handler registration and dispatch
    - Test pool resolution from array device to pool ID
    - Test progress poller parsing of `/proc/mdstat` samples
    - Test graceful shutdown
    - _Requirements: 12.1, 12.3_

- [ ] 6. Checkpoint — Verify safety primitives and HealthMonitor
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 7. Add-disk engine logic
  - [ ] 7.1 Implement `AddDisk` method on EngineService
    - Implement in `internal/engine/engine_impl.go`
    - Pre-operation checks: arrays healthy, no rebuild in progress, disk not in any pool, valid block device, check for existing data
    - Load pool, compute new slices for existing tiers the new disk can satisfy
    - Partition new disk: `CreateGPTPartitionTable` + `CreatePartition` per matching tier
    - For each matching tier: `AddMember` + `ReshapeArray` with correct RAID level per parity mode and new eligible count
    - Handle Case A (same size), Case B (larger than all), Case C (smaller than smallest), Case D (between existing)
    - For leftover capacity: compute new tiers, create new RAID arrays, create PVs, `ExtendVolumeGroup`
    - Post-operations: `ExtendLogicalVolume`, `ResizeFilesystem`, `WritePoolForgeSignature`, `SavePool`
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8, 2.9, 2.10_

  - [ ]* 7.2 Write property test for add-disk slicing (P17)
    - **Property 17: Add-disk slicing matches existing tiers**
    - Generate random existing pool (≥2 disks, computed tiers) and a new disk of random capacity, verify: new disk is partitioned into slices matching existing tiers for which cumulative boundary ≤ new disk capacity, each slice is added to the corresponding RAID array via reshape
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 2.1, 2.2**

  - [ ]* 7.3 Write property test for add-disk with larger disk (P18)
    - **Property 18: Add-disk with larger disk creates new tiers**
    - Generate random pool and a new disk larger than all existing disks, verify: new tiers computed from leftover capacity, new RAID arrays created for tiers with ≥2 eligible disks, new arrays added to Volume Group
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 2.3**

  - [ ]* 7.4 Write property test for add-disk LV/fs expansion (P19)
    - **Property 19: Add-disk expands LV and resizes filesystem**
    - Generate random pool and add-disk operation, verify: LV extended to use all new VG space, filesystem resized, total usable capacity ≥ capacity before add-disk
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 2.4**

  - [ ]* 7.5 Write property test for reshape parity mode preservation (P20)
    - **Property 20: Reshape preserves parity mode**
    - Generate random pool with parity mode and random add-disk/remove-disk operations, verify: RAID level after reshape matches parity mode + new eligible count per selection table (SHR-1: ≥3→RAID5, 2→RAID1; SHR-2: ≥4→RAID6, 3→RAID5, 2→RAID1)
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 2.5**

  - [ ]* 7.6 Write unit tests for add-disk
    - Extend `internal/engine/engine_test.go`
    - Test Case A: add 2 TB disk to pool with [1 TB, 2 TB, 4 TB] → verify slices match tiers 0 and 1, arrays reshaped
    - Test Case B: add 8 TB disk to pool with [1 TB, 2 TB, 4 TB] → verify all tiers matched + new tier from leftover
    - Test Case C: add 500 GB disk to pool with [1 TB, 2 TB] → verify new smallest tier created, existing disks repartitioned
    - Test Case D: add 3 TB disk to pool with [1 TB, 2 TB, 4 TB] → verify partial tier matching
    - Test rejection: disk already in pool, disk in another pool, arrays not healthy, rebuild in progress
    - _Requirements: 12.1_

- [ ] 8. Replace-disk engine logic
  - [ ] 8.1 Implement `ReplaceDisk` method on EngineService
    - Implement in `internal/engine/engine_impl.go`
    - Pre-operation checks: old disk is in failed state, new disk is valid block device, new disk not in any pool
    - Load pool, get failed disk's slice layout
    - Partition replacement disk to match failed disk's slices for all tiers the replacement can satisfy
    - For each degraded array that had a slice from failed disk: `AddMember` with replacement slice (triggers rebuild)
    - If replacement larger than failed disk: compute new tiers from leftover, follow add-disk expansion logic
    - If replacement smaller: partition for all satisfiable tiers, log warning for unsatisfiable tiers
    - Post-operations: `WritePoolForgeSignature`, update metadata (replace failed disk entry with new disk)
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8_

  - [ ]* 8.2 Write property test for replace-disk partitioning (P40)
    - **Property 40: Replace-disk partitions replacement to match failed disk layout**
    - Generate random pool with a failed disk and a valid replacement disk, verify: replacement partitioned with slices matching failed disk's layout for all satisfiable tiers, each replacement slice added to corresponding degraded array
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 3.1, 3.2**

  - [ ]* 8.3 Write property test for larger replacement expansion (P41)
    - **Property 41: Larger replacement disk triggers expansion**
    - Generate random pool with failed disk and replacement disk larger than failed disk, verify: additional capacity beyond failed disk's tier coverage creates new tiers or extends existing tiers per add-disk algorithm
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 3.3**

  - [ ]* 8.4 Write unit tests for replace-disk
    - Extend `internal/engine/engine_test.go`
    - Test replace 2 TB failed disk with 2 TB → verify slice layout match, rebuild initiated on all degraded arrays
    - Test replace 2 TB failed disk with 3 TB → verify rebuild + expansion from leftover
    - Test replace 2 TB failed disk with 1 TB → verify partial rebuild + warning for unsatisfiable tiers
    - Test rejection: old disk not failed, new disk already in pool, new disk not valid block device
    - _Requirements: 12.1, 12.5_

- [ ] 9. Remove-disk engine logic
  - [ ] 9.1 Implement downgrade evaluation algorithm
    - Create `internal/engine/downgrade.go`
    - Implement `EvaluateDowngradePaths(pool, diskToRemove)` returning a `DowngradeReport`
    - For each array containing slices from the disk: compute new member count, determine new RAID level per selection table, flag if downgrade required, reject if <2 members would remain
    - For tiers where the disk is the only eligible disk: flag tier destruction
    - Aggregate: total capacity reduction, per-array RAID level changes, whether removal is safe
    - _Requirements: 4.1, 4.2, 4.3, 4.4_

  - [ ] 9.2 Implement `RemoveDisk` method on EngineService
    - Implement in `internal/engine/engine_impl.go`
    - Pre-operation checks: ≥2 disks remain after removal
    - Run downgrade evaluation; if unsafe → return error; if downgrade required → return downgrade report for CLI confirmation
    - On confirmation: for each array with slices from removed disk: `RemoveMember` + `ReshapeArray` to new count/level
    - Recalculate VG capacity, `ReduceLogicalVolume` + `ResizeFilesystem` if capacity decreased
    - `WipePartitionTable` on removed disk (clears signature)
    - Update metadata: remove disk, update array configs
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8_

  - [ ]* 9.3 Write property test for downgrade evaluation (P42)
    - **Property 42: Remove-disk downgrade evaluation correctness**
    - Generate random pools and random disk removal proposals, verify: (a) removal rejected if any array would have <2 members, (b) new RAID level per array matches selection table for new member count, (c) tier destruction flagged when disk is sole eligible disk for a tier
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 4.1, 4.2**

  - [ ]* 9.4 Write property test for remove-disk reshape and resize (P43)
    - **Property 43: Remove-disk reshapes arrays and resizes LV/filesystem**
    - Generate random pool and confirmed disk removal, verify: each affected array reshaped to new member count and RAID level, LV and filesystem resized to match new VG capacity, PoolForge_Signature wiped from removed disk
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 4.5, 4.6**

  - [ ]* 9.5 Write unit tests for remove-disk
    - Extend `internal/engine/engine_test.go`
    - Test remove from 4-disk RAID 5 → 3-disk RAID 5 (no downgrade)
    - Test remove from 3-disk RAID 5 → 2-disk RAID 1 (downgrade)
    - Test remove from 2-disk pool → rejection (minimum 2 disks)
    - Test remove disk that is sole eligible for a tier → tier destruction
    - Test SHR-2 downgrade paths: RAID 6 → RAID 5, RAID 5 → RAID 1
    - _Requirements: 12.1_

- [ ] 10. Checkpoint — Verify add-disk, replace-disk, remove-disk
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 11. Delete-pool engine logic
  - [ ] 11.1 Implement `DeletePool` method on EngineService
    - Implement in `internal/engine/engine_impl.go`
    - Load pool, check for active rebuilds (warn if rebuilding, require confirmation)
    - Teardown sequence: `UnmountFilesystem` → `RemoveLogicalVolume` → `RemoveVolumeGroup` → for each array: `RemovePhysicalVolume` + `StopArray` → for each disk: `WipePartitionTable`
    - Delete pool from metadata store via `MetadataStore.DeletePool`
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5_

  - [ ]* 11.2 Write property test for pool deletion cleanup (P44)
    - **Property 44: Pool deletion cleans up all resources and metadata**
    - Generate random pool, delete it, verify: filesystem unmounted, LV removed, VG removed, all arrays stopped/destroyed, all disk signatures wiped, pool entry removed from metadata. No pool resources remain.
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 5.2, 5.3**

  - [ ]* 11.3 Write property test for pool deletion isolation (P8)
    - **Property 8: Pool deletion preserves other pools**
    - Generate multi-pool system (≥2 pools), delete one pool, verify: all other pools' arrays, VGs, LVs, mount points, and metadata are completely intact and unchanged
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 5.4, 6.2**

  - [ ]* 11.4 Write unit tests for delete-pool
    - Extend `internal/engine/engine_test.go`
    - Test delete single pool → all resources cleaned up
    - Test delete one pool in multi-pool system → other pool untouched
    - Test delete pool with active rebuild → warning + confirmation required
    - _Requirements: 12.1_

- [ ] 12. Self-healing engine logic
  - [ ] 12.1 Implement `HandleDiskFailure` method on EngineService
    - Implement in `internal/engine/engine_impl.go`
    - Load pool, mark disk as failed with timestamp in metadata
    - Identify all RAID arrays containing slices from failed disk, mark as degraded
    - Log failure with disk descriptor and timestamp
    - If hot spare available: for each degraded array, `AddMember` with spare slice to initiate rebuild
    - Handle double failure: SHR-1 → mark arrays as failed + critical alert; SHR-2 (RAID 6) → remain degraded + warning alert
    - Save updated pool state to metadata
    - _Requirements: 1.1, 1.2, 1.4, 1.5, 1.6, 1.8_

  - [ ] 12.2 Implement `GetRebuildProgress` method on EngineService
    - Implement in `internal/engine/engine_impl.go`
    - Load pool, look up rebuild progress for specified array from metadata
    - If rebuild active: query `RAIDManager.GetSyncStatus` for current percentage and ETA
    - Return `RebuildProgress` struct with state, percentage, ETA, target disk, timestamps
    - _Requirements: 1.3, 1.9, 7.2_

  - [ ] 12.3 Wire HealthMonitor to engine
    - In application startup (or engine constructor), create HealthMonitor instance
    - Register `OnDiskFailure` handler that calls `Engine.HandleDiskFailure`
    - Register `OnRebuildComplete` handler that updates metadata to reflect restored healthy state and logs completion
    - Start HealthMonitor in background goroutine
    - Ensure graceful shutdown on application exit
    - _Requirements: 1.7, 1.8_

  - [ ]* 12.4 Write property test for disk failure metadata update (P14)
    - **Property 14: Disk failure updates metadata and creates log entry**
    - Generate random pool and random disk failure event, verify: disk marked failed with timestamp, all arrays containing slices from failed disk marked degraded, log entry created
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 1.1, 1.8**

  - [ ]* 12.5 Write property test for auto-rebuild on spare (P15)
    - **Property 15: Auto-rebuild on spare availability**
    - Generate random pool with degraded arrays and available hot spare, verify: rebuild initiated on all degraded arrays using spare slices
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 1.2, 1.4**

  - [ ]* 12.6 Write property test for rebuild completion (P16)
    - **Property 16: Rebuild completion restores metadata and logs**
    - Generate random completed rebuild scenario, verify: metadata reflects healthy state for rebuilt array, completion log entry created with array identifier and timestamp
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 1.3**

  - [ ]* 12.7 Write property test for double failure behavior (P38)
    - **Property 38: Double failure behavior depends on parity mode**
    - Generate random pool with active rebuild and second disk failure on same array, verify: SHR-1 → array marked failed; SHR-2 (RAID 6) → array remains degraded. Both cases log both failed disk descriptors.
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 1.5, 1.6**

  - [ ]* 12.8 Write property test for rebuild progress status (P45)
    - **Property 45: Rebuild progress status reporting**
    - Generate random pool with actively rebuilding array, verify: status includes rebuild percentage (0-100), estimated time remaining, target disk descriptor
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 7.2**

  - [ ]* 12.9 Write unit tests for self-healing
    - Extend `internal/engine/engine_test.go`
    - Test single disk failure → disk marked failed, arrays degraded, rebuild with spare
    - Test double failure SHR-1 → arrays marked failed, critical alert
    - Test double failure SHR-2 → arrays remain degraded, warning alert
    - Test failure with no spare → warning logged, no rebuild
    - Test rebuild progress query during active rebuild
    - _Requirements: 12.1, 12.3_

- [ ] 13. Checkpoint — Verify delete-pool and self-healing
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 14. Unallocated capacity detection and expansion
  - [ ] 14.1 Implement `DetectUnallocated` and `ExpandPool` methods on EngineService
    - Implement in `internal/engine/engine_impl.go`
    - `DetectUnallocated`: load pool, for each member disk compare total capacity vs sum of assigned slice sizes, compute unallocated bytes per disk, return `UnallocatedReport`
    - `ExpandPool`: load pool, detect unallocated capacity, compute new tiers from unallocated space, create new RAID arrays (for tiers with ≥2 eligible disks), create PVs, `ExtendVolumeGroup`, `ExtendLogicalVolume`, `ResizeFilesystem`, update metadata
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6_

  - [ ]* 14.2 Write property test for expansion from unallocated capacity (P46)
    - **Property 46: Expansion from unallocated capacity**
    - Generate random pool with unallocated capacity on member disks, approve expansion, verify: new tiers computed, new RAID arrays created for tiers with ≥2 eligible disks, arrays added to VG, LV extended, filesystem resized, metadata updated
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 8.4, 8.6**

  - [ ]* 14.3 Write unit tests for unallocated capacity detection and expansion
    - Extend `internal/engine/engine_test.go`
    - Test detect unallocated on pool after add-disk with larger disk → correct unallocated bytes per disk
    - Test expand pool with unallocated capacity → new tiers, arrays, LV/fs extended
    - Test detect unallocated on fully allocated pool → zero unallocated
    - _Requirements: 12.1_

- [ ] 15. Pool configuration serialization
  - [ ] 15.1 Implement `ExportPool` and `ImportPool` methods on EngineService
    - Create `internal/engine/serialization/serialization.go`
    - `ExportPool`: load pool from metadata, convert to `PoolConfiguration` struct, marshal to JSON with sorted keys and 2-space indent for deterministic output, include `schema_version: 1`
    - `ImportPool`: parse JSON, validate schema version, validate all required fields present, validate disk descriptors exist and are accessible, validate disks not already in use, apply configuration
    - _Requirements: 9.1, 9.2, 9.3, 9.4, 9.5, 9.6_

  - [ ]* 15.2 Write property test for export determinism (P47)
    - **Property 47: Export produces complete deterministic JSON**
    - Generate random valid pool configurations, export twice, verify: byte-identical JSON output, all required fields present (pool name, parity mode, disks, tiers, arrays, VG, LV, mount point, schema version)
    - File: `internal/engine/serialization/serialization_prop_test.go`
    - **Validates: Requirements 9.1, 9.4**

  - [ ]* 15.3 Write property test for import validation (P48)
    - **Property 48: Import validates JSON structure**
    - Generate random invalid JSON inputs (missing fields, invalid schema version, non-existent disk descriptors, malformed JSON), verify: each rejected with descriptive error. Generate valid JSON inputs, verify: accepted.
    - File: `internal/engine/serialization/serialization_prop_test.go`
    - **Validates: Requirements 9.2, 9.3**

  - [ ]* 15.4 Write property test for serialization round-trip (P49)
    - **Property 49: Pool configuration serialization round-trip**
    - Generate random valid pool configurations, verify: `export(import(export(pool))) == export(pool)` — exporting, importing, then exporting again produces identical JSON
    - File: `internal/engine/serialization/serialization_prop_test.go`
    - **Validates: Requirements 9.5**

  - [ ]* 15.5 Write unit tests for serialization
    - Create `internal/engine/serialization/serialization_test.go`
    - Test export known pool → verify JSON structure, field ordering, indentation
    - Test import valid JSON → verify pool configuration matches
    - Test import invalid JSON (missing name, missing disks, bad schema version, non-existent disk) → verify descriptive error for each
    - _Requirements: 12.1, 12.7_

- [ ] 16. Multi-pool isolation and status enhancements
  - [ ] 16.1 Implement disk failure isolation across pools
    - Ensure `HandleDiskFailure` only modifies the pool containing the failed disk
    - Verify that metadata save for one pool does not touch other pool entries
    - Add explicit pool boundary checks in failure handling path
    - _Requirements: 6.1_

  - [ ] 16.2 Implement Phase 2 status enhancements
    - Extend `GetPoolStatus` in `internal/engine/engine_impl.go`
    - For degraded arrays: include failed/missing disk descriptor, affected tier index, sync state, RAID level, member descriptors with per-disk state, array capacity
    - For rebuilding arrays: include rebuild progress percentage, estimated time remaining, target disk descriptor
    - For failed disks: list all affected arrays (arrays that contained slices from the failed disk)
    - Extend `ArrayStatus` and `DiskStatusInfo` structs with Phase 2 fields per design
    - _Requirements: 7.1, 7.2, 7.3, 7.4_

  - [ ]* 16.3 Write property test for disk failure isolation (P9)
    - **Property 9: Disk failure isolation across pools**
    - Generate multi-pool system, trigger disk failure in one pool, verify: other pools' state, capacity, health, and metadata are completely unchanged
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 6.1**

  - [ ]* 16.4 Write property test for degraded array status (P11)
    - **Property 11: Degraded array status identifies failed disk and affected tier**
    - Generate random pool with degraded array, query status, verify: failed disk descriptor identified, affected tier index present, sync state from valid set, RAID level, member descriptors with per-disk state, array capacity
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 7.1, 7.4**

  - [ ]* 16.5 Write property test for failed disk affected arrays (P12)
    - **Property 12: Failed disk status lists all affected arrays**
    - Generate random pool with failed disk, query status, verify: every array that contained a slice from the failed disk is listed, count of affected arrays equals number of tiers the failed disk participated in
    - File: `internal/engine/engine_prop_test.go`
    - **Validates: Requirements 7.3**

- [ ] 17. Checkpoint — Verify expansion, serialization, isolation, and status
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 18. CLI commands for Phase 2 operations
  - [ ] 18.1 Implement `pool add-disk` CLI command
    - Add `add-disk` subcommand to `cmd/poolforge/main.go`
    - Parse: `pool add-disk <pool-name> --disk <device>`
    - Resolve pool name → ID, call `EngineService.AddDisk`
    - If existing data detected on disk: prompt for confirmation
    - Display summary on success (new capacity, tier changes), descriptive error on failure
    - _Requirements: 10.1, 10.8, 10.9_

  - [ ] 18.2 Implement `pool replace-disk` CLI command
    - Add `replace-disk` subcommand
    - Parse: `pool replace-disk <pool-name> --old <device> --new <device>`
    - Resolve pool name → ID, call `EngineService.ReplaceDisk`
    - Display summary on success (rebuild initiated, capacity changes), descriptive error on failure
    - _Requirements: 10.2, 10.8, 10.9_

  - [ ] 18.3 Implement `pool remove-disk` CLI command
    - Add `remove-disk` subcommand
    - Parse: `pool remove-disk <pool-name> --disk <device>`
    - Resolve pool name → ID, call `EngineService.RemoveDisk`
    - If downgrade required: display proposed RAID level changes, prompt for confirmation [y/N]
    - Display summary on success (new capacity, RAID level changes), descriptive error on failure
    - _Requirements: 10.3, 10.8, 10.9_

  - [ ] 18.4 Implement `pool delete` CLI command
    - Add `delete` subcommand
    - Parse: `pool delete <pool-name>`
    - Display warning: "All data will be destroyed. Confirm? [y/N]"
    - If rebuild in progress: additional warning about aborting rebuild
    - On confirmation: resolve pool name → ID, call `EngineService.DeletePool`
    - Display "Pool deleted" on success, descriptive error on failure
    - _Requirements: 10.4, 10.8, 10.9_

  - [ ] 18.5 Implement `pool expand` CLI command
    - Add `expand` subcommand
    - Parse: `pool expand <pool-name>`
    - Resolve pool name → ID, call `EngineService.DetectUnallocated` first to show unallocated capacity
    - If unallocated capacity exists: display details, call `EngineService.ExpandPool`
    - If no unallocated capacity: display "No unallocated capacity found"
    - Display summary on success, descriptive error on failure
    - _Requirements: 10.5, 10.8, 10.9_

  - [ ] 18.6 Implement `pool export` and `pool import` CLI commands
    - Add `export` subcommand: parse `pool export <pool-name> --output <file>`, call `EngineService.ExportPool`, write JSON to output file
    - Add `import` subcommand: parse `pool import --input <file>`, read JSON from input file, call `EngineService.ImportPool`
    - Display summary on success, descriptive error on failure
    - _Requirements: 10.6, 10.7, 10.8, 10.9_

  - [ ] 18.7 Extend `pool status` to display Phase 2 details
    - Update status display to show degraded array details (failed disk, tier, sync state)
    - Show rebuild progress (percentage, ETA, target disk) for rebuilding arrays
    - Show unallocated capacity if present
    - Show failed disk details with affected arrays
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 8.3_

- [ ] 19. Checkpoint — Verify CLI commands
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 20. Phase 1 regression verification
  - [ ] 20.1 Run all Phase 1 unit and property tests
    - Execute all tests in `internal/engine/`, `internal/storage/`, `internal/metadata/` — verify Phase 1 tests pass unchanged
    - Verify `CreatePool`, `GetPool`, `ListPools`, `GetPoolStatus` behavior unchanged
    - Verify CLI commands `pool create`, `pool status`, `pool list` work as before
    - _Requirements: 12.2_

- [ ] 21. Test infrastructure extensions
  - [ ] 21.1 Extend Terraform IaC for Phase 2 scenarios
    - Update `test/infra/main.tf`: add 2-3 spare EBS volumes (gp3, various sizes) for replacement and expansion tests
    - Update `test/infra/variables.tf`: add spare volume size variables
    - Update `test/infra/outputs.tf`: add spare volume device mappings
    - _Requirements: 13.6_

  - [ ] 21.2 Create EBS detach/attach scripts for failure simulation
    - Create `test/infra/scripts/detach_ebs.sh`: detach EBS volume by volume ID from running instance (simulates disk failure)
    - Create `test/infra/scripts/attach_ebs.sh`: attach EBS volume by volume ID to running instance (simulates disk replacement/expansion)
    - Both scripts accept volume ID and instance ID as parameters
    - _Requirements: 13.1, 13.2, 13.3_

  - [ ] 21.3 Extend Test_Runner for Phase 2 test scenarios
    - Update `test/infra/test_runner.sh`: add Phase 2 test phases for lifecycle operations, failure simulation (EBS detach/attach), rebuild progress collection from `/proc/mdstat`
    - Add full lifecycle test scenario orchestration
    - _Requirements: 13.4, 13.5_

- [ ] 22. Integration tests for Phase 2 operations
  - [ ] 22.1 Implement add-disk integration test
    - Create `test/integration/add_disk_test.go`
    - Attach a new EBS volume, run `pool add-disk`, verify reshape completion, LV/fs expansion, data integrity
    - _Requirements: 12.4, 13.2_

  - [ ] 22.2 Implement replace-disk integration test
    - Create `test/integration/replace_disk_test.go`
    - Detach an EBS volume (simulate failure), attach a new EBS volume, run `pool replace-disk`, verify rebuild completion and metadata update
    - _Requirements: 12.5, 13.3_

  - [ ] 22.3 Implement remove-disk integration test
    - Create `test/integration/remove_disk_test.go`
    - Run `pool remove-disk` on a multi-disk pool, verify downgrade evaluation, array reshape, LV/fs resize, data integrity
    - _Requirements: 12.1_

  - [ ] 22.4 Implement delete-pool integration test
    - Create `test/integration/delete_pool_test.go`
    - Create two pools, delete one, verify all resources cleaned up for deleted pool, other pool completely unaffected
    - _Requirements: 12.1_

  - [ ] 22.5 Implement self-healing integration test
    - Create `test/integration/self_healing_test.go`
    - Detach an EBS volume while PoolForge is running, verify HealthMonitor detects failure within 10 seconds, verify rebuild initiates with spare, verify rebuild completion restores healthy state
    - _Requirements: 12.3, 13.1_

  - [ ] 22.6 Implement expansion integration test
    - Create `test/integration/expansion_test.go`
    - Create pool, add disk to create unallocated capacity, run `pool expand`, verify new tiers, LV/fs expansion
    - _Requirements: 12.4_

  - [ ] 22.7 Implement export/import integration test
    - Create `test/integration/export_import_test.go`
    - Create pool, export config, verify JSON structure, import config, verify round-trip fidelity
    - _Requirements: 12.7_

  - [ ] 22.8 Implement full lifecycle integration test
    - Create `test/integration/full_lifecycle_test.go`
    - Full scenario: create pool → write data → detach EBS (failure) → verify rebuild → attach new EBS (add-disk) → replace a disk → remove a disk → export config → import config → verify data integrity → delete pool
    - _Requirements: 12.6, 13.4_

  - [ ] 22.9 Run Phase 1 integration tests as regression
    - Execute existing `test/integration/pool_create_test.go`, `pool_status_test.go`, `multi_pool_test.go`, `lifecycle_test.go` — verify all pass unchanged
    - _Requirements: 12.2_

- [ ] 23. Final checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation at logical boundaries
- Property tests use the `rapid` library (`github.com/flyingmutant/rapid`) with 100+ iterations each
- Unit tests use mocked storage interfaces; integration tests run against real EBS volumes via Test_Runner
- Storage abstraction implementations exec system commands (sgdisk, mdadm, pvcreate, etc.) — they require root privileges at runtime
- Integration tests (tasks 22.x) are designed to run inside the Test_Environment provisioned by tasks 21.x
- Phase 2 does NOT implement rollback of partial operations — that is Phase 4 scope
- All Phase 1 interfaces are extended additively — no breaking changes to existing method signatures
