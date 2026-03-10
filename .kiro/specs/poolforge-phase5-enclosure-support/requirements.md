# Requirements Document — Phase 5: External Enclosure Support

## Introduction

This document specifies the requirements for Phase 5 of the PoolForge project — a new phase added after the original four. PoolForge is an open-source storage management tool for Ubuntu LTS (24.04+) that replicates hybrid RAID functionality using mdadm and LVM. The full project scope is defined in the master spec at `.kiro/specs/hybrid-raid-manager/`. Phase 1 is defined at `.kiro/specs/poolforge-phase1-core-engine/`. Phase 2 is defined at `.kiro/specs/poolforge-phase2-lifecycle/`. Phase 3 is defined at `.kiro/specs/poolforge-phase3-web-portal/`. Phase 4 is defined at `.kiro/specs/poolforge-phase4-safety/`.

Phase 1 established the core engine: capacity-tier computation, GPT disk partitioning, mdadm RAID array creation, LVM stitching, ext4 filesystem creation, CLI, JSON metadata store, and cloud-based test infrastructure. Phase 2 delivered lifecycle operations: add-disk, replace-disk, remove-disk, delete-pool, self-healing rebuild, expansion, and export/import. Phase 3 layered the React Web_Portal, Go API_Server, Storage_Map visualization, Log_Viewer, and authentication. Phase 4 hardened safety with atomic operations, rollback, multi-interface disk support, and SMART monitoring.

Phase 5 addresses real-world issues discovered when using PoolForge with external eSATA and USB drive enclosures that can be powered off independently from the host system. The core problems are:

1. mdadm auto-assembles RAID arrays at boot before external drives are detected, causing arrays to start in a degraded state and triggering unnecessary full rebuilds.
2. Device names change after power cycles (e.g., /dev/sdf becomes /dev/sdj), causing re-add operations to fail when they reference stale device paths.
3. PoolForge generates mdadm.conf with explicit ARRAY definitions that override the AUTO -all directive, causing auto-assembly even when the administrator has disabled it.
4. There is no safe, sequenced shutdown or startup procedure for pools on external enclosures.
5. Drives that are temporarily absent (enclosure powered off) are re-added via `mdadm --add` (full rebuild) instead of `mdadm --re-add` (fast bitmap-based recovery).

Phase 5 solves these problems by introducing per-pool auto-start control, explicit pool start and stop commands with correct tier ordering, automatic degraded array repair via UUID-based drive matching and mdadm --re-add, mdadm.conf generation that respects manual-start pools, and mdadm systemd service management during installation. The Web_Portal and API_Server are extended with start/stop controls and external pool status indicators.

This is Phase 5 of 5:

| Phase | Scope |
|-------|-------|
| Phase 1 ✅ | Core engine, CLI (create/status/list), metadata store, test infrastructure |
| Phase 2 ✅ | Lifecycle operations (add disk, replace disk, remove disk, delete pool, self-healing/rebuild, expansion, export/import) |
| Phase 3 ✅ | Web portal (React), API server (Go REST), Storage_Map, Log_Viewer, authentication |
| Phase 4 ✅ | Safety hardening (atomic operations, rollback, multi-interface, SMART monitoring) |
| **Phase 5** | **External enclosure support (pool start/stop, auto-start control, mdadm.conf fix, degraded array auto-repair, Web_Portal/API extensions)** |

Phase 5 extends the following interfaces:
- **EngineService**: Adds `StartPool`, `StopPool`, `SetAutoStart` methods. All existing methods are unchanged.
- **RAIDManager**: Adds `AssembleArrayBySuperblock`, `ReAddMember` (UUID-based fast re-add), `StopArray` methods. All existing methods are unchanged.
- **MetadataStore**: Adds `is_external`, `requires_manual_start`, `last_shutdown`, `last_startup` fields to pool metadata. Schema remains version 1 with backward-compatible additions.
- **BootConfig** (`internal/safety/boot.go`): Modified to include AUTO -all directive and conditionally emit ARRAY definitions based on the `requires_manual_start` flag.
- **API_Server**: Adds POST /api/pools/:name/start, POST /api/pools/:name/stop, PUT /api/pools/:name/autostart endpoints. All existing endpoints are unchanged.
- **CLI**: Adds `pool start`, `pool stop`, `pool set-autostart` commands. All existing commands are unchanged.
- **Web_Portal**: Adds start/stop buttons for external pools, pool operational status indicators (Running, Offline, Safe_To_Power_Down), and external enclosure warning banners. All existing pages and components are unchanged.
- **Installer**: Adds mdadm systemd service disablement (mdmonitor, mdadm, mdadm-waitidle) during installation.

Phase 5 MUST NOT break any Phase 1, Phase 2, Phase 3, or Phase 4 functionality. Internal pools that auto-start at boot continue working exactly as before. All existing CLI commands, API endpoints, Web_Portal pages, metadata persistence, self-healing, atomic operations, rollback, SMART monitoring, and test infrastructure continue to work unchanged. The new functionality is strictly additive.

## Glossary

- **PoolForge**: The open-source storage management tool being specified
- **Pool**: A named collection of physical disks managed by PoolForge, presented as a single logical volume
- **Capacity_Tier**: A group of equal-sized partition slices derived from the smallest common capacity across disks in the pool
- **Slice**: A partition on a physical disk sized to match a specific Capacity_Tier
- **RAID_Array**: An mdadm software RAID array composed of same-sized Slices from different disks
- **Volume_Group**: An LVM volume group that aggregates all RAID_Arrays in a single Pool as physical volumes
- **Logical_Volume**: An LVM logical volume created on top of the Volume_Group, presented as the usable storage
- **Parity_Mode**: The redundancy level — parity1 (single parity, RAID 5 behavior) or parity2 (double parity, RAID 6 behavior)
- **Disk_Descriptor**: A block device path (e.g., /dev/sdb) identifying a physical disk managed by PoolForge
- **Metadata_Store**: A persistent record of Pool configuration, disk membership, Capacity_Tiers, and RAID_Array mappings, stored as JSON with atomic writes
- **Rebuild**: The process of reconstructing redundancy in a degraded RAID_Array after a disk failure — a full data resync onto a replacement disk
- **Re_Add**: The process of restoring a temporarily absent drive to a RAID_Array using `mdadm --re-add`, which performs a fast bitmap-based recovery instead of a full Rebuild. Re_Add is only possible when the drive's data is still intact and the RAID superblock UUID matches the array
- **External_Enclosure**: A physical drive enclosure (eSATA, USB, or other DAS) that is powered independently from the host system and can be powered off and on without shutting down the host
- **Internal_Pool**: A Pool whose member disks are on permanently attached internal storage (e.g., SATA drives inside the host chassis) and should auto-start at boot
- **External_Pool**: A Pool whose member disks reside in an External_Enclosure and should not auto-start at boot because the enclosure may not be powered on
- **Auto_Start**: The behavior where PoolForge assembles a Pool's RAID_Arrays, activates LVM, and mounts the filesystem automatically at system boot without administrator intervention
- **Manual_Start**: The behavior where a Pool's RAID_Arrays are not assembled at boot; the administrator must explicitly run `poolforge pool start` after ensuring the External_Enclosure is powered on and all drives are detected
- **Pool_Operational_Status**: The runtime state of a Pool indicating whether its arrays are assembled and filesystem is mounted. Values: Running (arrays assembled, LVM active, filesystem mounted, monitoring active), Offline (arrays not assembled), Safe_To_Power_Down (arrays stopped cleanly after a successful pool stop command)
- **Array_UUID**: The universally unique identifier embedded in the mdadm superblock of each RAID_Array member partition, used to match partitions to arrays regardless of device name changes
- **Superblock_Assembly**: The process of assembling a RAID_Array by scanning drive superblocks for matching Array_UUIDs rather than relying on device name paths, enabling correct assembly after device name changes
- **Tier_Order**: The sequence in which RAID_Arrays within a Pool are started or stopped, based on their Capacity_Tier index. Start order is ascending (md0 → md1 → md2). Stop order is descending (md2 → md1 → md0)
- **Boot_Config**: The mdadm configuration file at /etc/mdadm/mdadm.conf that controls which RAID arrays are auto-assembled at boot. PoolForge generates this file
- **AUTO_Directive**: The `AUTO -all` line in mdadm.conf that disables automatic assembly of RAID arrays not explicitly listed with ARRAY definitions
- **ARRAY_Definition**: A line in mdadm.conf of the form `ARRAY /dev/mdN metadata=1.2 UUID=...` that tells mdadm to auto-assemble a specific array at boot
- **Initramfs**: The initial RAM filesystem loaded during Linux boot, which contains a copy of mdadm.conf. Must be updated via `update-initramfs -u` after mdadm.conf changes for boot-time behavior to reflect the new configuration
- **Web_Portal**: A browser-based management interface served by PoolForge for configuring and monitoring Pools, built with React
- **API_Server**: The HTTP backend process (Go) that serves the Web_Portal and exposes a REST API for Pool management operations
- **Storage_Map**: A visual topology diagram in the Web_Portal that renders Pools as containers, RAID_Arrays as blocks, and disks as icons
- **Health_Color**: A color code applied to visual elements — green for healthy/running, amber for degraded/rebuilding, red for failed, grey for offline
- **Detail_Panel**: A contextual information panel in the Web_Portal that opens when the administrator clicks a Pool, RAID_Array, or disk element
- **HealthMonitor**: A background service that listens for mdadm events, Hot_Plug_Events, and SMART_Events, triggering Self_Healing and alerting workflows
- **EventLogger**: The structured logging component that records Log_Entries with component tagging
- **Test_Environment**: An automated, cloud-provisioned infrastructure on AWS consisting of EC2 instances with attached EBS volumes
- **EBS_Volume**: An Amazon Elastic Block Store volume attached to an EC2 instance, used in the Test_Environment to simulate a physical disk

## Requirements

### Requirement 1: mdadm.conf Generation Fix

**User Story:** As a system administrator, I want PoolForge to generate mdadm.conf correctly so that external pools are not auto-assembled at boot, preventing degraded arrays caused by missing drives.

#### Acceptance Criteria

1. WHEN generating the Boot_Config file at /etc/mdadm/mdadm.conf, THE PoolForge SHALL include the AUTO_Directive (`AUTO -all`) before any ARRAY_Definitions to disable default auto-assembly of unlisted arrays.
2. WHEN generating ARRAY_Definitions in the Boot_Config, THE PoolForge SHALL include ARRAY_Definitions only for Pools where the `requires_manual_start` metadata field is false (Auto_Start pools).
3. WHEN generating ARRAY_Definitions in the Boot_Config, THE PoolForge SHALL omit ARRAY_Definitions for all Pools where the `requires_manual_start` metadata field is true (Manual_Start pools).
4. WHEN the Boot_Config is written, THE PoolForge SHALL execute `update-initramfs -u` to propagate the configuration change to the Initramfs so that boot-time behavior matches the updated mdadm.conf.
5. WHEN a Pool's `requires_manual_start` flag is changed via the `pool set-autostart` command, THE PoolForge SHALL regenerate the Boot_Config and update the Initramfs.
6. WHEN a new Pool is created, THE PoolForge SHALL regenerate the Boot_Config to include or exclude the new Pool's ARRAY_Definitions based on the Pool's `requires_manual_start` flag.
7. WHEN a Pool is deleted, THE PoolForge SHALL regenerate the Boot_Config to remove the deleted Pool's ARRAY_Definitions.
8. IF the Boot_Config file does not exist, THEN THE PoolForge SHALL create the file with the AUTO_Directive and the appropriate ARRAY_Definitions.

### Requirement 2: Pool Start Command

**User Story:** As a system administrator, I want to start an external pool after powering on the enclosure, so that PoolForge assembles the arrays correctly using superblock detection, repairs any degraded arrays automatically, and brings the pool online in the correct sequence.

#### Acceptance Criteria

1. THE PoolForge CLI SHALL provide a `pool start <pool-name>` command that brings an Offline pool to Running status.
2. WHEN the administrator executes `pool start`, THE PoolForge SHALL verify that the expected number of member drives are detected by scanning block devices and matching drive capacities against the Pool metadata before proceeding with assembly.
3. IF fewer drives are detected than expected, THEN THE PoolForge SHALL display a warning identifying the expected and detected drive counts and prompt the administrator for confirmation before proceeding.
4. WHEN assembling RAID_Arrays during pool start, THE PoolForge SHALL use Superblock_Assembly (`mdadm --assemble --scan` or per-array UUID-based assembly) to match partitions to arrays by Array_UUID, handling device name changes transparently.
5. WHEN assembling RAID_Arrays during pool start, THE PoolForge SHALL start arrays in ascending Tier_Order (lowest tier index first: md0, then md1, then md2) to ensure lower tiers are ready before higher tiers.
6. WHEN a RAID_Array is assembled in a degraded state during pool start, THE PoolForge SHALL automatically attempt to repair the array by scanning all detected large-capacity drives for partitions whose superblock Array_UUID matches the degraded array's UUID.
7. WHEN a matching partition is found for a degraded array and the partition is not already a member of the array, THE PoolForge SHALL use Re_Add (`mdadm --re-add`) to restore the partition to the array, enabling fast bitmap-based recovery instead of a full Rebuild.
8. WHEN all RAID_Arrays are assembled and healthy (or repair is initiated), THE PoolForge SHALL activate the Volume_Group using `vgchange -ay`.
9. WHEN the Volume_Group is activated, THE PoolForge SHALL mount the Logical_Volume's ext4 filesystem at the configured mount point.
10. WHEN the filesystem is mounted, THE PoolForge SHALL start the HealthMonitor for the Pool to resume SMART monitoring, mdadm event listening, and Hot_Plug_Event detection.
11. WHEN pool start completes successfully, THE PoolForge SHALL update the Pool metadata with the `last_startup` timestamp and set the Pool_Operational_Status to Running.
12. WHEN pool start completes successfully, THE PoolForge SHALL display a summary showing the status of each RAID_Array (healthy or recovering) and the mount point.
13. IF the Pool is already in Running status, THEN THE PoolForge SHALL reject the start command with an error stating the pool is already running.
14. IF any RAID_Array fails to assemble (no superblock matches found), THEN THE PoolForge SHALL log an error identifying the failed array and abort the start operation, leaving the Pool in Offline status.

### Requirement 3: Pool Stop Command

**User Story:** As a system administrator, I want to safely stop an external pool before powering down the enclosure, so that all data is synced, arrays are cleanly stopped, and I receive confirmation that it is safe to power down.

#### Acceptance Criteria

1. THE PoolForge CLI SHALL provide a `pool stop <pool-name>` command that brings a Running pool to Safe_To_Power_Down status.
2. WHEN the administrator executes `pool stop`, THE PoolForge SHALL stop the HealthMonitor for the Pool, ceasing SMART monitoring, mdadm event listening, and Hot_Plug_Event detection for that Pool.
3. WHEN the HealthMonitor is stopped, THE PoolForge SHALL sync all pending writes to stable storage using the `sync` system call.
4. WHEN data is synced, THE PoolForge SHALL unmount the Logical_Volume's ext4 filesystem from the configured mount point.
5. WHEN the filesystem is unmounted, THE PoolForge SHALL deactivate the Logical_Volume and Volume_Group using `lvchange -an` and `vgchange -an`.
6. WHEN LVM is deactivated, THE PoolForge SHALL stop RAID_Arrays in descending Tier_Order (highest tier index first: md2, then md1, then md0) using `mdadm --stop`, issuing a `sync` call before stopping each array.
7. WHEN each RAID_Array is stopped, THE PoolForge SHALL wait briefly (configurable, default 1 second) before stopping the next array to allow clean shutdown.
8. WHEN all RAID_Arrays are stopped, THE PoolForge SHALL verify that no Pool arrays remain active by checking /proc/mdstat.
9. IF any RAID_Array fails to stop, THEN THE PoolForge SHALL log an error identifying the array and attempt to force-stop remaining arrays before reporting the failure.
10. WHEN pool stop completes successfully, THE PoolForge SHALL update the Pool metadata with the `last_shutdown` timestamp and set the Pool_Operational_Status to Safe_To_Power_Down.
11. WHEN pool stop completes successfully, THE PoolForge SHALL display a confirmation message: "It is now SAFE to power down the external enclosure."
12. IF the Pool is already in Offline or Safe_To_Power_Down status, THEN THE PoolForge SHALL reject the stop command with an error stating the pool is not running.
13. WHEN the Boot_Config is verified during pool stop, THE PoolForge SHALL confirm that the AUTO_Directive is present in mdadm.conf and log a warning if it is missing.

### Requirement 4: Pool Auto-Start Configuration

**User Story:** As a system administrator, I want to control which pools auto-start at boot and which require manual start, so that internal pools continue working automatically while external enclosure pools wait for me to power on the enclosure first.

#### Acceptance Criteria

1. THE PoolForge CLI SHALL provide a `pool set-autostart <pool-name> <true|false>` command that sets the `requires_manual_start` metadata flag for the specified Pool.
2. WHEN the administrator sets auto-start to false, THE PoolForge SHALL set `requires_manual_start` to true in the Pool metadata, regenerate the Boot_Config to exclude the Pool's ARRAY_Definitions, and update the Initramfs.
3. WHEN the administrator sets auto-start to true, THE PoolForge SHALL set `requires_manual_start` to false in the Pool metadata, regenerate the Boot_Config to include the Pool's ARRAY_Definitions, and update the Initramfs.
4. WHEN auto-start is changed, THE PoolForge SHALL display a confirmation message showing the pool name and the new auto-start setting.
5. THE PoolForge SHALL default `requires_manual_start` to false for newly created Pools, preserving the existing Auto_Start behavior for Internal_Pools.
6. WHEN the administrator creates a Pool with the `--external` flag, THE PoolForge SHALL set `requires_manual_start` to true and `is_external` to true in the Pool metadata.
7. IF the specified pool name does not exist, THEN THE PoolForge SHALL reject the command with an error identifying the unknown pool.

### Requirement 5: Pool Metadata Extensions

**User Story:** As a system administrator, I want PoolForge to track whether a pool is on an external enclosure and when it was last started and stopped, so that the system can make informed decisions about auto-start behavior and display meaningful status information.

#### Acceptance Criteria

1. THE Metadata_Store SHALL store an `is_external` boolean field for each Pool, indicating whether the Pool resides on an External_Enclosure.
2. THE Metadata_Store SHALL store a `requires_manual_start` boolean field for each Pool, indicating whether the Pool should be excluded from Auto_Start at boot.
3. THE Metadata_Store SHALL store a `last_shutdown` timestamp field for each Pool, recording the time of the most recent successful pool stop operation.
4. THE Metadata_Store SHALL store a `last_startup` timestamp field for each Pool, recording the time of the most recent successful pool start operation.
5. THE PoolForge SHALL default `is_external` to false and `requires_manual_start` to false for all existing Pools during schema migration, preserving backward compatibility with Pools created in prior phases.
6. WHEN the administrator requests Pool status for a Pool with `is_external` set to true, THE PoolForge SHALL include the external enclosure designation, the Pool_Operational_Status, the `last_startup` timestamp, and the `last_shutdown` timestamp in the status output.
7. THE Metadata_Store schema additions SHALL be backward-compatible with the existing version 1 schema, requiring no breaking changes to existing Pool metadata files.

### Requirement 6: Degraded Array Auto-Repair via UUID Matching

**User Story:** As a system administrator, I want PoolForge to automatically repair degraded arrays after a power cycle by finding the correct drives using UUID matching and re-adding them with fast recovery, so that I avoid unnecessary full rebuilds when drives are simply reconnected with different device names.

#### Acceptance Criteria

1. WHEN a RAID_Array is in a degraded state during pool start, THE PoolForge SHALL retrieve the Array_UUID from the array's mdadm detail output.
2. WHEN scanning for missing members, THE PoolForge SHALL examine the mdadm superblock of each partition on all detected large-capacity drives (matching the Pool's expected drive size range) to find partitions whose Array_UUID matches the degraded array.
3. WHEN a partition with a matching Array_UUID is found and the partition is not already an active member of the array, THE PoolForge SHALL execute `mdadm --re-add <array-device> <partition-device>` to restore the partition with fast bitmap-based recovery.
4. WHEN Re_Add succeeds, THE PoolForge SHALL log an informational Log_Entry identifying the array, the re-added partition, and the previous and current device names.
5. IF Re_Add fails for a partition (e.g., bitmap recovery not possible due to excessive changes), THEN THE PoolForge SHALL fall back to `mdadm --add <array-device> <partition-device>` to initiate a full Rebuild, and log a warning-level Log_Entry explaining the fallback.
6. WHEN all degraded arrays have been repaired or repair has been initiated, THE PoolForge SHALL report the final state of each array (healthy, recovering, or still degraded) in the pool start summary.
7. THE PoolForge SHALL handle the case where a drive's device name has changed (e.g., /dev/sdf became /dev/sdj) transparently, relying solely on Array_UUID matching rather than stored device name paths.
8. WHEN a drive is successfully re-added under a new device name, THE PoolForge SHALL update the Disk_Descriptor in the Pool metadata to reflect the current device name.
9. WHEN pool start completes successfully, THE PoolForge SHALL reconcile ALL member device names in the Pool metadata by querying `mdadm --detail` for each assembled RAID_Array and updating every Disk_Descriptor and partition device path to match the current device assignments, regardless of whether the drive was re-added or assembled normally. This ensures the metadata, API responses, Web_Portal Storage_Map, and Detail_Panel all display the current device names (e.g., /dev/sdj1 instead of the stale /dev/sdb1) after any device name changes.

### Requirement 7: Installation Script Updates

**User Story:** As a system administrator, I want the PoolForge installer to disable mdadm systemd services so that PoolForge has full control over array assembly and external enclosures are not disrupted by system-level auto-assembly.

#### Acceptance Criteria

1. WHEN PoolForge is installed, THE Installer SHALL disable the mdmonitor.service systemd unit to prevent mdadm from monitoring arrays independently of PoolForge.
2. WHEN PoolForge is installed, THE Installer SHALL disable the mdadm.service systemd unit to prevent mdadm from auto-assembling arrays at boot independently of PoolForge.
3. WHEN PoolForge is installed, THE Installer SHALL disable the mdadm-waitidle.service systemd unit to prevent mdadm from interfering with PoolForge shutdown sequencing.
4. IF any of the mdadm systemd services do not exist on the target system, THEN THE Installer SHALL skip the missing service without error and log an informational message.
5. WHEN the mdadm services are disabled, THE Installer SHALL log a message confirming that mdadm auto-assembly is disabled and PoolForge will manage all array assembly.
6. THE Installer SHALL ensure that the Boot_Config contains the AUTO_Directive and update the Initramfs after installation.

### Requirement 8: Web Portal Extensions for External Pools

**User Story:** As a system administrator, I want the web portal to show start/stop controls for external pools and display their operational status, so that I can manage external enclosures from the browser without using the CLI.

#### Acceptance Criteria

1. WHEN a Pool has `is_external` set to true, THE Web_Portal SHALL display a warning banner on the Pool's Detail_Panel indicating that the Pool resides on an external enclosure and requires manual start/stop.
2. WHEN a Pool has Pool_Operational_Status of Offline or Safe_To_Power_Down, THE Web_Portal SHALL display a "Start Pool" button on the Pool's Detail_Panel and Dashboard card.
3. WHEN a Pool has Pool_Operational_Status of Running, THE Web_Portal SHALL display a "Stop Pool" button on the Pool's Detail_Panel and Dashboard card.
4. WHEN the administrator clicks the "Start Pool" button, THE Web_Portal SHALL submit a request to the API_Server POST /api/pools/:name/start endpoint and display the operation result.
5. WHEN the administrator clicks the "Stop Pool" button, THE Web_Portal SHALL submit a request to the API_Server POST /api/pools/:name/stop endpoint and display the operation result.
6. THE Web_Portal SHALL display the Pool_Operational_Status on each Pool's Dashboard card and Detail_Panel using the following indicators: green "Running" label for Running pools, grey "Offline" label for Offline pools, and blue "Safe to Power Down" label for Safe_To_Power_Down pools.
7. WHEN a Pool has `is_external` set to true, THE Web_Portal SHALL display the `last_startup` and `last_shutdown` timestamps in the Pool's Detail_Panel.
8. THE Web_Portal SHALL display an auto-start toggle control in the Pool's Detail_Panel that allows the administrator to change the `requires_manual_start` setting via the API_Server PUT /api/pools/:name/autostart endpoint.
9. WHEN a pool stop operation completes successfully via the Web_Portal, THE Web_Portal SHALL display a prominent "Safe to Power Down" confirmation message.
10. THE Web_Portal SHALL apply grey Health_Color to Offline pools in the Storage_Map and Dashboard, distinguishing them from healthy (green), degraded (amber), and failed (red) pools.

### Requirement 9: API Server Extensions

**User Story:** As a system administrator, I want REST API endpoints for pool start, stop, and auto-start configuration, so that the web portal and automation tools can manage external enclosure pools programmatically.

#### Acceptance Criteria

1. THE API_Server SHALL expose a `POST /api/pools/:name/start` endpoint that triggers the pool start sequence for the specified Pool and returns the operation result including the status of each RAID_Array.
2. THE API_Server SHALL expose a `POST /api/pools/:name/stop` endpoint that triggers the pool stop sequence for the specified Pool and returns the operation result.
3. THE API_Server SHALL expose a `PUT /api/pools/:name/autostart` endpoint that accepts a JSON body with an `auto_start` boolean field and updates the Pool's `requires_manual_start` flag accordingly.
4. THE API_Server SHALL require a valid Session_Token for all pool start, stop, and autostart endpoints.
5. WHEN the start endpoint receives a request for a Pool that is already Running, THE API_Server SHALL return an HTTP 409 Conflict status with a response body stating the pool is already running.
6. WHEN the stop endpoint receives a request for a Pool that is not Running, THE API_Server SHALL return an HTTP 409 Conflict status with a response body stating the pool is not running.
7. WHEN the start or stop endpoint receives a request for a Pool that does not exist, THE API_Server SHALL return an HTTP 404 status with a response body identifying the unknown pool.
8. THE API_Server SHALL return JSON response bodies for all new endpoints, consistent with the response model conventions established in Phase 3.
9. WHEN a pool start operation detects fewer drives than expected, THE API_Server SHALL return an HTTP 200 status with a warning field in the response body, allowing the client to decide whether to proceed or abort.

### Requirement 10: Boot Behavior Preservation

**User Story:** As a system administrator, I want internal pools to continue auto-starting at boot exactly as before, so that Phase 5 changes do not disrupt my existing setup.

#### Acceptance Criteria

1. WHEN the system boots and a Pool has `requires_manual_start` set to false, THE PoolForge SHALL reassemble the Pool's RAID_Arrays, activate the Volume_Group, mount the Logical_Volume, and start the HealthMonitor automatically, preserving the existing Phase 1 boot behavior.
2. WHEN the system boots and a Pool has `requires_manual_start` set to true, THE PoolForge SHALL skip assembly of the Pool's RAID_Arrays and set the Pool_Operational_Status to Offline.
3. THE PoolForge SHALL process Auto_Start pools and Manual_Start pools independently during boot, ensuring that a failure to start one pool does not prevent other pools from starting.
4. WHEN the PoolForge service starts at boot, THE PoolForge SHALL log the auto-start decision for each Pool, identifying whether the Pool was started automatically or skipped due to Manual_Start configuration.
5. THE PoolForge SHALL treat all existing Pools created before Phase 5 as Auto_Start pools (requires_manual_start defaults to false), ensuring zero behavioral change for pre-existing installations.

### Requirement 11: Testing (Phase 5 Scope)

**User Story:** As a developer, I want a comprehensive test suite for Phase 5 external enclosure support, so that I can verify pool start/stop sequencing, device name change handling, degraded array repair, auto-start configuration, and confirm no regressions in Phase 1 through Phase 4.

#### Acceptance Criteria

1. WHEN Phase 5 is completed, THE test suite SHALL include pool start/stop sequencing tests that verify RAID_Arrays are started in ascending Tier_Order and stopped in descending Tier_Order, with LVM and filesystem operations occurring in the correct sequence.
2. WHEN Phase 5 is completed, THE test suite SHALL include device name change tests that simulate drive device name changes (by detaching and reattaching EBS_Volumes in the Test_Environment) and verify that Superblock_Assembly correctly assembles arrays regardless of the new device names.
3. WHEN Phase 5 is completed, THE test suite SHALL include degraded array repair tests that simulate a degraded array (by temporarily detaching an EBS_Volume), execute pool start, and verify that the Re_Add operation is used for fast recovery instead of a full Rebuild.
4. WHEN Phase 5 is completed, THE test suite SHALL include auto-start configuration tests that verify setting auto-start to false excludes the Pool's ARRAY_Definitions from the Boot_Config, and setting auto-start to true includes them.
5. WHEN Phase 5 is completed, THE test suite SHALL include mdadm.conf generation tests that verify the Boot_Config contains the AUTO_Directive, includes ARRAY_Definitions only for Auto_Start pools, and omits ARRAY_Definitions for Manual_Start pools.
6. WHEN Phase 5 is completed, THE test suite SHALL include full power cycle simulation tests that execute the sequence: create pool, write data, stop pool, simulate enclosure power-off (detach EBS_Volumes), simulate enclosure power-on (reattach EBS_Volumes with potentially different device names), start pool, and verify data integrity.
7. WHEN Phase 5 is completed, THE test suite SHALL include API endpoint tests that validate POST /api/pools/:name/start, POST /api/pools/:name/stop, and PUT /api/pools/:name/autostart for correct responses, authentication enforcement, conflict detection, and error handling.
8. WHEN Phase 5 is completed, THE test suite SHALL include Web_Portal component tests that validate start/stop button rendering, Pool_Operational_Status indicators, external enclosure warning banners, auto-start toggle behavior, and Safe_To_Power_Down confirmation display.
9. WHEN Phase 5 is completed, THE test suite SHALL include regression tests that confirm all Phase 1 functionality (pool creation, status, list, metadata persistence, tier computation), all Phase 2 functionality (add-disk, replace-disk, remove-disk, delete-pool, self-healing rebuild, expansion, export/import), all Phase 3 functionality (API endpoints, Web_Portal, Storage_Map, Log_Viewer, authentication), and all Phase 4 functionality (atomic operations, rollback, multi-interface support, SMART monitoring) remain correct.
10. THE test suite SHALL include property-based tests for Boot_Config generation idempotence (generating the Boot_Config twice with the same pool configuration produces identical output) and Pool metadata round-trip (saving and loading pool metadata with the new Phase 5 fields produces equivalent data).
11. THE full power cycle simulation tests SHALL execute against the cloud-based Test_Environment using EC2 instances with attached EBS_Volumes, simulating enclosure power cycles by detaching and reattaching volumes.

### Requirement 12: Test Infrastructure (Phase 5 Scenarios)

**User Story:** As a developer, I want the cloud-based test environment to support Phase 5 test scenarios, so that I can test pool start/stop, device name changes, and degraded array repair against real block devices.

#### Acceptance Criteria

1. THE Test_Environment SHALL support enclosure power cycle simulation by detaching all EBS_Volumes belonging to a Pool from the running EC2 instance and reattaching them, potentially with different device name assignments.
2. THE Test_Environment SHALL support degraded array simulation by detaching a subset of EBS_Volumes belonging to a Pool, starting the pool in degraded mode, then reattaching the missing volumes and verifying Re_Add recovery.
3. THE Test_Environment SHALL support device name change simulation by detaching an EBS_Volume and reattaching it to a different device slot on the EC2 instance, verifying that Superblock_Assembly handles the name change.
4. THE Test_Environment SHALL execute a full Phase 5 lifecycle test scenario: create a Pool with Manual_Start configuration, write data, stop the pool, detach EBS_Volumes, reattach EBS_Volumes with different device assignments, start the pool, verify data integrity, verify no full Rebuild was triggered, and verify device name updates in metadata.
5. THE Test_Runner SHALL collect pool start/stop logs, Re_Add operation logs, and Boot_Config contents from the EC2 instance for each Phase 5 test scenario.
6. THE IaC_Template SHALL provision sufficient EBS_Volumes to support all Phase 5 test scenarios, including volumes for simulating partial detach (degraded state) and full detach (power cycle).

### Requirement 13: Safe Software Update

**User Story:** As a system administrator, I want to update PoolForge from a prior version (Phase 1–4 build) to the Phase 5 build without losing any pool data, RAID arrays, metadata, or configuration, so that the upgrade is seamless and non-destructive.

#### Acceptance Criteria

1. WHEN the PoolForge binary is replaced with the Phase 5 build, THE PoolForge SHALL read existing metadata files created by Phase 1–4 builds without error, applying backward-compatible defaults for any missing Phase 5 fields (`is_external` defaults to false, `requires_manual_start` defaults to false, `operational_status` defaults to "running", `last_shutdown` defaults to null, `last_startup` defaults to null, `uuid` on RAID arrays defaults to empty string).
2. WHEN the Phase 5 build starts for the first time after an upgrade, THE PoolForge SHALL NOT modify any existing RAID arrays, LVM volume groups, logical volumes, or mounted filesystems. All pools that were running before the upgrade SHALL continue running without interruption.
3. WHEN the Phase 5 build starts for the first time after an upgrade, THE PoolForge SHALL populate the `uuid` field for each RAID array by querying `mdadm --detail` for each assembled array and storing the Array_UUID in the pool metadata.
4. WHEN the Phase 5 build starts for the first time after an upgrade, THE PoolForge SHALL regenerate the Boot_Config to include the AUTO_Directive (`AUTO -all`) and ARRAY_Definitions for all existing pools (since all pre-Phase 5 pools default to `requires_manual_start == false`), and update the Initramfs.
5. THE upgrade process SHALL NOT require the administrator to stop any pools, unmount any filesystems, or stop any RAID arrays. The upgrade SHALL be performed by replacing the PoolForge binary and restarting the PoolForge systemd service.
6. WHEN the Phase 5 build writes metadata for the first time after an upgrade, THE PoolForge SHALL preserve all existing metadata fields and values from the prior version, adding only the new Phase 5 fields with their default values.
7. THE PoolForge SHALL detect whether it is running for the first time after an upgrade by checking whether the metadata contains Phase 5 fields (e.g., `operational_status` is absent). If Phase 5 fields are absent, THE PoolForge SHALL perform the one-time migration (populate UUIDs, regenerate Boot_Config) and log an informational message: "PoolForge upgraded to Phase 5. Metadata migrated. Boot config updated."
8. WHEN the Phase 5 build is installed via the Installer (`install.sh`), THE Installer SHALL back up the existing metadata file to `/var/lib/poolforge/metadata.json.pre-phase5-backup` before the Phase 5 service starts, providing a rollback point if needed.
9. IF the metadata backup already exists (indicating a prior upgrade attempt), THEN THE Installer SHALL NOT overwrite the existing backup.
10. THE upgrade process SHALL be tested in the Test_Environment by installing a Phase 4 build, creating pools with data, upgrading to the Phase 5 build, and verifying that all pools remain running, all data is intact, metadata contains Phase 5 defaults, and the Boot_Config is correctly regenerated.

## Extensibility Notes

Phase 5 extends the PoolForge architecture with external enclosure awareness. The following interfaces are extended or introduced:

- **EngineService**: Adds `StartPool`, `StopPool`, `SetAutoStart`. These methods follow the same Atomic_Operation semantics established in Phase 4 — StartPool and StopPool create Checkpoints and support Rollback on failure. Future phases could extend start/stop with pre/post hooks for container orchestration (e.g., stopping Docker containers before pool stop).
- **RAIDManager**: Adds `AssembleArrayBySuperblock` (UUID-based assembly), `ReAddMember` (fast re-add), `StopArray` (clean array stop). These complement the existing `CreateArray`, `ReshapeArray`, `AddMember`, `RemoveMember` methods.
- **BootConfig**: Modified to be pool-aware, generating ARRAY_Definitions conditionally based on per-pool auto-start settings. The Boot_Config regeneration is triggered by pool create, pool delete, and set-autostart operations.
- **MetadataStore**: Adds `is_external`, `requires_manual_start`, `last_shutdown`, `last_startup` fields. Schema remains version 1 with backward-compatible additions. All existing fields and behavior are unchanged.
- **API_Server**: Adds three new endpoints. All existing Phase 3 and Phase 4 endpoints are unchanged.
- **Web_Portal**: Adds start/stop controls, Pool_Operational_Status indicators, external enclosure warnings, and auto-start toggle. All existing Phase 3 and Phase 4 pages and components are unchanged.
- **Installer**: Adds mdadm systemd service disablement. The installer changes are additive and do not affect existing installation steps.

The architecture supports future extension for container orchestration hooks (stop/start Docker containers around pool stop/start), scheduled power-on/off automation, and multi-enclosure grouping, though these are not in scope for Phase 5.
