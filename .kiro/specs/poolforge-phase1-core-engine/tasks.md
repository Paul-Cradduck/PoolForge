# Implementation Plan: PoolForge Phase 1 — Core Engine and Test Infrastructure

## Overview

Incremental build-up of the PoolForge storage management tool in Go. Starts with project scaffolding and core data types, then implements the capacity-tier algorithm with property tests, followed by storage abstraction layer, metadata store, engine orchestration, CLI, and finally test infrastructure with integration tests. Each task builds on the previous, with no orphaned code.

## Tasks

- [ ] 1. Project scaffolding and core data types
  - [ ] 1.1 Initialize Go module and directory structure
    - Run `go mod init` for the PoolForge module
    - Create directory structure: `cmd/poolforge/`, `internal/engine/`, `internal/storage/`, `internal/metadata/`, `test/integration/`, `test/infra/`
    - Add `rapid` dependency (`github.com/flyingmutant/rapid`)
    - Create a minimal `cmd/poolforge/main.go` placeholder
    - _Requirements: 4.1_

  - [ ] 1.2 Define core data types and interfaces
    - Create `internal/engine/types.go` with all Phase 1 types: `ParityMode`, `PoolState`, `ArrayState`, `DiskState`, `Pool`, `DiskInfo`, `SliceInfo`, `CapacityTier`, `RAIDArray`, `CreatePoolRequest`, `PoolSummary`, `PoolStatus`, `ArrayStatus`, `DiskStatusInfo`
    - Create `internal/engine/engine.go` with the `EngineService` interface definition
    - _Requirements: 1.1, 1.2, 1.5–1.9, 3.4_

  - [ ] 1.3 Define storage abstraction interfaces
    - Create `internal/storage/interfaces.go` with `DiskManager`, `RAIDManager`, `LVMManager`, `FilesystemManager` interfaces and their associated types (`Partition`, `RAIDCreateOpts`, `RAIDArrayInfo`, `RAIDArrayDetail`, `MemberInfo`, `VGInfo`, `FSUsage`)
    - _Requirements: 4.3_

  - [ ] 1.4 Define metadata store interface
    - Create `internal/metadata/store.go` with the `MetadataStore` interface and the JSON schema struct for serialization (version field, pools map)
    - _Requirements: 3.5, 1.14_

- [ ] 2. Capacity tier computation algorithm
  - [ ] 2.1 Implement `ComputeCapacityTiers` function
    - Create `internal/engine/tiers.go`
    - Implement the tier computation algorithm: sort capacities, extract unique values, compute slice sizes as differences between consecutive unique capacities
    - Compute eligible disk count per tier
    - Skip tiers with fewer than 2 eligible disks
    - _Requirements: 1.2, 1.3_

  - [ ] 2.2 Implement `SelectRAIDLevel` function
    - Create `internal/engine/raid_selection.go`
    - Implement RAID level selection based on parity mode and eligible disk count per the selection table
    - _Requirements: 1.5, 1.6, 1.7, 1.8, 1.9_

  - [ ] 2.3 Implement `ComputeDiskSlices` function
    - Create `internal/engine/slicing.go`
    - Given a disk capacity and computed tiers, return the list of slices (tier index + size) the disk is eligible for
    - _Requirements: 1.1, 1.3_

  - [ ]* 2.4 Write property test for capacity tier computation (P1)
    - **Property 1: Capacity tier computation produces correct slice sizes**
    - Generate random disk capacity sets (≥2 disks), verify: slice sizes equal differences between consecutive sorted unique capacities, first tier equals smallest capacity, sum of slice sizes equals largest capacity, tier count equals unique capacity count
    - **Validates: Requirements 1.2**

  - [ ]* 2.5 Write property test for disk slicing (P2)
    - **Property 2: Disk slicing matches eligible tiers**
    - Generate random disk capacity and tier sets, verify each disk receives exactly one slice per tier for which cumulative boundary ≤ disk capacity
    - **Validates: Requirements 1.1, 1.3**

  - [ ]* 2.6 Write property test for one RAID array per tier (P3)
    - **Property 3: One RAID array per capacity tier**
    - Generate random disk sets, compute tiers, verify array count equals tier count (with ≥2 eligible disks) and each array contains exactly the eligible slices
    - **Validates: Requirements 1.4**

  - [ ]* 2.7 Write property test for RAID level selection (P4)
    - **Property 4: RAID level selection follows parity mode and disk count rules**
    - Generate random parity mode and eligible disk count, verify selected RAID level matches the selection table
    - **Validates: Requirements 1.5, 1.6, 1.7, 1.8, 1.9**

- [ ] 3. Checkpoint — Verify tier computation
  - Ensure all tests pass, ask the user if questions arise.


- [ ] 4. Storage abstraction implementations
  - [ ] 4.1 Implement `DiskManager` (sgdisk wrapper)
    - Create `internal/storage/disk.go`
    - Implement `GetDiskInfo`: parse disk capacity from blockdev/lsblk
    - Implement `CreateGPTPartitionTable`: exec `sgdisk --zap-all` then `sgdisk --clear`
    - Implement `CreatePartition`: exec `sgdisk --new` with computed start/size sectors
    - Implement `ListPartitions`: exec `sgdisk --print` and parse output
    - _Requirements: 1.1, 1.3, 4.3_

  - [ ] 4.2 Implement `RAIDManager` (mdadm wrapper)
    - Create `internal/storage/raid.go`
    - Implement `CreateArray`: exec `mdadm --create` with level, metadata version 1.2, and member devices
    - Implement `GetArrayDetail`: exec `mdadm --detail` and parse output for state, members, capacity
    - Implement `AssembleArray`: exec `mdadm --assemble` for boot reassembly
    - Implement `StopArray`: exec `mdadm --stop`
    - _Requirements: 1.4, 1.5–1.9, 3.6_

  - [ ] 4.3 Implement `LVMManager` (LVM2 wrapper)
    - Create `internal/storage/lvm.go`
    - Implement `CreatePhysicalVolume`: exec `pvcreate`
    - Implement `CreateVolumeGroup`: exec `vgcreate`
    - Implement `CreateLogicalVolume`: exec `lvcreate -l 100%FREE`
    - Implement `GetVolumeGroupInfo`: exec `vgdisplay` and parse output
    - _Requirements: 1.10_

  - [ ] 4.4 Implement `FilesystemManager` (ext4 wrapper)
    - Create `internal/storage/fs.go`
    - Implement `CreateFilesystem`: exec `mkfs.ext4`
    - Implement `MountFilesystem`: exec `mount`
    - Implement `UnmountFilesystem`: exec `umount`
    - Implement `GetUsage`: exec `df` and parse output
    - _Requirements: 1.11, 4.4_

  - [ ]* 4.5 Write unit tests for storage abstraction layer
    - Create `internal/storage/disk_test.go`, `raid_test.go`, `lvm_test.go`, `fs_test.go`
    - Test command construction and output parsing with mocked exec
    - Test error handling for failed commands
    - _Requirements: 5.1, 5.4_

- [ ] 5. Metadata store implementation
  - [ ] 5.1 Implement JSON metadata store
    - Create `internal/metadata/json_store.go`
    - Implement `SavePool`: marshal to JSON, write to temp file, fsync, rename (atomic write)
    - Implement `LoadPool`: read JSON file, unmarshal, look up pool by ID
    - Implement `ListPools`: read JSON file, return summaries of all pools
    - Handle first-run case (no metadata file exists → create empty structure)
    - Use schema version 1 with `version` field
    - Storage path: `/var/lib/poolforge/metadata.json` (configurable for testing)
    - _Requirements: 1.14, 3.5_

  - [ ]* 5.2 Write property test for metadata round-trip persistence (P13)
    - **Property 13: Metadata store round-trip persistence**
    - Generate random valid pool configurations, save then load, verify all fields preserved
    - Use temp directory for test isolation
    - **Validates: Requirements 3.5, 1.14**

  - [ ]* 5.3 Write unit tests for metadata store
    - Create `internal/metadata/metadata_test.go`
    - Test save/load specific pool configurations
    - Test first-run (missing file) behavior
    - Test corrupted JSON handling
    - Test atomic write (verify temp file + rename pattern)
    - _Requirements: 5.1, 5.4_

- [ ] 6. Checkpoint — Verify storage and metadata layers
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 7. Core engine implementation
  - [ ] 7.1 Implement `CreatePool` orchestration
    - Create `internal/engine/engine_impl.go` with the concrete `EngineService` implementation
    - Wire the full creation flow: validate inputs (≥2 disks, no conflicts, unique name) → compute tiers → partition disks → create RAID arrays → create PVs → create VG → create LV → create ext4 → save metadata
    - Generate pool ID (UUID), VG name (`vg_poolforge_<id>`), LV name (`lv_poolforge_<id>`), mount point (`/mnt/poolforge/<name>`)
    - Return error for fewer than 2 disks (Req 1.12), disk already in pool (Req 1.13), duplicate name (Req 1.14)
    - _Requirements: 1.1–1.14_

  - [ ] 7.2 Implement `GetPool`, `ListPools`, `GetPoolStatus`
    - `GetPool`: load pool from metadata store by ID
    - `ListPools`: load all pools, return summaries with name, state, capacity, used, disk count
    - `GetPoolStatus`: load pool, query RAID array details and filesystem usage, build hierarchical status response (pool state, array statuses, disk statuses)
    - _Requirements: 2.1, 2.4, 3.1, 3.2, 3.3, 3.4_

  - [ ] 7.3 Implement boot reassembly logic
    - Create `internal/engine/reassemble.go`
    - Load all pools from metadata store
    - For each pool: assemble RAID arrays via `RAIDManager.AssembleArray`, activate VG, mount filesystem
    - _Requirements: 3.6_

  - [ ]* 7.4 Write property test for pool creation structural invariant (P5)
    - **Property 5: Pool creation produces exactly one VG, one LV, and one ext4 filesystem**
    - Generate random valid disk sets, create pool (with mocked storage), verify exactly one VG, one LV, ext4 on LV
    - **Validates: Requirements 1.10, 1.11**

  - [ ]* 7.5 Write property test for disk membership exclusivity (P6)
    - **Property 6: Disk membership exclusivity across pools**
    - Generate two pool creation requests sharing at least one disk, verify second request rejected with error identifying conflicting disk and owning pool
    - **Validates: Requirements 1.13, 2.3**

  - [ ]* 7.6 Write property test for pool isolation (P7)
    - **Property 7: Pool isolation — disjoint resources**
    - Generate two pools on disjoint disk sets, verify VGs, LVs, RAID arrays, and member disks are completely disjoint
    - **Validates: Requirements 2.2, 1.14**

  - [ ]* 7.7 Write property test for pool status hierarchy (P10)
    - **Property 10: Pool status contains complete hierarchy information**
    - Generate random pool, query status, verify response contains pool state, total/used capacity, array details (level, tier, state, capacity, members), disk details (descriptor, state, contributing arrays)
    - **Validates: Requirements 3.1, 3.2, 3.3, 3.4**

  - [ ]* 7.8 Write property test for pool list fields (P37)
    - **Property 37: Pool list contains required fields**
    - Generate random set of pools, list them, verify each entry has name, state, total capacity, used capacity, disk count
    - **Validates: Requirements 2.4**

  - [ ]* 7.9 Write unit tests for engine
    - Create `internal/engine/engine_test.go`
    - Test specific example: 3 disks of 1 TB/2 TB/4 TB → expected tiers, RAID levels, slice counts
    - Test minimum disk count rejection (< 2 disks)
    - Test all-same-size disks (single tier, uniform pool)
    - Test parity2 with exactly 2 disks → RAID 1
    - Test disk conflict detection
    - Test pool name uniqueness
    - Test pool not found error
    - _Requirements: 5.1, 5.2, 5.4_

- [ ] 8. Checkpoint — Verify core engine
  - Ensure all tests pass, ask the user if questions arise.


- [ ] 9. CLI implementation
  - [ ] 9.1 Implement CLI framework and `pool create` command
    - Update `cmd/poolforge/main.go` with CLI argument parsing (use `cobra` or stdlib `flag`)
    - Implement `pool create --disks /dev/sdb,/dev/sdc --parity parity1 --name mypool`
    - Parse comma-separated disk list, validate parity mode flag (parity1/parity2)
    - Call `EngineService.CreatePool` and display pool summary (name, parity, tier count, capacity)
    - _Requirements: 1.1, 4.1_

  - [ ] 9.2 Implement `pool status` command
    - Implement `pool status <pool-name>`
    - Look up pool by name (resolve name → ID via metadata)
    - Call `EngineService.GetPoolStatus` and display hierarchical output: pool state, arrays (level, tier, state, members), disks (state, arrays)
    - _Requirements: 3.1, 3.2, 3.3_

  - [ ] 9.3 Implement `pool list` command
    - Implement `pool list`
    - Call `EngineService.ListPools` and display table: name, state, capacity, used, disk count
    - _Requirements: 2.4_

- [ ] 10. Checkpoint — Verify CLI wiring
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 11. Test infrastructure — Terraform IaC
  - [ ] 11.1 Create Terraform configuration
    - Create `test/infra/main.tf`: EC2 instance (t3.medium, Ubuntu 24.04 AMI), security group (SSH ingress), key pair
    - Create 6 EBS volumes (gp3): 1 GB, 2 GB, 3 GB, 4 GB, 5 GB, 10 GB with volume attachments
    - Tag all resources with `poolforge-test-{run-id}` for orphan identification
    - _Requirements: 6.1, 6.4, 6.11_

  - [ ] 11.2 Create Terraform variables and outputs
    - Create `test/infra/variables.tf`: instance type, volume sizes, region, run ID
    - Create `test/infra/outputs.tf`: instance public IP, SSH key path, EBS device mappings
    - _Requirements: 6.1, 6.2_

  - [ ] 11.3 Create EC2 setup script
    - Create `test/infra/scripts/setup.sh`
    - Install mdadm, lvm2, and ext4 utilities
    - Copy and install PoolForge binary
    - _Requirements: 6.1, 4.3_

- [ ] 12. Test infrastructure — Test_Runner
  - [ ] 12.1 Implement Test_Runner script
    - Create `test/infra/test_runner.sh`
    - Implement lifecycle: `terraform init` → `terraform apply -auto-approve` → SCP binary → SSH setup → SSH run tests → SCP results → `terraform destroy -auto-approve`
    - Use `trap EXIT` to guarantee teardown runs regardless of test outcome
    - Return non-zero exit code on any test failure
    - Log orphaned resource IDs if teardown fails
    - _Requirements: 6.7, 6.8, 6.9, 6.10_

  - [ ]* 12.2 Write tests for Test_Runner teardown guarantee (P35)
    - **Property 35: Test_Runner teardown executes regardless of test outcome**
    - Verify that the trap-based teardown mechanism fires on both success and failure paths
    - **Validates: Requirements 6.9**

  - [ ]* 12.3 Write tests for Test_Runner exit code (P36)
    - **Property 36: Test_Runner exit code reflects test results**
    - Verify non-zero exit code when any test scenario fails
    - **Validates: Requirements 6.8**

- [ ] 13. Integration tests
  - [ ] 13.1 Implement pool creation integration test
    - Create `test/integration/pool_create_test.go`
    - Test pool creation with 4+ mixed-size EBS volumes
    - Verify tier computation, RAID arrays created, VG/LV/ext4 present
    - _Requirements: 5.5, 6.5_

  - [ ] 13.2 Implement multi-pool isolation integration test
    - Create `test/integration/multi_pool_test.go`
    - Create two pools on disjoint EBS volume sets
    - Verify no shared VGs, LVs, arrays, or disks
    - _Requirements: 2.1, 2.2, 6.5_

  - [ ] 13.3 Implement pool status integration test
    - Create `test/integration/pool_status_test.go`
    - Create pool, query status, verify hierarchical output matches actual system state
    - _Requirements: 3.1, 3.2, 3.3_

  - [ ] 13.4 Implement full lifecycle integration test
    - Create `test/integration/lifecycle_test.go`
    - Create pool → write data to ext4 → read data back → verify integrity → verify status
    - Test boot reassembly: save metadata, stop arrays, reassemble from metadata, verify data intact
    - _Requirements: 5.5, 6.6, 3.6_

- [ ] 14. Final checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests use the `rapid` library (`github.com/flyingmutant/rapid`) with 100+ iterations each
- Unit tests use mocked storage interfaces; integration tests run against real EBS volumes via Test_Runner
- Storage abstraction implementations exec system commands (sgdisk, mdadm, pvcreate, etc.) — they require root privileges at runtime
- Integration tests (task 13) are designed to run inside the Test_Environment provisioned by task 11/12
