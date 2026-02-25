# Requirements Document — Phase 2: Lifecycle Operations

## Introduction

This document specifies the requirements for Phase 2 of the PoolForge project. PoolForge is an open-source storage management tool for Ubuntu LTS (24.04+) that replicates Synology Hybrid RAID (SHR) functionality using mdadm and LVM. The full project scope is defined in the master spec at `.kiro/specs/hybrid-raid-manager/`. Phase 1 is defined at `.kiro/specs/poolforge-phase1-core-engine/`.

Phase 1 established the foundation: capacity-tier computation from mixed-size disks, GPT disk partitioning, mdadm RAID array creation, LVM stitching (PV → VG → LV), ext4 filesystem creation, a CLI for pool creation/status/list, a JSON-based metadata store with atomic writes, and the automated cloud-based test infrastructure (Terraform IaC for EC2 + EBS, Test_Runner script). Phase 1 defined and implemented the core interfaces: EngineService (CreatePool, GetPool, ListPools, GetPoolStatus), StorageAbstraction (DiskManager, RAIDManager, LVMManager, FilesystemManager), and MetadataStore (SavePool, LoadPool, ListPools).

Phase 2 builds on these interfaces to deliver lifecycle operations: adding disks to existing pools (with array reshape and filesystem expansion), replacing failed disks, removing disks, deleting pools, disk failure detection with automatic self-healing rebuilds, unallocated capacity detection with administrator-approved expansion, and pool configuration serialization (JSON export/import). Phase 2 also completes the multi-pool isolation and pool status criteria that were deferred from Phase 1.

This is Phase 2 of 4:

| Phase | Scope |
|-------|-------|
| Phase 1 ✅ | Core engine, CLI (create/status/list), metadata store, test infrastructure |
| **Phase 2** | **Lifecycle operations (add disk, replace disk, remove disk, delete pool, self-healing/rebuild, expansion, export/import)** |
| Phase 3 | Web portal (React), API server (Go REST), Storage_Map, Log_Viewer, authentication |
| Phase 4 | Safety hardening (atomic operations, rollback, multi-interface, SMART monitoring) |

Phase 2 extends the Phase 1 interfaces as follows:
- **EngineService**: Adds `AddDisk`, `ReplaceDisk`, `RemoveDisk`, `DeletePool`, `HandleDiskFailure`, `GetRebuildProgress`, `ExportPool`, `ImportPool`, `DetectUnallocated`, `ExpandPool` methods.
- **StorageAbstraction**: Adds `ReshapeArray`, `AddMember`, `RemoveMember` to RAIDManager. Adds `ExtendVolumeGroup`, `ReduceVolumeGroup` to LVMManager. Adds `ExtendLogicalVolume` to LVMManager. Adds `ResizeFilesystem` to FilesystemManager.
- **MetadataStore**: Adds `DeletePool` and disk failure state tracking. Schema remains version 1 with additional fields for disk state and rebuild progress.
- **HealthMonitor**: New component introduced in Phase 2 — listens for mdadm events and triggers self-healing workflows.

Phase 2 MUST NOT break any Phase 1 functionality. All existing CLI commands (pool create, pool status, pool list), metadata persistence, and test infrastructure continue to work unchanged.

## Glossary

- **PoolForge**: The open-source storage management tool being specified
- **Pool**: A named collection of physical disks managed by PoolForge, presented as a single logical volume. Each Pool is independent and isolated from other Pools on the same system
- **Capacity_Tier**: A group of equal-sized partition slices derived from the smallest common capacity across disks in the pool
- **Slice**: A partition on a physical disk sized to match a specific Capacity_Tier
- **RAID_Array**: An mdadm software RAID array composed of same-sized Slices from different disks
- **Volume_Group**: An LVM volume group that aggregates all RAID_Arrays in a single Pool as physical volumes
- **Logical_Volume**: An LVM logical volume created on top of the Volume_Group, presented as the usable storage
- **Parity_Mode**: The redundancy level — SHR-1 (single parity, RAID 5 behavior) or SHR-2 (double parity, RAID 6 behavior)
- **Disk_Descriptor**: A block device path (e.g., /dev/sdb) identifying a physical disk managed by PoolForge
- **Partition_Table**: The GPT partition layout PoolForge creates on each managed disk
- **Metadata_Store**: A persistent record of Pool configuration, disk membership, Capacity_Tiers, and RAID_Array mappings, stored as JSON with atomic writes
- **Rebuild**: The process of reconstructing redundancy in a degraded RAID_Array after a disk failure, by syncing data onto a replacement or spare disk
- **Reshape**: The process of modifying an existing RAID_Array geometry to accommodate a new member disk (e.g., growing a 3-disk RAID 5 to a 4-disk RAID 5)
- **Hot_Plug_Event**: A hardware event in which a disk is physically connected or disconnected while the system is running
- **Hot_Spare**: A disk or partition pre-assigned to a Pool that is not actively part of any RAID_Array but is available for automatic Rebuild when a failure occurs
- **Degraded_State**: The condition of a RAID_Array operating with fewer members than its configured redundancy level, still functional but with reduced fault tolerance
- **Failed_State**: The condition of a RAID_Array that has lost more members than its redundancy level can tolerate, resulting in data unavailability
- **Self_Healing**: The automatic process by which PoolForge detects a disk failure and initiates Rebuild operations without administrator intervention
- **Expansion**: The process of increasing a Pool's usable capacity by adding a new disk, reshaping arrays, and extending the Logical_Volume and filesystem
- **Downgrade**: The reduction of a RAID_Array's redundancy level (e.g., RAID 5 to RAID 1) when a disk is removed and the remaining member count requires a lower RAID level
- **Pre_Operation_Check**: A set of validation and consistency checks performed by PoolForge before executing any destructive or modifying operation to confirm data safety
- **PoolForge_Signature**: A metadata marker written to the Partition_Table of each managed disk, identifying the disk as belonging to a specific Pool
- **Pool_Configuration**: The complete serializable state of a Pool including disk membership, Capacity_Tiers, RAID_Array mappings, Volume_Group, Logical_Volume, and Parity_Mode
- **Round_Trip**: The property that exporting a Pool_Configuration to JSON and importing it back produces an equivalent configuration
- **Unallocated_Capacity**: Disk space on Pool member disks that is not currently assigned to any Capacity_Tier or RAID_Array, typically resulting from a previous add-disk or replace-disk operation
- **HealthMonitor**: A background service that listens for mdadm disk failure events and udev Hot_Plug_Events, triggering Self_Healing workflows
- **Test_Environment**: An automated, cloud-provisioned infrastructure on AWS consisting of EC2 instances with attached EBS volumes, used to simulate real disk operations
- **EBS_Volume**: An Amazon Elastic Block Store volume attached to an EC2 instance, used in the Test_Environment to simulate a physical disk

## Requirements

### Requirement 1: Disk Failure Detection and Self-Healing

**User Story:** As a system administrator, I want the system to detect disk failures and automatically begin rebuilding degraded arrays, so that data remains protected with minimal manual intervention.

#### Acceptance Criteria

1. WHEN mdadm reports a disk failure event for a disk in a Pool, THE PoolForge SHALL mark the disk as failed in the Metadata_Store and log the failure with the Disk_Descriptor and timestamp.
2. WHEN a Hot_Spare disk is available in the Pool, THE PoolForge SHALL automatically initiate a Rebuild of each degraded RAID_Array using the spare disk.
3. WHEN a Rebuild completes for a RAID_Array, THE PoolForge SHALL update the Metadata_Store to reflect the restored healthy state and log the completion with the RAID_Array identifier and timestamp.
4. IF multiple RAID_Arrays are degraded due to the same disk failure, THEN THE PoolForge SHALL rebuild all affected RAID_Arrays using the replacement or spare disk.
5. IF a second disk fails while a Rebuild is in progress in SHR-1 Parity_Mode, THEN THE PoolForge SHALL mark the affected RAID_Arrays as failed and log a critical alert identifying the two failed Disk_Descriptors.
6. IF a second disk fails while a Rebuild is in progress in SHR-2 Parity_Mode, THEN THE PoolForge SHALL continue operating in Degraded_State and log a warning alert identifying the two failed Disk_Descriptors.
7. THE HealthMonitor SHALL listen for mdadm events continuously while the PoolForge service is running, processing failure events within 10 seconds of receipt.
8. WHEN the HealthMonitor detects a disk failure, THE HealthMonitor SHALL identify all RAID_Arrays containing Slices from the failed disk and trigger Rebuild for each degraded array.
9. THE PoolForge SHALL persist Rebuild progress in the Metadata_Store so that Rebuild state survives service restarts.

### Requirement 2: Add a New Disk to an Existing Pool

**User Story:** As a system administrator, I want to add a new disk of any size to an existing pool, so that I can expand storage capacity without recreating the pool.

#### Acceptance Criteria

1. WHEN the administrator adds a new Disk_Descriptor to an existing Pool, THE PoolForge SHALL partition the new disk into Slices matching the existing Capacity_Tiers for which the disk has sufficient capacity.
2. WHEN the new disk produces Slices for existing Capacity_Tiers, THE PoolForge SHALL add each Slice to the corresponding RAID_Array by reshaping the array to include the new member.
3. WHEN the new disk has remaining capacity beyond all existing Capacity_Tiers, THE PoolForge SHALL compute new Capacity_Tiers from the leftover space, create new RAID_Arrays from the new Slices, and add them to the Volume_Group.
4. WHEN all RAID_Arrays are updated, THE PoolForge SHALL extend the Logical_Volume to use the newly available space in the Volume_Group and resize the ext4 filesystem to fill the expanded Logical_Volume.
5. WHEN reshaping a RAID_Array, THE PoolForge SHALL maintain the existing Parity_Mode redundancy level throughout the Reshape operation.
6. IF the new disk is smaller than the smallest existing Capacity_Tier, THEN THE PoolForge SHALL create a new smallest Capacity_Tier, repartition existing disks to include the new tier, and reshape all affected RAID_Arrays.
7. IF the Disk_Descriptor refers to a disk already in the Pool, THEN THE PoolForge SHALL reject the request with an error identifying the duplicate disk.
8. IF the Disk_Descriptor refers to a disk already in another Pool, THEN THE PoolForge SHALL reject the request with an error identifying the conflicting disk and the owning Pool.
9. THE PoolForge SHALL perform a Pre_Operation_Check before adding a disk, verifying that all existing RAID_Arrays are in a healthy state and no Rebuild is in progress.
10. THE PoolForge SHALL update the Metadata_Store with the new disk membership, updated Capacity_Tiers, and modified RAID_Array configurations after a successful add-disk operation.

### Requirement 3: Replace a Failed Disk

**User Story:** As a system administrator, I want to replace a failed disk with a new disk, so that I can restore full redundancy to my storage pool.

#### Acceptance Criteria

1. WHEN the administrator specifies a failed Disk_Descriptor and a replacement Disk_Descriptor, THE PoolForge SHALL partition the replacement disk to match the Slice layout of the failed disk for all Capacity_Tiers the failed disk participated in.
2. WHEN the replacement disk is partitioned, THE PoolForge SHALL add each replacement Slice to the corresponding degraded RAID_Array and initiate a Rebuild.
3. WHEN the replacement disk has greater capacity than the failed disk, THE PoolForge SHALL use the additional capacity to create new Capacity_Tiers or extend existing tiers, following the same logic as add-disk Expansion.
4. IF the replacement disk has less capacity than the failed disk, THEN THE PoolForge SHALL partition the replacement disk for all Capacity_Tiers it can satisfy and log a warning identifying the Capacity_Tiers that cannot be rebuilt due to insufficient replacement disk capacity.
5. IF the specified failed Disk_Descriptor is not in a failed state, THEN THE PoolForge SHALL reject the request with an error stating the disk is not failed.
6. IF the replacement Disk_Descriptor is already a member of any Pool, THEN THE PoolForge SHALL reject the request with an error identifying the conflicting Pool.
7. WHEN all Rebuilds complete, THE PoolForge SHALL update the Metadata_Store to replace the failed disk entry with the replacement disk entry and reflect the restored RAID_Array states.
8. THE PoolForge SHALL perform a Pre_Operation_Check before replacing a disk, verifying that the replacement disk is not in use and has a valid block device.

### Requirement 4: Remove a Disk from a Pool

**User Story:** As a system administrator, I want to remove a disk from a pool, so that I can repurpose the disk or reduce the pool size while preserving data integrity.

#### Acceptance Criteria

1. WHEN the administrator requests removal of a Disk_Descriptor from a Pool, THE PoolForge SHALL perform a Pre_Operation_Check to verify that removing the disk will not cause data loss in any RAID_Array.
2. IF removing the disk would reduce any RAID_Array below the minimum member count for its RAID level (2 for RAID 1, 3 for RAID 5, 4 for RAID 6), THEN THE PoolForge SHALL evaluate whether a Downgrade to a lower RAID level is possible while maintaining at least single-parity redundancy.
3. IF a safe Downgrade path exists, THEN THE PoolForge SHALL inform the administrator of the proposed RAID level changes and require explicit confirmation before proceeding.
4. IF removing the disk would result in data loss (no viable Downgrade path and insufficient remaining members for any redundant RAID level), THEN THE PoolForge SHALL reject the removal with an error explaining the data loss risk.
5. WHEN the administrator confirms the removal, THE PoolForge SHALL remove the disk Slices from each RAID_Array, reshape the arrays to the new member count and RAID level, and wipe the PoolForge_Signature from the removed disk.
6. WHEN all RAID_Arrays are reshaped after disk removal, THE PoolForge SHALL recalculate the Volume_Group capacity and resize the Logical_Volume and ext4 filesystem if the total capacity decreased.
7. THE PoolForge SHALL update the Metadata_Store to remove the disk from Pool membership and reflect the updated RAID_Array configurations.
8. IF the Pool has only two disks and the administrator requests removal of one, THEN THE PoolForge SHALL reject the removal with an error stating that a Pool requires a minimum of two disks.

### Requirement 5: Delete a Pool

**User Story:** As a system administrator, I want to delete a storage pool, so that I can reclaim all disks and clean up resources when a pool is no longer needed.

#### Acceptance Criteria

1. WHEN the administrator requests deletion of a Pool, THE PoolForge SHALL require explicit confirmation before proceeding, warning that all data on the Pool will be destroyed.
2. WHEN the administrator confirms deletion, THE PoolForge SHALL unmount the ext4 filesystem, remove the Logical_Volume, remove the Volume_Group, stop and destroy all RAID_Arrays belonging to the Pool, and wipe the PoolForge_Signature from all member disks.
3. WHEN all Pool resources are removed, THE PoolForge SHALL delete the Pool entry from the Metadata_Store.
4. WHEN a Pool is deleted on a multi-Pool system, THE PoolForge SHALL remove only the resources belonging to the deleted Pool, leaving all other Pools intact and operational.
5. IF any RAID_Array in the Pool is actively rebuilding, THEN THE PoolForge SHALL warn the administrator and require explicit confirmation that the rebuild will be aborted.

### Requirement 6: Multi-Pool Isolation (Phase 2 Completion)

**User Story:** As a system administrator, I want complete isolation between pools, so that failures and deletions in one pool never affect other pools.

*Note: Phase 1 established basic multi-pool creation and resource isolation (criteria 1-4 from master Requirement 2). Phase 2 completes the remaining isolation criteria now that lifecycle operations are implemented.*

#### Acceptance Criteria

1. WHEN a disk failure occurs, THE PoolForge SHALL limit the impact to the Pool containing the failed disk and leave all other Pools unaffected in state, capacity, and health.
2. WHEN the administrator deletes a Pool, THE PoolForge SHALL remove only the RAID_Arrays, Volume_Group, Logical_Volume, and Partition_Table entries belonging to that Pool, leaving all other Pools intact.

### Requirement 7: Pool Status (Phase 2 Completion)

**User Story:** As a system administrator, I want detailed status information about degraded arrays, rebuild progress, and failed disks, so that I can monitor recovery operations and understand the impact of failures.

*Note: Phase 1 established basic status reporting (criteria 1-4, 7, 9-10 from master Requirement 3). Phase 2 completes the degraded/rebuilding state details now that self-healing is implemented.*

#### Acceptance Criteria

1. WHEN a RAID_Array is in a Degraded_State, THE PoolForge SHALL identify the specific failed or missing Disk_Descriptor and the affected Capacity_Tier in the status output.
2. WHEN a RAID_Array is actively rebuilding, THE PoolForge SHALL report the Rebuild progress as a percentage, the estimated time remaining, and the Disk_Descriptor of the disk being rebuilt onto.
3. WHEN a disk is in a failed state, THE PoolForge SHALL identify all RAID_Arrays affected by that disk failure in the status output.
4. WHEN the administrator requests detailed status for a specific RAID_Array, THE PoolForge SHALL display the sync state (clean, active, resyncing, recovering, or degraded), the RAID level, the Capacity_Tier, the member Disk_Descriptors with per-disk state, and the array capacity.

### Requirement 8: Unallocated Capacity Detection and Administrator-Approved Expansion

**User Story:** As a system administrator, I want PoolForge to detect unallocated disk capacity and offer to expand the pool with my approval, so that I can reclaim wasted space without manually tracking disk utilization.

#### Acceptance Criteria

1. THE PoolForge SHALL periodically scan all Pool member disks for Unallocated_Capacity that is not assigned to any Capacity_Tier or RAID_Array.
2. WHEN Unallocated_Capacity is detected on one or more disks, THE PoolForge SHALL log an informational message identifying the disks and the amount of unallocated space.
3. WHEN the administrator requests Pool status and Unallocated_Capacity exists, THE PoolForge SHALL include the unallocated space details in the status output.
4. WHEN the administrator approves Expansion of a Pool with Unallocated_Capacity, THE PoolForge SHALL compute new Capacity_Tiers from the unallocated space, create new RAID_Arrays, add them to the Volume_Group, extend the Logical_Volume, and resize the ext4 filesystem.
5. THE PoolForge SHALL require explicit administrator approval before performing any Expansion using Unallocated_Capacity; automatic Expansion without approval is prohibited.
6. THE PoolForge SHALL update the Metadata_Store with the new Capacity_Tiers and RAID_Array configurations after a successful Expansion.

### Requirement 9: Pool Configuration Serialization

**User Story:** As a system administrator, I want to export and import pool configurations as JSON, so that I can back up pool metadata, migrate configurations between systems, and verify configuration integrity.

#### Acceptance Criteria

1. WHEN the administrator requests a Pool configuration export, THE PoolForge SHALL serialize the complete Pool_Configuration to a JSON file, including Pool name, Parity_Mode, disk membership with Disk_Descriptors and capacities, Capacity_Tiers with Slice sizes, RAID_Array mappings with RAID levels and member Slices, Volume_Group name, Logical_Volume name, and mount point.
2. WHEN the administrator provides a JSON configuration file for import, THE PoolForge SHALL validate the JSON structure and all referenced fields before applying the configuration.
3. IF the imported JSON contains invalid structure, missing required fields, or references to non-existent Disk_Descriptors, THEN THE PoolForge SHALL reject the import with a descriptive error identifying the validation failure.
4. THE PoolForge SHALL format exported JSON with consistent field ordering and indentation so that exports are deterministic and diff-friendly.
5. FOR ALL valid Pool_Configurations, exporting to JSON and then importing the JSON SHALL produce a Pool_Configuration equivalent to the original (Round_Trip property).
6. THE PoolForge SHALL include a schema version field in the exported JSON to support forward-compatible imports across PoolForge versions.

### Requirement 10: CLI Commands for Phase 2 Operations

**User Story:** As a system administrator, I want CLI commands for all lifecycle operations, so that I can manage pool expansion, disk replacement, removal, deletion, and configuration export/import from the command line.

#### Acceptance Criteria

1. THE PoolForge CLI SHALL provide a `pool add-disk <pool-name> --disk <device>` command that adds a new disk to an existing Pool.
2. THE PoolForge CLI SHALL provide a `pool replace-disk <pool-name> --old <device> --new <device>` command that replaces a failed disk with a new disk.
3. THE PoolForge CLI SHALL provide a `pool remove-disk <pool-name> --disk <device>` command that removes a disk from a Pool, with an interactive confirmation prompt when a Downgrade is required.
4. THE PoolForge CLI SHALL provide a `pool delete <pool-name>` command that deletes a Pool, with an interactive confirmation prompt warning of data destruction.
5. THE PoolForge CLI SHALL provide a `pool expand <pool-name>` command that triggers administrator-approved Expansion of Unallocated_Capacity.
6. THE PoolForge CLI SHALL provide a `pool export <pool-name> --output <file>` command that exports the Pool_Configuration to a JSON file.
7. THE PoolForge CLI SHALL provide a `pool import --input <file>` command that imports a Pool_Configuration from a JSON file.
8. WHEN any CLI command encounters an error, THE PoolForge CLI SHALL display a descriptive error message and exit with a non-zero exit code.
9. WHEN any CLI command completes successfully, THE PoolForge CLI SHALL display a summary of the operation performed and exit with a zero exit code.

### Requirement 11: Safety and Data Integrity

**User Story:** As a system administrator, I want PoolForge to validate all operations before execution and protect against accidental data loss, so that I can trust the tool with my storage infrastructure.

#### Acceptance Criteria

1. THE PoolForge SHALL validate that a Disk_Descriptor refers to a valid, accessible block device before any operation that modifies disk contents.
2. THE PoolForge SHALL check for existing data, filesystems, or partition tables on a disk before using the disk in a create, add-disk, or replace-disk operation, and warn the administrator if existing data is detected.
3. THE PoolForge SHALL write a PoolForge_Signature to the Partition_Table of each managed disk, identifying the disk as belonging to a specific Pool.
4. THE PoolForge SHALL verify PoolForge_Signature presence and Pool membership before any operation that modifies a managed disk.
5. THE PoolForge SHALL perform consistency checks on all RAID_Arrays before initiating a Reshape or Rebuild operation, verifying that the arrays are in a state compatible with the requested operation.
6. IF a Pre_Operation_Check fails, THEN THE PoolForge SHALL abort the operation and display a descriptive error identifying the failed check.

### Requirement 12: Testing (Phase 2 Scope)

**User Story:** As a developer, I want a comprehensive test suite for Phase 2 lifecycle operations, so that I can verify new functionality and confirm no regressions in Phase 1.

#### Acceptance Criteria

1. WHEN Phase 2 is completed, THE test suite SHALL include integration tests that validate all lifecycle operations introduced in Phase 2: add-disk, replace-disk, remove-disk, delete-pool, self-healing rebuild, expansion, and configuration export/import.
2. WHEN Phase 2 is completed, THE test suite SHALL include regression tests that confirm all Phase 1 functionality (pool creation, status, list, metadata persistence, tier computation) remains correct.
3. THE test suite SHALL include failure scenario tests that simulate disk failures (via EBS_Volume detach in the Test_Environment), verify automatic Rebuild initiation, and confirm Rebuild completion restores healthy state.
4. THE test suite SHALL include expansion tests that simulate adding a new disk (via EBS_Volume attach in the Test_Environment), verify Reshape completion, and confirm the Logical_Volume and ext4 filesystem are extended.
5. THE test suite SHALL include replacement tests that simulate disk failure followed by replacement with a new EBS_Volume, verifying Rebuild and metadata update.
6. THE test suite SHALL include a full lifecycle test scenario: create a Pool, write data, simulate a disk failure, verify Rebuild, expand the Pool by adding a disk, replace a disk, remove a disk, export configuration, import configuration, verify data integrity, and delete the Pool.
7. THE test suite SHALL include property-based tests for the Round_Trip property of Pool configuration serialization (export → import → export produces identical JSON).
8. THE integration tests and failure scenario tests SHALL execute against the cloud-based Test_Environment using EC2 instances with attached EBS_Volumes.

### Requirement 13: Test Infrastructure (Phase 2 Scenarios)

**User Story:** As a developer, I want the cloud-based test environment to support Phase 2 test scenarios, so that I can test lifecycle operations against real block devices.

#### Acceptance Criteria

1. THE Test_Environment SHALL support disk failure simulation by detaching an EBS_Volume from the running EC2 instance while PoolForge is managing a Pool that includes the corresponding Disk_Descriptor.
2. THE Test_Environment SHALL support Pool expansion simulation by attaching a new EBS_Volume to the running EC2 instance and executing the add-disk workflow.
3. THE Test_Environment SHALL support disk replacement simulation by detaching a failed EBS_Volume and attaching a new EBS_Volume of the same or different size.
4. THE Test_Environment SHALL execute a full lifecycle test scenario: create a Pool with mixed-size EBS_Volumes, write data, detach an EBS_Volume to simulate failure, verify Rebuild, attach a new EBS_Volume and add to Pool, replace a disk, verify data integrity after all operations.
5. THE Test_Runner SHALL collect Rebuild progress logs and timing data from the EC2 instance for each failure and recovery test scenario.
6. THE IaC_Template SHALL provision sufficient EBS_Volumes to support all Phase 2 test scenarios, including spare volumes for replacement and expansion tests.

## Extensibility Notes

The following interfaces are extended in Phase 2 from the Phase 1 foundation:

- **EngineService**: Phase 2 adds `AddDisk`, `ReplaceDisk`, `RemoveDisk`, `DeletePool`, `HandleDiskFailure`, `GetRebuildProgress`, `ExportPool`, `ImportPool`, `DetectUnallocated`, `ExpandPool`. Phase 3 will expose these methods via REST API endpoints without modification.
- **StorageAbstraction**: Phase 2 adds `ReshapeArray`, `AddMember`, `RemoveMember` to RAIDManager. Adds `ExtendVolumeGroup`, `ReduceVolumeGroup`, `ExtendLogicalVolume`, `ReduceLogicalVolume` to LVMManager. Adds `ResizeFilesystem` to FilesystemManager. Phase 4 will wrap these with atomic operation semantics.
- **MetadataStore**: Phase 2 adds `DeletePool` and extends the pool schema with disk failure state, rebuild progress tracking, and PoolForge_Signature fields. Schema remains version 1 with backward-compatible additions.
- **HealthMonitor**: New in Phase 2. Phase 3 will expose rebuild progress via WebSocket for real-time UI updates. Phase 4 will integrate SMART monitoring into the same event pipeline.
