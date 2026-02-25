# Requirements Document — Phase 1: Core Engine and Test Infrastructure

## Introduction

This document specifies the requirements for Phase 1 of the PoolForge project. PoolForge is an open-source storage management tool for Ubuntu LTS (24.04+) that replicates Synology Hybrid RAID (SHR) functionality using mdadm and LVM. The full project scope is defined in the master spec at `.kiro/specs/hybrid-raid-manager/`.

Phase 1 establishes the core engine: capacity-tier computation from mixed-size disks, GPT disk partitioning, mdadm RAID array creation, LVM stitching (PV → VG → LV), ext4 filesystem creation, a CLI for pool management, and a JSON-based metadata store with atomic writes. Phase 1 also delivers the automated cloud-based test infrastructure (Terraform IaC for EC2 + EBS, Test_Runner script) so that real-disk integration testing is available from the start.

This is Phase 1 of 4. Subsequent phases build on the interfaces established here:

| Phase | Scope |
|-------|-------|
| **Phase 1** | Core engine, CLI (create/status/list), metadata store, test infrastructure |
| Phase 2 | Lifecycle operations (add disk, replace disk, remove disk, self-healing/rebuild) |
| Phase 3 | Web portal (React), API server (Go REST), Storage_Map, Log_Viewer, authentication |
| Phase 4 | Safety hardening (atomic operations, rollback, multi-interface, SMART monitoring) |

Phase 1 interfaces (EngineService, StorageAbstraction, MetadataStore) are designed for extensibility. Phase 2 will add AddDisk/ReplaceDisk/RemoveDisk methods to EngineService. Phase 3 will layer the API server on top of the same EngineService interface. Phase 4 will wrap operations with atomic/rollback semantics. The MetadataStore schema is versioned to support forward-compatible migrations.

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
- **Metadata_Store**: A persistent record of Pool configuration, disk membership, Capacity_Tiers, and RAID_Array mappings, stored as JSON with atomic writes (write-to-temp + fsync + rename)
- **Uniform_Pool**: A Pool in which all member disks have the same raw capacity, resulting in a single Capacity_Tier
- **Test_Environment**: An automated, cloud-provisioned infrastructure on AWS consisting of EC2 instances with attached EBS volumes, used to simulate real disk operations for integration testing of PoolForge
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

### Requirement 2: Multi-Pool Isolation (Phase 1 Scope)

**User Story:** As a system administrator, I want to manage multiple independent storage pools on the same system, so that I can organize storage for different purposes without pools interfering with each other.

*Note: This requirement covers basic multi-pool creation and isolation. Deletion isolation (criterion 6 from master) and failure isolation (criterion 5 from master) are deferred to Phase 2 when lifecycle operations are implemented.*

#### Acceptance Criteria

1. THE PoolForge SHALL support creating and managing multiple independent Pools on the same system.
2. THE PoolForge SHALL assign each Pool its own dedicated Volume_Group, Logical_Volume, and set of RAID_Arrays, with no shared components between Pools.
3. IF the administrator attempts to add a Disk_Descriptor that is already a member of another Pool, THEN THE PoolForge SHALL reject the operation with an error identifying the owning Pool.
4. WHEN the administrator requests a list of all Pools, THE PoolForge SHALL display each Pool with its name, state, capacity, and member disk count.

### Requirement 3: Pool Status and Health Monitoring (Phase 1 Scope)

**User Story:** As a system administrator, I want to view the health and status of my storage pool with a clear breakdown of every RAID array and disk, so that I can detect issues and understand the pool structure.

*Note: This requirement covers basic status reporting for healthy pools. Degraded/rebuilding state details (criteria 4, 5, 6, 8 from master) are deferred to Phase 2 when self-healing is implemented.*

#### Acceptance Criteria

1. WHEN the administrator requests Pool status, THE PoolForge SHALL display the overall Pool state (healthy, degraded, or failed), the total usable capacity, the used capacity, and the Logical_Volume usage.
2. WHEN the administrator requests Pool status, THE PoolForge SHALL list every RAID_Array composing the Pool, showing for each array: the RAID level, the Capacity_Tier, the array state (healthy, degraded, rebuilding, or failed), the array capacity, and the member Disk_Descriptors.
3. WHEN the administrator requests Pool status, THE PoolForge SHALL list every physical disk in the Pool, showing for each disk: the Disk_Descriptor, the overall disk health (healthy, degraded, or failed), and the RAID_Arrays to which the disk contributes Slices.
4. THE PoolForge SHALL assign a distinct state label to every level of the hierarchy: Pool (healthy, degraded, failed), RAID_Array (healthy, degraded, rebuilding, failed), and disk (healthy, failed).
5. THE PoolForge SHALL persist Pool configuration in the Metadata_Store so that Pool state survives system reboots.
6. WHEN the system boots, THE PoolForge SHALL reassemble all RAID_Arrays and reactivate the Volume_Group and Logical_Volume from the Metadata_Store without manual intervention.

### Requirement 4: Platform and Technology Constraints

**User Story:** As a system administrator, I want PoolForge to run on my Ubuntu LTS server using well-supported technologies, so that I can rely on long-term stability and community support.

#### Acceptance Criteria

1. THE PoolForge backend (CLI and core engine) SHALL be implemented in Go and distributed as a single statically-linked binary.
2. THE PoolForge SHALL target Ubuntu LTS 24.04 or later as the supported platform.
3. THE PoolForge SHALL depend only on system packages available in the default Ubuntu LTS repositories (mdadm, lvm2, and standard filesystem utilities).
4. THE PoolForge SHALL create ext4 filesystems on all Logical_Volumes, using the ext4 utilities provided by the Ubuntu LTS base system.

*Note: Criterion 2 from master (React Web_Portal) is deferred to Phase 3. Criterion 4 from master (smartmontools dependency) is deferred to Phase 4. Phase 1 constrains dependencies to mdadm, lvm2, and ext4 utilities only.*

### Requirement 5: Testing (Phase 1 Scope)

**User Story:** As a developer, I want a test suite for Phase 1 functionality, so that I can verify the core engine and establish a regression baseline for subsequent phases.

*Note: This covers criteria 1-5 from master Requirement 11. End-to-end tests (criterion 6), failure injection tests (criterion 7), and EBS-as-disk-descriptor mapping (criteria 8-9) are partially in scope — integration tests run against the Test_Environment but full lifecycle E2E tests are Phase 2+.*

#### Acceptance Criteria

1. THE PoolForge project SHALL maintain a test suite that includes unit tests and integration tests.
2. WHEN Phase 1 is completed, THE test suite SHALL include tests that validate all functionality introduced in Phase 1.
3. WHEN a subsequent implementation phase is completed, THE test suite SHALL include regression tests that confirm all Phase 1 functionality remains correct.
4. THE unit tests SHALL validate individual functions and algorithms in isolation, including Capacity_Tier computation, Slice sizing, RAID level selection, and Metadata_Store operations.
5. THE integration tests SHALL validate interactions between PoolForge components and system tools (mdadm, LVM, ext4 utilities), including disk partitioning, RAID_Array creation, Volume_Group assembly, and filesystem creation.

### Requirement 6: Automated Cloud-Based Test Infrastructure

**User Story:** As a developer, I want an automated cloud-based test environment that provisions real disk infrastructure on AWS, so that I can run integration tests against actual block devices without maintaining dedicated hardware.

#### Acceptance Criteria

1. THE PoolForge project SHALL include an IaC_Template (Terraform) that defines all AWS resources required for the Test_Environment, including an EC2 instance running Ubuntu LTS 24.04 or later with PoolForge installed, and multiple EBS_Volumes of varying sizes (ranging from 1 GB to 10 GB) attached to the instance.
2. WHEN the administrator executes a single provisioning command, THE IaC_Template SHALL create the EC2 instance, attach all EBS_Volumes, install PoolForge on the instance, and output the connection details for the Test_Environment.
3. WHEN the administrator executes a single teardown command, THE IaC_Template SHALL destroy all AWS resources created for the Test_Environment, including the EC2 instance and all attached EBS_Volumes.
4. THE IaC_Template SHALL use cost-effective resource types (t3.medium or equivalent EC2 instance type and gp3 EBS_Volumes of 1 GB to 10 GB) to minimize test infrastructure costs.
5. THE Test_Environment SHALL support automated execution of the following test scenarios against real EBS_Volumes: Pool creation with mixed-size EBS_Volumes and multi-Pool creation with isolation verification.
6. THE Test_Environment SHALL execute a full lifecycle test scenario: create a Pool, write data to the Pool, verify data integrity, and verify Pool status reports correct structure.
7. THE PoolForge project SHALL include a Test_Runner script that provisions the Test_Environment, executes the full test suite against the live environment, collects test results and logs from the EC2 instance, tears down the Test_Environment, and reports a pass or fail status for each test scenario.
8. THE Test_Runner SHALL return a non-zero exit code when any test scenario fails, enabling integration with CI/CD pipelines.
9. THE Test_Runner SHALL complete the Teardown of all AWS resources regardless of whether the test suite passes or fails, to prevent orphaned resources.
10. IF the Teardown fails to destroy any AWS resource, THEN THE Test_Runner SHALL log an error identifying each orphaned resource by its AWS resource identifier.
11. THE IaC_Template SHALL tag all created AWS resources with a consistent identifier so that orphaned resources can be identified and cleaned up manually if Teardown fails.

*Note: Criteria 5 and 6 are scoped to Phase 1 test scenarios only. Master criteria covering disk failure simulation, self-healing verification, pool expansion, and disk replacement testing are deferred to Phase 2. SMART mock testing (master criterion 7) is deferred to Phase 4.*

## Extensibility Notes

The following interfaces established in Phase 1 are designed for forward compatibility with later phases:

- **EngineService**: Phase 1 implements `CreatePool`, `GetPool`, `ListPools`, `GetPoolStatus`. Phase 2 will add `AddDisk`, `ReplaceDisk`, `RemoveDisk`, `DeletePool`, `HandleDiskFailure`, `GetRebuildProgress`. Phase 3 will add no new engine methods but will expose the existing interface via REST API.
- **StorageAbstraction (DiskManager, RAIDManager, LVMManager, FilesystemManager)**: Phase 1 implements create/query operations. Phase 2 will add `ReshapeArray`, `AddMember`, `RemoveMember`, `ExtendVolumeGroup`, `ExtendLogicalVolume`, `ResizeFilesystem`. Phase 4 will wrap these with atomic operation semantics.
- **MetadataStore**: Phase 1 implements `SavePool`, `LoadPool`, `ListPools` with schema version 1. Phase 2 will add `DeletePool` and disk state tracking. Phase 4 will add `SaveSMARTData`, `LoadSMARTData`, and threshold persistence. The schema includes a `version` field for forward-compatible migrations.
