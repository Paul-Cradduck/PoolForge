# Requirements Document

## Introduction

PoolForge is an open-source storage management tool for Ubuntu LTS (24.04+) that replicates Synology Hybrid RAID (SHR) functionality using mdadm and LVM. PoolForge automates the partitioning of mixed-size disks into capacity-tier slices, creates multiple mdadm RAID arrays from those slices, and stitches them together via LVM into a single unified logical volume with an ext4 filesystem. The tool supports single-parity (SHR-1/RAID 5) and double-parity (SHR-2/RAID 6) configurations, handles self-healing rebuilds when disks fail, and supports hot-adding or replacing disks of different sizes with automatic array reshaping. PoolForge manages multiple independent Pools on the same system, each with its own isolated set of RAID_Arrays, Volume_Group, and Logical_Volume.

The backend is implemented in Go (single binary, system-level operations, strong long-term support). The web portal frontend is implemented in React (widely supported, large ecosystem). The architecture is designed to be extensible for future notification support (email, webhook, Slack) without requiring core redesign.

## Glossary

- **PoolForge**: The open-source storage management tool being specified
- **Pool**: A named collection of physical disks managed by PoolForge, presented as a single logical volume. Each Pool is independent and isolated from other Pools on the same system, with its own Volume_Group, Logical_Volume, and set of RAID_Arrays
- **Capacity_Tier**: A group of equal-sized partition slices derived from the smallest common capacity across disks in the pool
- **Slice**: A partition on a physical disk sized to match a specific Capacity_Tier
- **RAID_Array**: An mdadm software RAID array composed of same-sized Slices from different disks
- **Volume_Group**: An LVM volume group that aggregates all RAID_Arrays in a single Pool as physical volumes. Each Pool has exactly one Volume_Group
- **Logical_Volume**: An LVM logical volume created on top of the Volume_Group, presented as the usable storage. Each Pool has exactly one Logical_Volume
- **Parity_Mode**: The redundancy level — SHR-1 (single parity, RAID 5 behavior) or SHR-2 (double parity, RAID 6 behavior)
- **Disk_Descriptor**: A block device path (e.g., /dev/sdb) identifying a physical disk managed by PoolForge
- **Partition_Table**: The GPT partition layout PoolForge creates on each managed disk
- **Rebuild**: The process of reconstructing redundancy in a degraded RAID_Array after a disk failure
- **Reshape**: The process of modifying an existing RAID_Array to accommodate a new or replacement disk
- **Metadata_Store**: A persistent record of Pool configuration, disk membership, Capacity_Tiers, and RAID_Array mappings
- **Web_Portal**: A browser-based management interface served by PoolForge for configuring and monitoring Pools, built with React
- **API_Server**: The HTTP backend process (Go) that serves the Web_Portal and exposes a REST API for Pool management operations
- **Session**: An authenticated user interaction with the Web_Portal, identified by a session token
- **Uniform_Pool**: A Pool in which all member disks have the same raw capacity, resulting in a single Capacity_Tier
- **Interface_Type**: The physical connection interface through which a disk is attached to the system (e.g., SATA, eSATA, USB, or other DAS interfaces)
- **DAS**: Direct-Attached Storage — storage devices connected directly to the host system via a local bus or cable, as opposed to network-attached storage
- **Hot_Plug_Event**: A hardware event in which a disk is physically connected or disconnected while the system is running, supported by interfaces such as eSATA and USB
- **Pre_Operation_Check**: A set of validation and consistency checks performed by PoolForge before executing any destructive or modifying operation to confirm data safety
- **Atomic_Operation**: An operation that either completes fully or rolls back entirely, leaving the Pool in its prior consistent state
- **Storage_Map**: A visual topology diagram in the Web_Portal that renders Pools as containers, RAID_Arrays as blocks within those containers, and disks as individual icons within each RAID_Array block
- **Health_Color**: A color code applied to visual elements in the Storage_Map — green for healthy, amber for degraded or rebuilding, red for failed
- **Detail_Panel**: A contextual information panel in the Web_Portal that opens when the administrator clicks a Pool, RAID_Array, or disk element in the Storage_Map
- **Log_Entry**: A single timestamped record in the PoolForge event log, containing a severity level, a source component identifier, and a message
- **Log_Level**: The severity classification of a Log_Entry — one of debug, info, warning, or error
- **Log_Viewer**: A component of the Web_Portal that displays, filters, and exports Log_Entries
- **Live_Tail**: A real-time streaming mode in the Log_Viewer that appends new Log_Entries to the display as they are generated
- **SMART_Data**: Self-Monitoring, Analysis, and Reporting Technology data retrieved from a physical disk, containing health attributes, error counters, and predictive failure indicators
- **SMART_Check**: A periodic inspection of SMART_Data from a managed disk performed by PoolForge to assess disk health
- **SMART_Event**: A logged occurrence when a SMART_Check detects a warning threshold breach or a significant change in disk health attributes
- **Test_Environment**: An automated, cloud-provisioned infrastructure on AWS consisting of EC2 instances with attached EBS volumes, used to simulate real disk operations for integration and end-to-end testing of PoolForge
- **IaC_Template**: An Infrastructure as Code definition (Terraform) that declaratively specifies all AWS resources required for the Test_Environment
- **Test_Runner**: A script that orchestrates the full test lifecycle: provisioning the Test_Environment, executing the test suite, collecting results and logs, and tearing down all resources
- **EBS_Volume**: An Amazon Elastic Block Store volume attached to an EC2 instance, used in the Test_Environment to simulate a physical disk managed by PoolForge
- **Teardown**: The process of destroying all AWS resources created for a Test_Environment to prevent orphaned resources and ongoing costs

## Requirements

### Requirement 1: Pool Creation with Mixed-Size Disks

**User Story:** As a system administrator, I want to create a storage pool from disks of varying sizes, so that I can maximize usable capacity while maintaining redundancy.

#### Acceptance Criteria

1. WHEN the administrator provides two or more Disk_Descriptors and a Parity_Mode, THE PoolForge SHALL create a new Pool by partitioning each disk into Slices aligned to the computed Capacity_Tiers.
2. WHEN computing Capacity_Tiers, THE PoolForge SHALL sort all disk capacities, identify unique capacity values, and derive Slice sizes equal to the difference between consecutive sorted unique capacities (with the smallest capacity as the first tier).
3. WHEN partitioning a disk, THE PoolForge SHALL create one Slice per Capacity_Tier for which the disk has sufficient remaining capacity, using GPT Partition_Tables.
4. WHEN all disks are partitioned, THE PoolForge SHALL create one RAID_Array per Capacity_Tier from the corresponding Slices across all eligible disks.
5. WHEN Parity_Mode is SHR-1 and a Capacity_Tier has three or more Slices, THE PoolForge SHALL create the RAID_Array with RAID 5 redundancy.
6. WHEN Parity_Mode is SHR-1 and a Capacity_Tier has exactly two Slices, THE PoolForge SHALL create the RAID_Array with RAID 1 redundancy.
7. WHEN Parity_Mode is SHR-2 and a Capacity_Tier has four or more Slices, THE PoolForge SHALL create the RAID_Array with RAID 6 redundancy.
8. WHEN Parity_Mode is SHR-2 and a Capacity_Tier has exactly three Slices, THE PoolForge SHALL create the RAID_Array with RAID 5 redundancy.
9. WHEN Parity_Mode is SHR-2 and a Capacity_Tier has exactly two Slices, THE PoolForge SHALL create the RAID_Array with RAID 1 redundancy.
10. WHEN all RAID_Arrays are created, THE PoolForge SHALL register each RAID_Array as a physical volume in a single Volume_Group and create one Logical_Volume spanning the entire Volume_Group.
11. WHEN the Logical_Volume is created, THE PoolForge SHALL create an ext4 filesystem on the Logical_Volume.
12. IF fewer than two Disk_Descriptors are provided, THEN THE PoolForge SHALL reject the request with an error message stating the minimum disk count.
13. IF any provided Disk_Descriptor refers to a disk already in an existing Pool, THEN THE PoolForge SHALL reject the request with an error identifying the conflicting disk.
14. THE PoolForge SHALL assign a unique name to each Pool and store the Pool configuration in the Metadata_Store, ensuring the Pool is independent and isolated from all other Pools on the system.

### Requirement 2: Multi-Pool Isolation

**User Story:** As a system administrator, I want to manage multiple independent storage pools on the same system, so that I can organize storage for different purposes without pools interfering with each other.

#### Acceptance Criteria

1. THE PoolForge SHALL support creating and managing multiple independent Pools on the same system.
2. THE PoolForge SHALL assign each Pool its own dedicated Volume_Group, Logical_Volume, and set of RAID_Arrays, with no shared components between Pools.
3. IF the administrator attempts to add a Disk_Descriptor that is already a member of another Pool, THEN THE PoolForge SHALL reject the operation with an error identifying the owning Pool.
4. WHEN the administrator requests a list of all Pools, THE PoolForge SHALL display each Pool with its name, state, capacity, and member disk count.
5. WHEN a disk failure occurs, THE PoolForge SHALL limit the impact to the Pool containing the failed disk and leave all other Pools unaffected.
6. WHEN the administrator deletes a Pool, THE PoolForge SHALL remove only the RAID_Arrays, Volume_Group, Logical_Volume, and Partition_Table entries belonging to that Pool, leaving all other Pools intact.

### Requirement 3: Pool Status and Health Monitoring

**User Story:** As a system administrator, I want to view the health and status of my storage pool with a clear breakdown of every RAID array and disk, so that I can detect degraded arrays, identify exactly which components are affected, and take corrective action.

#### Acceptance Criteria

1. WHEN the administrator requests Pool status, THE PoolForge SHALL display the overall Pool state (healthy, degraded, or failed), the total usable capacity, the used capacity, and the Logical_Volume usage.
2. WHEN the administrator requests Pool status, THE PoolForge SHALL list every RAID_Array composing the Pool, showing for each array: the RAID level, the Capacity_Tier, the array state (healthy, degraded, rebuilding, or failed), the array capacity, and the member Disk_Descriptors.
3. WHEN the administrator requests Pool status, THE PoolForge SHALL list every physical disk in the Pool, showing for each disk: the Disk_Descriptor, the overall disk health (healthy, degraded, or failed), and the RAID_Arrays to which the disk contributes Slices.
4. WHEN a RAID_Array is in a degraded state, THE PoolForge SHALL identify the specific failed or missing Disk_Descriptor and the affected Capacity_Tier in the status output.
5. WHEN a RAID_Array is actively rebuilding, THE PoolForge SHALL report the rebuild progress as a percentage, the estimated time remaining, and the Disk_Descriptor of the disk being rebuilt onto.
6. WHEN a disk is in a failed state, THE PoolForge SHALL identify all RAID_Arrays affected by that disk failure in the status output.
7. THE PoolForge SHALL assign a distinct state label to every level of the hierarchy: Pool (healthy, degraded, failed), RAID_Array (healthy, degraded, rebuilding, failed), and disk (healthy, failed).
8. WHEN the administrator requests detailed status for a specific RAID_Array, THE PoolForge SHALL display the sync state (clean, active, resyncing, recovering, or degraded), the RAID level, the Capacity_Tier, the member Disk_Descriptors with per-disk state, and the array capacity.
9. THE PoolForge SHALL persist Pool configuration in the Metadata_Store so that Pool state survives system reboots.
10. WHEN the system boots, THE PoolForge SHALL reassemble all RAID_Arrays and reactivate the Volume_Group and Logical_Volume from the Metadata_Store without manual intervention.

### Requirement 4: Disk Failure Detection and Self-Healing

**User Story:** As a system administrator, I want the system to detect disk failures and automatically begin rebuilding degraded arrays, so that data remains protected with minimal manual intervention.

#### Acceptance Criteria

1. WHEN mdadm reports a disk failure event for a disk in a Pool, THE PoolForge SHALL mark the disk as failed in the Metadata_Store and log the failure with the disk identifier and timestamp.
2. WHEN a hot spare disk is available in the Pool, THE PoolForge SHALL automatically initiate a Rebuild of each degraded RAID_Array using the spare disk.
3. WHEN a Rebuild completes for a RAID_Array, THE PoolForge SHALL update the Metadata_Store to reflect the restored state and log the completion.
4. IF multiple RAID_Arrays are degraded due to the same disk failure, THEN THE PoolForge SHALL rebuild all affected RAID_Arrays using the replacement or spare disk.
5. IF a second disk fails while a Rebuild is in progress in SHR-1 Parity_Mode, THEN THE PoolForge SHALL mark the affected RAID_Arrays as failed and log a critical alert.
6. IF a second disk fails while a Rebuild is in progress in SHR-2 Parity_Mode, THEN THE PoolForge SHALL continue operating in degraded mode and log a warning alert.

### Requirement 5: Add a New Disk to an Existing Pool

**User Story:** As a system administrator, I want to add a new disk of any size to an existing pool, so that I can expand storage capacity without recreating the pool.

#### Acceptance Criteria

1. WHEN the administrator adds a new Disk_Descriptor to an existing Pool, THE PoolForge SHALL partition the new disk into Slices matching the existing Capacity_Tiers for which the disk has sufficient capacity.
2. WHEN the new disk produces Slices for existing Capacity_Tiers, THE PoolForge SHALL add each Slice to the corresponding RAID_Array by reshaping the array to include the new member.
3. WHEN the new disk has remaining capacity beyond all existing Capacity_Tiers, THE PoolForge SHALL compute new Capacity_Tiers from the leftover space, create new RAID_Arrays from the new Slices, and add them to the Volume_Group.
4. WHEN all RAID_Arrays are updated, THE PoolForge SHALL extend the Logical_Volume to use the newly available space in the Volume_Group and resize the ext4 filesystem to fill the expanded Logical_Volume.
5. WHEN reshaping a RAID_Array, THE PoolForge SHALL maintain the existing Parity_Mode redundancy level throughout the reshape operation.
6. IF the new disk is smaller than the smallest existing Capacity_Tier, THEN THE PoolForge SHALL create a new smallest Capacity_Tier, repartition existing disks to include the new tier, and reshape all affected RAID_Arrays.
7. IF the Disk_Descriptor refers to a disk already in the Pool, THEN THE PoolForge SHALL reject the request with an error identifying the duplicate disk.

### Requirement 6: Web Portal Management Interface with Rich Visualization

**User Story:** As a system administrator, I want a browser-based management interface with an interactive visual topology map of my storage hierarchy, so that I can see at a glance which pools, RAID arrays, and disks are healthy, degraded, or failed, and drill into any component for full details.

#### Acceptance Criteria

1. THE Web_Portal SHALL display a dashboard listing all Pools managed by PoolForge, showing each Pool name, overall state (healthy, degraded, or failed), total capacity, and used capacity.
2. THE Web_Portal SHALL render a Storage_Map for each Pool that displays the Pool as a visual container, each RAID_Array within the Pool as a distinct block or card inside that container, and each disk within a RAID_Array as an individual icon or block inside the corresponding RAID_Array card.
3. THE Web_Portal SHALL apply Health_Color coding to every element in the Storage_Map: green for healthy disks, amber for degraded or rebuilding disks, and red for failed disks.
4. THE Web_Portal SHALL apply Health_Color coding to every RAID_Array card in the Storage_Map: green for healthy arrays, amber for degraded or rebuilding arrays, and red for failed arrays.
5. THE Web_Portal SHALL apply Health_Color coding to every Pool container in the Storage_Map: green for healthy Pools, amber for degraded Pools, and red for failed Pools.
6. WHEN the administrator clicks a Pool container in the Storage_Map, THE Web_Portal SHALL open a Detail_Panel displaying the Pool name, overall state, Parity_Mode, total capacity, used capacity, and a list of all member RAID_Arrays with their states.
7. WHEN the administrator clicks a RAID_Array card in the Storage_Map, THE Web_Portal SHALL open a Detail_Panel displaying the sync state (clean, active, resyncing, recovering, or degraded), the RAID level, the Capacity_Tier, the array capacity, and the member Disk_Descriptors with per-disk state.
8. WHEN the administrator clicks a disk icon in the Storage_Map, THE Web_Portal SHALL open a Detail_Panel displaying the Disk_Descriptor, the disk health (healthy, degraded, or failed), the Interface_Type, the raw capacity, the latest SMART_Data summary, and a list of all RAID_Arrays to which the disk contributes Slices.
9. WHEN a RAID_Array is actively rebuilding, THE Web_Portal SHALL display a progress bar on the affected RAID_Array card in the Storage_Map showing the rebuild progress as a percentage and estimated time remaining.
10. WHEN a disk is actively being rebuilt onto, THE Web_Portal SHALL display a progress bar on the affected disk icon in the Storage_Map showing the rebuild progress as a percentage.
11. WHEN a disk is in a failed state, THE Web_Portal SHALL visually highlight that disk icon in red and apply amber highlighting to all RAID_Array cards affected by the failure in the Storage_Map.
12. THE Web_Portal SHALL provide a navigation path from Pool to RAID_Arrays to individual disks, enabling the administrator to drill down through the hierarchy to locate the source of a problem.
13. THE API_Server SHALL serve the Web_Portal and expose a REST API that provides the same Pool, RAID_Array, and disk status data displayed in the Web_Portal.
14. WHEN the administrator accesses the Web_Portal, THE API_Server SHALL require authentication and establish a Session before granting access to Pool management operations.

### Requirement 7: Authentication

**User Story:** As a system administrator, I want the web portal to require login credentials, so that only authorized users can manage storage pools.

#### Acceptance Criteria

1. THE API_Server SHALL authenticate administrators using local username and password credentials stored on the PoolForge system.
2. WHEN an administrator submits valid credentials, THE API_Server SHALL create a Session and return a session token to the Web_Portal.
3. WHEN an administrator submits invalid credentials, THE API_Server SHALL reject the login attempt with an error message and log the failed attempt.
4. THE API_Server SHALL require a valid session token for all API endpoints that perform Pool management operations or retrieve Pool data.
5. IF a request is made with an expired or invalid session token, THEN THE API_Server SHALL reject the request with an authentication error and redirect the Web_Portal to the login page.
6. THE API_Server SHALL store password credentials using a secure one-way hash with a per-user salt.
7. THE API_Server SHALL provide an API endpoint and CLI command for creating and managing local user accounts.

### Requirement 8: Integrated Log Viewer

**User Story:** As a system administrator, I want to view, filter, search, and export PoolForge logs directly in the Web_Portal, so that I can diagnose issues and audit system events without accessing the server CLI or external log tools.

#### Acceptance Criteria

1. THE Web_Portal SHALL provide a Log_Viewer page that displays Log_Entries in reverse chronological order, showing for each entry: the timestamp, the Log_Level, the source component identifier (Pool name, RAID_Array identifier, or Disk_Descriptor), and the message.
2. THE Log_Viewer SHALL allow the administrator to filter displayed Log_Entries by one or more Log_Levels (debug, info, warning, or error).
3. THE Log_Viewer SHALL allow the administrator to filter displayed Log_Entries by a time range specified as a start timestamp and an end timestamp.
4. THE Log_Viewer SHALL allow the administrator to filter displayed Log_Entries by a specific source component: a named Pool, a specific RAID_Array, or a specific Disk_Descriptor.
5. THE Log_Viewer SHALL allow the administrator to search displayed Log_Entries by a keyword substring match against the message field.
6. WHEN multiple filters are active simultaneously, THE Log_Viewer SHALL apply all filters as a logical AND, displaying only Log_Entries that satisfy every active filter.
7. THE Log_Viewer SHALL support a Live_Tail mode that streams new Log_Entries to the display in real time as they are generated, without requiring the administrator to refresh the page.
8. WHEN Live_Tail mode is active and filters are applied, THE Log_Viewer SHALL apply the active filters to incoming Log_Entries and display only those that match.
9. WHEN the administrator views a Detail_Panel for a specific Pool, RAID_Array, or disk, THE Web_Portal SHALL display a contextual log section within the Detail_Panel showing recent Log_Entries filtered to that component.
10. THE Log_Viewer SHALL allow the administrator to export the currently displayed (filtered) Log_Entries as a downloadable file.
11. THE API_Server SHALL expose REST API endpoints that provide Log_Entry data with support for the same filtering parameters available in the Log_Viewer (Log_Level, time range, source component, and keyword search).

### Requirement 9: SMART Disk Health Monitoring

**User Story:** As a system administrator, I want PoolForge to monitor SMART data from all managed disks, so that I can predict disk failures before they happen and take preventive action rather than reacting to failures after data is at risk.

#### Acceptance Criteria

1. THE PoolForge SHALL perform periodic SMART_Checks on all disks managed by any Pool, at a configurable interval with a default of once per hour.
2. WHEN a SMART_Check completes, THE PoolForge SHALL store the retrieved SMART_Data in the Metadata_Store associated with the corresponding Disk_Descriptor.
3. WHEN a SMART_Check detects that a disk attribute has crossed a warning threshold (e.g., reallocated sector count, current pending sector count, or uncorrectable error count exceeding configured limits), THE PoolForge SHALL generate a SMART_Event and log a warning-level Log_Entry identifying the disk, the attribute, and the threshold breached.
4. WHEN a SMART_Check detects that a disk reports a SMART overall-health status of "FAILED", THE PoolForge SHALL generate a SMART_Event and log an error-level Log_Entry identifying the disk.
5. THE Web_Portal SHALL display SMART health indicators on each disk icon in the Storage_Map, using amber for disks with SMART warnings and red for disks with SMART failure status.
6. WHEN the administrator views the Detail_Panel for a disk, THE Web_Portal SHALL display the latest SMART_Data attributes, the history of SMART_Events for that disk, and the time of the last SMART_Check.
7. THE CLI SHALL provide a command to display the current SMART_Data and SMART_Event history for a specified Disk_Descriptor.
8. THE PoolForge SHALL allow the administrator to configure SMART warning thresholds via the CLI or API_Server.
9. IF a SMART_Check cannot retrieve data from a disk (e.g., the disk does not support SMART or the query times out), THEN THE PoolForge SHALL log an info-level Log_Entry and mark the disk SMART status as "unavailable" in the Metadata_Store.

### Requirement 10: Platform and Technology Constraints

**User Story:** As a system administrator, I want PoolForge to run on my Ubuntu LTS server using well-supported technologies, so that I can rely on long-term stability and community support.

#### Acceptance Criteria

1. THE PoolForge backend (API_Server, CLI, and core engine) SHALL be implemented in Go and distributed as a single statically-linked binary.
2. THE Web_Portal frontend SHALL be implemented in React and served as static assets by the API_Server.
3. THE PoolForge SHALL target Ubuntu LTS 24.04 or later as the supported platform.
4. THE PoolForge SHALL depend only on system packages available in the default Ubuntu LTS repositories (mdadm, lvm2, smartmontools, and standard filesystem utilities).
5. THE PoolForge SHALL create ext4 filesystems on all Logical_Volumes, using the ext4 utilities provided by the Ubuntu LTS base system.

### Requirement 11: Testing

**User Story:** As a developer, I want a comprehensive test suite for each implementation phase, so that I can verify new functionality and confirm no regressions in previously completed phases.

#### Acceptance Criteria

1. THE PoolForge project SHALL maintain a test suite that includes unit tests, integration tests, and end-to-end tests.
2. WHEN a new implementation phase is completed, THE test suite SHALL include tests that validate all functionality introduced in that phase.
3. WHEN a new implementation phase is completed, THE test suite SHALL include regression tests that confirm all functionality from previous phases remains correct.
4. THE unit tests SHALL validate individual functions and algorithms in isolation, including Capacity_Tier computation, Slice sizing, RAID level selection, and Metadata_Store operations.
5. THE integration tests SHALL validate interactions between PoolForge components and system tools (mdadm, LVM, ext4 utilities, smartmontools), including disk partitioning, RAID_Array creation, Volume_Group assembly, and filesystem creation.
6. THE end-to-end tests SHALL validate complete user workflows through the CLI and API_Server, including Pool creation, disk addition, disk replacement, failure detection, and Web_Portal operations.
7. THE test suite SHALL include failure injection tests that simulate disk failures, interrupted operations, and degraded states to validate self-healing and rollback behavior.
8. THE integration tests and end-to-end tests SHALL execute against the cloud-based Test_Environment defined in Requirement 12, using EC2 instances with attached EBS_Volumes to simulate real disk operations.
9. WHEN the Test_Runner provisions the Test_Environment, THE test suite SHALL treat the attached EBS_Volumes as the Disk_Descriptors for all integration and end-to-end test scenarios.

### Requirement 12: Automated Cloud-Based Test Infrastructure

**User Story:** As a developer, I want an automated cloud-based test environment that provisions real disk infrastructure on AWS, so that I can run integration and end-to-end tests against actual block devices without maintaining dedicated hardware.

#### Acceptance Criteria

1. THE PoolForge project SHALL include an IaC_Template (Terraform) that defines all AWS resources required for the Test_Environment, including an EC2 instance running Ubuntu LTS 24.04 or later with PoolForge installed, and multiple EBS_Volumes of varying sizes (ranging from 1 GB to 10 GB) attached to the instance.
2. WHEN the administrator executes a single provisioning command, THE IaC_Template SHALL create the EC2 instance, attach all EBS_Volumes, install PoolForge on the instance, and output the connection details for the Test_Environment.
3. WHEN the administrator executes a single teardown command, THE IaC_Template SHALL destroy all AWS resources created for the Test_Environment, including the EC2 instance and all attached EBS_Volumes.
4. THE IaC_Template SHALL use cost-effective resource types (t3.medium or equivalent EC2 instance type and gp3 EBS_Volumes of 1 GB to 10 GB) to minimize test infrastructure costs.
5. THE Test_Environment SHALL support automated execution of the following test scenarios against real EBS_Volumes: Pool creation with mixed-size EBS_Volumes, disk failure simulation by detaching an EBS_Volume from the running instance, self-healing and Rebuild verification after simulated failure, Pool expansion by attaching a new EBS_Volume and executing the add-disk workflow, disk replacement by detaching a failed EBS_Volume and attaching a new one, and multi-Pool creation with isolation verification.
6. THE Test_Environment SHALL execute a full lifecycle test scenario: create a Pool, write data to the Pool, simulate a disk failure by detaching an EBS_Volume, verify Rebuild completion, expand the Pool by attaching a new EBS_Volume, and verify data integrity after all operations.
7. WHEN a test scenario requires SMART monitoring validation, THE Test_Environment SHALL use a mock SMART data provider, because EBS_Volumes do not support SMART queries.
8. THE PoolForge project SHALL include a Test_Runner script that provisions the Test_Environment, executes the full test suite against the live environment, collects test results and logs from the EC2 instance, tears down the Test_Environment, and reports a pass or fail status for each test scenario.
9. THE Test_Runner SHALL return a non-zero exit code when any test scenario fails, enabling integration with CI/CD pipelines.
10. THE Test_Runner SHALL complete the Teardown of all AWS resources regardless of whether the test suite passes or fails, to prevent orphaned resources.
11. IF the Teardown fails to destroy any AWS resource, THEN THE Test_Runner SHALL log an error identifying each orphaned resource by its AWS resource identifier.
12. THE IaC_Template SHALL tag all created AWS resources with a consistent identifier so that orphaned resources can be identified and cleaned up manually if Teardown fails.

## Implementation Phases

The following phases define the build order for PoolForge. Each phase builds on the previous one without breaking it. Interfaces between layers are designed so that later phases can extend earlier work cleanly.

### Phase 1: Core Engine and Test Infrastructure

**Scope:** Capacity-tier computation, disk partitioning, mdadm array creation, LVM stitching, ext4 filesystem creation, CLI (create pool, status), metadata store. Automated cloud-based Test_Environment (Terraform IaC_Template, Test_Runner script, EC2 provisioning with EBS_Volumes) so that real-disk integration testing is available for this phase and all subsequent phases.

**Testing:** Unit tests for tier computation and RAID level selection. Integration tests for disk partitioning, array creation, LVM assembly, and ext4 filesystem creation, executed against the cloud-based Test_Environment.

### Phase 2: Lifecycle Operations

**Scope:** Add disk, replace disk, remove disk, self-healing/rebuild, expansion detection and admin-approved expansion.

**Testing:** Integration tests for each lifecycle operation. Failure scenario tests for disk failure, rebuild, and degraded-state handling. Regression tests confirming Phase 1 functionality.

### Phase 3: Web Portal, API Server, and Visualization

**Scope:** API server (Go), Web_Portal (React), Storage_Map visualization, Log_Viewer, authentication (local username/password).

**Testing:** API endpoint tests for all REST endpoints. UI component tests for Storage_Map, Detail_Panel, Log_Viewer, and authentication flows. End-to-end tests for complete web-based workflows. Regression tests confirming Phase 1 and Phase 2 functionality.

### Phase 4: Safety Hardening

**Scope:** Zero data loss guarantees, atomic operations with rollback, multi-interface support (eSATA, USB, DAS), SMART monitoring integration.

**Testing:** Failure injection tests for interrupted operations and rollback verification. Multi-interface detection and hot-plug tests. SMART monitoring tests. Regression tests confirming Phase 1, Phase 2, and Phase 3 functionality.

## Future Considerations

- **Notifications:** The architecture should support future addition of notification channels (email, webhook, Slack) for alerting on disk failures, SMART warnings, rebuild completions, and other significant events. This is not in scope for the initial implementation but the event logging and SMART_Event systems are designed to serve as extension points for notification delivery.
