# Requirements Document — Phase 4: Safety Hardening

## Introduction

This document specifies the requirements for Phase 4 of the PoolForge project — the final phase. PoolForge is an open-source storage management tool for Ubuntu LTS (24.04+) that replicates Synology Hybrid RAID (SHR) functionality using mdadm and LVM. The full project scope is defined in the master spec at `.kiro/specs/hybrid-raid-manager/`. Phase 1 is defined at `.kiro/specs/poolforge-phase1-core-engine/`. Phase 2 is defined at `.kiro/specs/poolforge-phase2-lifecycle/`. Phase 3 is defined at `.kiro/specs/poolforge-phase3-web-portal/`.

Phase 1 established the core engine: capacity-tier computation from mixed-size disks, GPT disk partitioning, mdadm RAID array creation, LVM stitching (PV → VG → LV), ext4 filesystem creation, a CLI for pool creation/status/list, a JSON-based metadata store with atomic writes, and the automated cloud-based test infrastructure (Terraform IaC for EC2 + EBS, Test_Runner script). Phase 1 defined and implemented the core interfaces: EngineService (CreatePool, GetPool, ListPools, GetPoolStatus), StorageAbstraction (DiskManager, RAIDManager, LVMManager, FilesystemManager), and MetadataStore (SavePool, LoadPool, ListPools).

Phase 2 built on those interfaces to deliver lifecycle operations: adding disks to existing pools (with array reshape and filesystem expansion), replacing failed disks, removing disks, deleting pools, disk failure detection with automatic self-healing rebuilds, unallocated capacity detection with administrator-approved expansion, pool configuration serialization (JSON export/import), and the HealthMonitor background service. Phase 2 extended EngineService with AddDisk, ReplaceDisk, RemoveDisk, DeletePool, HandleDiskFailure, GetRebuildProgress, ExportPool, ImportPool, DetectUnallocated, and ExpandPool. Phase 2 also introduced Pre_Operation_Checks and basic safety validation (PoolForge_Signature, disk state verification, consistency checks before reshape/rebuild).

Phase 3 layered the web interface on top of the existing EngineService without modifying it. The API_Server is a thin HTTP wrapper around the same EngineService interface that the CLI uses. Phase 3 delivered the React Web_Portal with Dashboard, Storage_Map visualization with Health_Color coding, Detail_Panels, rebuild progress bars, Log_Viewer with filtering and Live_Tail via WebSocket, local username/password authentication with session tokens, pool management forms, and the EventLogger structured logging system.

Phase 4 is the final phase. It hardens PoolForge for production safety by wrapping all existing storage operations with atomic/rollback semantics (ensuring zero data loss), adding multi-interface disk support (SATA, eSATA, USB 3.0+, other DAS), integrating SMART disk health monitoring into the existing HealthMonitor pipeline, and extending the Web_Portal with SMART indicators and configuration. Phase 4 does not introduce new pool management operations — it wraps the existing Phase 2 operations (CreatePool, AddDisk, ReplaceDisk, RemoveDisk, DeletePool, ExpandPool) with checkpoint-and-rollback semantics, adds write barriers and sync guarantees during critical operations, and performs post-operation consistency verification. Phase 4 also adds the smartmontools dependency (deferred from Phase 1) and the mock SMART data provider for EBS-based testing (deferred from Phase 1 test infrastructure).

This is Phase 4 of 4:

| Phase | Scope |
|-------|-------|
| Phase 1 ✅ | Core engine, CLI (create/status/list), metadata store, test infrastructure |
| Phase 2 ✅ | Lifecycle operations (add disk, replace disk, remove disk, delete pool, self-healing/rebuild, expansion, export/import) |
| Phase 3 ✅ | Web portal (React), API server (Go REST), Storage_Map, Log_Viewer, authentication |
| **Phase 4** | **Safety hardening (atomic operations, rollback, multi-interface, SMART monitoring)** |

Phase 4 extends the following interfaces:
- **StorageAbstraction**: Wraps all existing DiskManager, RAIDManager, LVMManager, and FilesystemManager operations with atomic operation semantics (pre-operation checkpoint, execute, rollback on failure, post-operation verification).
- **HealthMonitor**: Integrates SMART monitoring into the existing event pipeline. The HealthMonitor now listens for mdadm events (Phase 2), udev Hot_Plug_Events (Phase 4), and SMART_Events (Phase 4).
- **SMARTMonitor** (`internal/smart`): New component — periodic SMART data collection, threshold evaluation, SMART_Event generation, and mock provider for EBS-based testing.
- **MetadataStore**: Adds `SaveSMARTData`, `LoadSMARTData`, and SMART threshold persistence. Schema remains version 1 with backward-compatible additions.
- **API_Server**: Adds SMART-related endpoints: GET /api/smart/:disk, PUT /api/smart/thresholds, GET /api/smart/:disk/history.
- **CLI**: Adds `smart status <disk>`, `smart history <disk>`, `smart thresholds set` commands.
- **Web_Portal**: Adds SMART health indicators on disk icons, SMART data display in disk Detail_Panels, and SMART threshold configuration page.

Phase 4 MUST NOT break any Phase 1, Phase 2, or Phase 3 functionality. All existing CLI commands, API endpoints, Web_Portal pages, metadata persistence, self-healing, and test infrastructure continue to work unchanged.

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
- **Rebuild**: The process of reconstructing redundancy in a degraded RAID_Array after a disk failure
- **Reshape**: The process of modifying an existing RAID_Array geometry to accommodate a new member disk
- **Pre_Operation_Check**: A set of validation and consistency checks performed by PoolForge before executing any destructive or modifying operation to confirm data safety
- **Atomic_Operation**: An operation that either completes fully or rolls back entirely, leaving the Pool in its prior consistent state. Implemented via checkpoint-execute-rollback semantics
- **Checkpoint**: A snapshot of the current Pool state (metadata, array states, LVM configuration) saved before an Atomic_Operation begins, used as the rollback target if the operation fails
- **Rollback**: The process of reverting a Pool to its Checkpoint state by executing reverse operations (e.g., removing added partitions, restoring array membership, reverting LVM changes) when an Atomic_Operation fails partway through
- **Write_Barrier**: A forced synchronization of in-flight writes to stable storage (via fsync or sync) performed during critical operations to ensure data durability before proceeding to the next step
- **Post_Operation_Verification**: A set of consistency checks performed after an Atomic_Operation completes successfully, including mdadm array state verification, LVM metadata consistency, and optional data checksums
- **Interface_Type**: The physical connection interface through which a disk is attached to the system (e.g., SATA, eSATA, USB 3.0+, or other DAS interfaces)
- **DAS**: Direct-Attached Storage — storage devices connected directly to the host system via a local bus or cable, as opposed to network-attached storage
- **Hot_Plug_Event**: A hardware event in which a disk is physically connected or disconnected while the system is running, supported by interfaces such as eSATA and USB
- **SMART_Data**: Self-Monitoring, Analysis, and Reporting Technology data retrieved from a physical disk, containing health attributes, error counters, and predictive failure indicators
- **SMART_Check**: A periodic inspection of SMART_Data from a managed disk performed by PoolForge to assess disk health
- **SMART_Event**: A logged occurrence when a SMART_Check detects a warning threshold breach or a significant change in disk health attributes
- **SMART_Threshold**: A configurable limit for a SMART attribute (e.g., reallocated sector count, pending sector count, uncorrectable error count) that triggers a SMART_Event when exceeded
- **Mock_SMART_Provider**: A test component that simulates SMART_Data responses for EBS_Volumes in the Test_Environment, since EBS does not support native SMART queries
- **PoolForge_Signature**: A metadata marker written to the Partition_Table of each managed disk, identifying the disk as belonging to a specific Pool
- **Web_Portal**: A browser-based management interface served by PoolForge for configuring and monitoring Pools, built with React
- **API_Server**: The HTTP backend process (Go) that serves the Web_Portal and exposes a REST API for Pool management operations
- **Detail_Panel**: A contextual information panel in the Web_Portal that opens when the administrator clicks a Pool, RAID_Array, or disk element in the Storage_Map
- **Storage_Map**: A visual topology diagram in the Web_Portal that renders Pools as containers, RAID_Arrays as blocks within those containers, and disks as individual icons within each RAID_Array block
- **Health_Color**: A color code applied to visual elements in the Storage_Map — green for healthy, amber for degraded/rebuilding/SMART warning, red for failed/SMART failed
- **HealthMonitor**: A background service that listens for mdadm disk failure events, udev Hot_Plug_Events, and SMART_Events, triggering Self_Healing and alerting workflows
- **EventLogger**: The structured logging component that records Log_Entries with component tagging, supports query/filter operations, and provides a streaming channel for Live_Tail
- **Test_Environment**: An automated, cloud-provisioned infrastructure on AWS consisting of EC2 instances with attached EBS volumes, used to simulate real disk operations
- **EBS_Volume**: An Amazon Elastic Block Store volume attached to an EC2 instance, used in the Test_Environment to simulate a physical disk
- **Test_Runner**: A script that orchestrates the full test lifecycle: provisioning the Test_Environment, executing the test suite, collecting results and logs, and tearing down all resources

## Requirements

### Requirement 1: Zero Data Loss Guarantee

**User Story:** As a system administrator, I want PoolForge to guarantee zero data loss across all operations, so that I can trust the tool with my production storage infrastructure knowing that no operation will leave my data in an inconsistent or corrupted state.

#### Acceptance Criteria

1. THE PoolForge SHALL wrap every Pool-modifying operation (CreatePool, AddDisk, ReplaceDisk, RemoveDisk, DeletePool, ExpandPool) in an Atomic_Operation that either completes fully or rolls back entirely to the prior consistent state.
2. WHEN an Atomic_Operation begins, THE PoolForge SHALL create a Checkpoint by saving the current Pool state (metadata, RAID_Array states, Volume_Group configuration, Logical_Volume configuration) to the Metadata_Store before executing any storage modifications.
3. WHEN an Atomic_Operation fails at any step, THE PoolForge SHALL execute a Rollback by reversing all completed steps in reverse order, restoring the Pool to the Checkpoint state, and logging each rollback step with the operation name, the failed step, and the rollback actions taken.
4. WHEN an Atomic_Operation completes successfully, THE PoolForge SHALL perform Post_Operation_Verification by checking all affected RAID_Arrays via mdadm for consistent state, verifying LVM metadata consistency, and confirming the ext4 filesystem is mountable and consistent.
5. IF Post_Operation_Verification detects an inconsistency after a successful operation, THEN THE PoolForge SHALL log an error-level Log_Entry identifying the inconsistency and mark the Pool state as degraded until the administrator resolves the issue.
6. THE PoolForge SHALL issue Write_Barriers (fsync or sync calls) after each critical step within an Atomic_Operation to ensure data durability before proceeding to the next step.
7. THE PoolForge SHALL validate all Pre_Operation_Checks (disk accessibility, PoolForge_Signature verification, RAID_Array consistency, no active Rebuild in progress) before creating the Checkpoint and beginning the Atomic_Operation.
8. IF a Rollback fails to fully restore the Checkpoint state, THEN THE PoolForge SHALL log a critical-level Log_Entry identifying the partially rolled-back state and the specific step that failed to reverse, and mark the Pool state as failed.
9. THE PoolForge SHALL persist the Checkpoint and the current operation step in the Metadata_Store so that if the PoolForge process crashes mid-operation, the next startup can detect the incomplete operation and complete the Rollback.

### Requirement 2: Multi-Interface Disk Support

**User Story:** As a system administrator, I want PoolForge to support disks connected via SATA, eSATA, USB 3.0+, and other DAS interfaces, so that I can use any directly-attached disk regardless of its connection type.

#### Acceptance Criteria

1. THE PoolForge SHALL detect and enumerate disks connected via SATA, eSATA, USB 3.0+, and other DAS interfaces, treating all detected disks as valid Disk_Descriptors for Pool operations.
2. WHEN enumerating disks, THE PoolForge SHALL identify the Interface_Type for each detected disk and include the Interface_Type in the disk metadata stored in the Metadata_Store.
3. WHEN a disk is connected via a less reliable interface (USB), THE PoolForge SHALL log a warning-level Log_Entry advising the administrator that USB-connected disks have higher failure risk due to connection instability, and display the warning in the Web_Portal when the disk is selected for a Pool operation.
4. THE PoolForge SHALL handle Hot_Plug_Events for eSATA and USB interfaces by detecting newly connected disks and making them available for Pool operations, and detecting disconnected disks and triggering the disk failure workflow for any affected Pools.
5. WHEN a Hot_Plug_Event indicates a disk disconnection for a disk in a Pool, THE PoolForge SHALL treat the disconnection as a disk failure and initiate the same Self_Healing workflow used for mdadm-reported failures.
6. WHEN a Hot_Plug_Event indicates a new disk connection, THE PoolForge SHALL log an informational Log_Entry identifying the new Disk_Descriptor and Interface_Type, and make the disk available for add-disk or replace-disk operations.
7. THE PoolForge SHALL perform all Pool operations (create, add-disk, replace-disk, remove-disk, delete, expand) identically regardless of the Interface_Type of the member disks.
8. WHEN displaying disk information in the CLI status output, API responses, and Web_Portal Detail_Panels, THE PoolForge SHALL include the Interface_Type for each disk.

### Requirement 3: SMART Disk Health Monitoring

**User Story:** As a system administrator, I want PoolForge to monitor SMART data from all managed disks, so that I can predict disk failures before they happen and take preventive action rather than reacting to failures after data is at risk.

#### Acceptance Criteria

1. THE PoolForge SHALL perform periodic SMART_Checks on all disks managed by any Pool, at a configurable interval with a default of once per hour.
2. WHEN a SMART_Check completes, THE PoolForge SHALL store the retrieved SMART_Data in the Metadata_Store associated with the corresponding Disk_Descriptor.
3. WHEN a SMART_Check detects that a disk attribute has crossed a SMART_Threshold (e.g., reallocated sector count, current pending sector count, or uncorrectable error count exceeding configured limits), THE PoolForge SHALL generate a SMART_Event and log a warning-level Log_Entry identifying the disk, the attribute, and the threshold breached.
4. WHEN a SMART_Check detects that a disk reports a SMART overall-health status of "FAILED", THE PoolForge SHALL generate a SMART_Event and log an error-level Log_Entry identifying the disk.
5. THE Web_Portal SHALL display SMART health indicators on each disk icon in the Storage_Map, using amber Health_Color for disks with SMART warnings and red Health_Color for disks with SMART failure status.
6. WHEN the administrator views the Detail_Panel for a disk, THE Web_Portal SHALL display the latest SMART_Data attributes (overall health, temperature, reallocated sectors, pending sectors, uncorrectable errors, power-on hours), the history of SMART_Events for that disk, and the time of the last SMART_Check.
7. IF a SMART_Check cannot retrieve data from a disk (e.g., the disk does not support SMART or the query times out), THEN THE PoolForge SHALL log an info-level Log_Entry and mark the disk SMART status as "unavailable" in the Metadata_Store.
8. THE PoolForge SHALL integrate SMART_Events into the existing HealthMonitor event pipeline so that SMART warnings and failures are processed alongside mdadm failure events and Hot_Plug_Events.
9. THE PoolForge SHALL depend on smartmontools (available in the default Ubuntu LTS repositories) for SMART_Data retrieval.

### Requirement 4: SMART Threshold Configuration

**User Story:** As a system administrator, I want to configure SMART warning thresholds via the CLI and API, so that I can tune alerting sensitivity to match my environment and disk models.

#### Acceptance Criteria

1. THE PoolForge SHALL allow the administrator to configure SMART_Thresholds for the following attributes: reallocated sector count, current pending sector count, and uncorrectable error count.
2. THE PoolForge SHALL persist SMART_Thresholds in the Metadata_Store so that configured thresholds survive service restarts.
3. WHEN the administrator updates SMART_Thresholds, THE PoolForge SHALL apply the new thresholds to all subsequent SMART_Checks without requiring a service restart.
4. THE PoolForge SHALL provide default SMART_Thresholds (reallocated sectors: 100, pending sectors: 50, uncorrectable errors: 10) that are used when the administrator has not configured custom thresholds.
5. THE Web_Portal SHALL provide a SMART threshold configuration page accessible from the Configuration navigation, allowing the administrator to view and modify SMART_Thresholds.
6. FOR ALL valid SMART_Threshold configurations, setting thresholds via the CLI or API and then reading them back SHALL return the same threshold values (round-trip property).

### Requirement 5: SMART CLI Commands

**User Story:** As a system administrator, I want CLI commands to view SMART data, event history, and configure thresholds, so that I can monitor disk health and tune alerting from the command line.

#### Acceptance Criteria

1. THE PoolForge CLI SHALL provide a `smart status <disk>` command that displays the current SMART_Data for the specified Disk_Descriptor, including overall health, temperature, reallocated sectors, pending sectors, uncorrectable errors, power-on hours, and the time of the last SMART_Check.
2. THE PoolForge CLI SHALL provide a `smart history <disk>` command that displays the SMART_Event history for the specified Disk_Descriptor, showing each event timestamp, the attribute that triggered the event, and the threshold that was breached.
3. THE PoolForge CLI SHALL provide a `smart thresholds set` command that accepts flags for each configurable threshold (e.g., `--reallocated-sectors <value>`, `--pending-sectors <value>`, `--uncorrectable-errors <value>`) and updates the SMART_Thresholds in the Metadata_Store.
4. WHEN any SMART CLI command encounters an error (e.g., disk not found, SMART unavailable), THE CLI SHALL display a descriptive error message and exit with a non-zero exit code.
5. WHEN any SMART CLI command completes successfully, THE CLI SHALL display the requested data and exit with a zero exit code.

### Requirement 6: SMART API Endpoints

**User Story:** As a system administrator, I want REST API endpoints for SMART data, so that the web portal and automation tools can access disk health information programmatically.

#### Acceptance Criteria

1. THE API_Server SHALL expose a `GET /api/smart/:disk` endpoint that returns the current SMART_Data for the specified Disk_Descriptor, including overall health, temperature, reallocated sectors, pending sectors, uncorrectable errors, power-on hours, and the time of the last SMART_Check.
2. THE API_Server SHALL expose a `PUT /api/smart/thresholds` endpoint that accepts a JSON body with SMART_Threshold values and updates the configured thresholds in the Metadata_Store.
3. THE API_Server SHALL expose a `GET /api/smart/:disk/history` endpoint that returns the SMART_Event history for the specified Disk_Descriptor.
4. THE API_Server SHALL require a valid Session_Token for all SMART API endpoints.
5. WHEN a SMART API endpoint receives a request for a Disk_Descriptor that is not managed by any Pool, THE API_Server SHALL return an HTTP 404 status with a response body identifying the unknown disk.
6. THE API_Server SHALL return JSON response bodies for all SMART endpoints, consistent with the response model conventions established in Phase 3.

### Requirement 7: Web Portal SMART Integration

**User Story:** As a system administrator, I want SMART health indicators visible in the web portal, so that I can see disk health at a glance and drill into SMART details without using the CLI.

#### Acceptance Criteria

1. THE Web_Portal SHALL display SMART health indicators on each disk icon in the Storage_Map: amber Health_Color for disks with SMART warnings, red Health_Color for disks with SMART failure status, and no additional indicator for disks with healthy SMART status or unavailable SMART data.
2. WHEN the administrator views the Detail_Panel for a disk, THE Web_Portal SHALL display a SMART section containing the latest SMART_Data attributes, the SMART_Event history for that disk, and the time of the last SMART_Check.
3. THE Web_Portal SHALL provide a SMART threshold configuration page accessible from the Configuration navigation bar, allowing the administrator to view current SMART_Thresholds, modify threshold values, and save changes via the PUT /api/smart/thresholds endpoint.
4. WHEN a SMART_Event is generated for a disk, THE Web_Portal SHALL display a Notification_Banner identifying the affected Disk_Descriptor, the SMART attribute, and the threshold breached.

### Requirement 8: Platform Dependency Update

**User Story:** As a system administrator, I want PoolForge to include smartmontools as a dependency, so that SMART monitoring works out of the box on Ubuntu LTS.

#### Acceptance Criteria

1. THE PoolForge SHALL depend on smartmontools (available in the default Ubuntu LTS repositories) for SMART_Data retrieval via the smartctl command.
2. THE PoolForge SHALL verify that smartmontools is installed during startup and log a warning-level Log_Entry if smartmontools is not found, disabling SMART monitoring gracefully without affecting other PoolForge functionality.
3. WHEN smartmontools is not installed, THE PoolForge SHALL mark all disk SMART statuses as "unavailable" and disable periodic SMART_Checks until smartmontools becomes available.

### Requirement 9: Testing (Phase 4 Scope)

**User Story:** As a developer, I want a comprehensive test suite for Phase 4 safety hardening, so that I can verify atomic operations, rollback, multi-interface support, and SMART monitoring, and confirm no regressions in Phase 1, Phase 2, and Phase 3.

#### Acceptance Criteria

1. WHEN Phase 4 is completed, THE test suite SHALL include failure injection tests that simulate operation failures at each step of an Atomic_Operation (partitioning, mdadm creation, LVM operations, filesystem operations) and verify that Rollback restores the Pool to the Checkpoint state.
2. WHEN Phase 4 is completed, THE test suite SHALL include rollback verification tests that confirm the Pool metadata, RAID_Array states, Volume_Group configuration, and Logical_Volume configuration match the Checkpoint state after a failed operation is rolled back.
3. WHEN Phase 4 is completed, THE test suite SHALL include Post_Operation_Verification tests that confirm mdadm array consistency, LVM metadata consistency, and ext4 filesystem consistency after each successful Atomic_Operation.
4. WHEN Phase 4 is completed, THE test suite SHALL include multi-interface detection tests that verify PoolForge correctly identifies and enumerates disks connected via SATA, eSATA, USB, and other DAS interfaces.
5. WHEN Phase 4 is completed, THE test suite SHALL include Hot_Plug_Event handling tests that verify disk connection and disconnection events are detected and processed correctly.
6. WHEN Phase 4 is completed, THE test suite SHALL include SMART monitoring tests that verify periodic SMART_Checks, threshold evaluation, SMART_Event generation, and SMART_Data persistence.
7. WHEN Phase 4 is completed, THE test suite SHALL include SMART API endpoint tests that validate GET /api/smart/:disk, PUT /api/smart/thresholds, and GET /api/smart/:disk/history for correct responses, authentication enforcement, and error handling.
8. WHEN Phase 4 is completed, THE test suite SHALL include regression tests that confirm all Phase 1 functionality (pool creation, status, list, metadata persistence, tier computation), all Phase 2 functionality (add-disk, replace-disk, remove-disk, delete-pool, self-healing rebuild, expansion, export/import), and all Phase 3 functionality (API endpoints, Web_Portal, Storage_Map, Log_Viewer, authentication) remain correct.
9. THE test suite SHALL include property-based tests for SMART_Data persistence round-trip (store → load produces equivalent data), SMART_Threshold configuration round-trip (set → get returns same values), and Checkpoint/Rollback state equivalence (rollback restores exact Checkpoint state).
10. THE failure injection tests and rollback verification tests SHALL execute against the cloud-based Test_Environment using EC2 instances with attached EBS_Volumes.

### Requirement 10: Test Infrastructure (Phase 4 Scenarios)

**User Story:** As a developer, I want the cloud-based test environment to support Phase 4 test scenarios, so that I can test atomic operations, rollback, and SMART monitoring against real block devices.

#### Acceptance Criteria

1. THE Test_Environment SHALL include a Mock_SMART_Provider that simulates SMART_Data responses for EBS_Volumes, since EBS does not support native SMART queries.
2. THE Mock_SMART_Provider SHALL support configurable SMART_Data values (overall health, temperature, reallocated sectors, pending sectors, uncorrectable errors, power-on hours) for each simulated disk.
3. THE Mock_SMART_Provider SHALL support triggering simulated SMART threshold breaches and SMART failure events for testing SMART_Event generation and alerting.
4. THE Test_Environment SHALL support failure injection by interrupting storage operations at configurable steps (e.g., after partitioning but before mdadm creation, after mdadm creation but before LVM operations) to test Rollback behavior.
5. THE Test_Environment SHALL execute a full safety test scenario: create a Pool with Atomic_Operation semantics, inject a failure mid-operation, verify Rollback restores the prior state, retry the operation successfully, verify Post_Operation_Verification passes, run SMART_Checks with the Mock_SMART_Provider, trigger a SMART threshold breach, and verify the SMART_Event is logged and displayed.
6. THE IaC_Template SHALL provision the Mock_SMART_Provider alongside PoolForge on the EC2 instance during Test_Environment setup.
7. THE Test_Runner SHALL collect Rollback logs, Post_Operation_Verification results, and SMART monitoring logs from the EC2 instance for each Phase 4 test scenario.

## Extensibility Notes

Phase 4 is the final planned phase. The following interfaces are extended or introduced in Phase 4:

- **StorageAbstraction**: All existing DiskManager, RAIDManager, LVMManager, and FilesystemManager operations are wrapped with Atomic_Operation semantics (Checkpoint → Execute → Rollback on failure → Post_Operation_Verification on success). The underlying operation interfaces from Phase 1 and Phase 2 are unchanged.
- **HealthMonitor**: Extended to process SMART_Events alongside mdadm failure events (Phase 2) and Hot_Plug_Events (Phase 4). The HealthMonitor now serves as the unified event pipeline for all disk health and connectivity events.
- **SMARTMonitor** (`internal/smart`): New in Phase 4. Periodic SMART_Data collection via smartmontools, threshold evaluation, SMART_Event generation, and Mock_SMART_Provider for testing.
- **MetadataStore**: Adds `SaveSMARTData`, `LoadSMARTData`, SMART threshold persistence, and Checkpoint/Rollback state storage. Schema remains version 1 with backward-compatible additions.
- **API_Server**: Adds GET /api/smart/:disk, PUT /api/smart/thresholds, GET /api/smart/:disk/history endpoints. All existing Phase 3 endpoints are unchanged.
- **CLI**: Adds `smart status`, `smart history`, `smart thresholds set` commands. All existing Phase 1, 2, and 3 CLI commands are unchanged.
- **Web_Portal**: Adds SMART health indicators on disk icons in the Storage_Map, SMART data section in disk Detail_Panels, SMART threshold configuration page, and SMART Notification_Banners. All existing Phase 3 Web_Portal pages and components are unchanged.

The architecture supports future extension for notification channels (email, webhook, Slack) via the EventLogger and HealthMonitor event pipeline, though notifications are not in scope for any current phase.
