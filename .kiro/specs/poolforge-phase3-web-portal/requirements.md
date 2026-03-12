# Requirements Document — Phase 3: Web Portal, API Server, and Visualization

## Introduction

This document specifies the requirements for Phase 3 of the PoolForge project. PoolForge is an open-source storage management tool for Ubuntu LTS (24.04+) that replicates hybrid RAID functionality using mdadm and LVM. The full project scope is defined in the master spec at `.kiro/specs/hybrid-raid-manager/`. Phase 1 is defined at `.kiro/specs/poolforge-phase1-core-engine/`. Phase 2 is defined at `.kiro/specs/poolforge-phase2-lifecycle/`.

Phase 1 established the core engine: capacity-tier computation from mixed-size disks, GPT disk partitioning, mdadm RAID array creation, LVM stitching (PV → VG → LV), ext4 filesystem creation, a CLI for pool creation/status/list, a JSON-based metadata store with atomic writes, and the automated cloud-based test infrastructure (Terraform IaC for EC2 + EBS, Test_Runner script). Phase 1 defined and implemented the core interfaces: EngineService (CreatePool, GetPool, ListPools, GetPoolStatus), StorageAbstraction (DiskManager, RAIDManager, LVMManager, FilesystemManager), and MetadataStore (SavePool, LoadPool, ListPools).

Phase 2 built on those interfaces to deliver lifecycle operations: adding disks to existing pools (with array reshape and filesystem expansion), replacing failed disks, removing disks, deleting pools, disk failure detection with automatic self-healing rebuilds, unallocated capacity detection with administrator-approved expansion, pool configuration serialization (JSON export/import), and the HealthMonitor background service. Phase 2 extended EngineService with AddDisk, ReplaceDisk, RemoveDisk, DeletePool, HandleDiskFailure, GetRebuildProgress, ExportPool, ImportPool, DetectUnallocated, and ExpandPool.

Phase 3 layers the web interface on top of the existing EngineService without modifying it. The API_Server is a thin HTTP wrapper around the same EngineService interface that the CLI uses. All pool management operations available through the CLI are exposed as REST API endpoints, and the React Web_Portal consumes those endpoints to provide a browser-based management experience. Phase 3 also introduces the Storage_Map visualization with interactive topology, Health_Color coding, Detail_Panels, rebuild progress bars, an integrated Log_Viewer with filtering and live tail via WebSocket, local username/password authentication with session tokens, and a `poolforge serve` CLI command to start the API server.

This is Phase 3 of 4:

| Phase | Scope |
|-------|-------|
| Phase 1 ✅ | Core engine, CLI (create/status/list), metadata store, test infrastructure |
| Phase 2 ✅ | Lifecycle operations (add disk, replace disk, remove disk, delete pool, self-healing/rebuild, expansion, export/import) |
| **Phase 3** | **Web portal (React), API server (Go REST), Storage_Map, Log_Viewer, authentication** |
| Phase 4 | Safety hardening (atomic operations, rollback, multi-interface, SMART monitoring) |

Phase 3 introduces the following new components:
- **API_Server** (`internal/api`): Go HTTP server exposing REST endpoints for all EngineService operations, serving the React SPA as static assets, and providing WebSocket support for live log streaming. Listens on HTTPS with a configurable port (default 8443).
- **Web_Portal** (`web/`): React SPA with Dashboard, Storage_Map visualization, Detail_Panels, Log_Viewer, authentication UI, pool management forms, and configuration pages.
- **EventLogger** (`internal/logger`): Structured logging with component tagging, severity levels, query/filter support, and streaming for WebSocket live tail.
- **Authentication**: Local username/password authentication with bcrypt-hashed passwords, per-user salts, and session tokens.

Phase 3 MUST NOT break any Phase 1 or Phase 2 functionality. All existing CLI commands (pool create, pool status, pool list, pool add-disk, pool replace-disk, pool remove-disk, pool delete, pool expand, pool export, pool import), metadata persistence, self-healing, and test infrastructure continue to work unchanged.

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
- **Metadata_Store**: A persistent record of Pool configuration, disk membership, Capacity_Tiers, and RAID_Array mappings
- **Rebuild**: The process of reconstructing redundancy in a degraded RAID_Array after a disk failure
- **Reshape**: The process of modifying an existing RAID_Array geometry to accommodate a new member disk
- **Web_Portal**: A browser-based management interface served by PoolForge for configuring and monitoring Pools, built with React
- **API_Server**: The HTTP backend process (Go) that serves the Web_Portal static assets and exposes a REST API for Pool management operations. Listens on HTTPS with a configurable port (default 8443)
- **Session**: An authenticated user interaction with the Web_Portal, identified by a session token with an expiration time
- **Session_Token**: A cryptographically random string issued upon successful authentication, used to authorize subsequent API requests
- **Storage_Map**: A visual topology diagram in the Web_Portal that renders Pools as containers, RAID_Arrays as blocks within those containers, and disks as individual icons within each RAID_Array block
- **Health_Color**: A color code applied to visual elements in the Storage_Map — green for healthy, amber for degraded or rebuilding, red for failed
- **Detail_Panel**: A contextual information panel in the Web_Portal that opens when the administrator clicks a Pool, RAID_Array, or disk element in the Storage_Map
- **Log_Entry**: A single timestamped record in the PoolForge event log, containing a severity level, a source component identifier, and a message
- **Log_Level**: The severity classification of a Log_Entry — one of debug, info, warning, or error
- **Log_Viewer**: A component of the Web_Portal that displays, filters, and exports Log_Entries
- **Live_Tail**: A real-time streaming mode in the Log_Viewer that appends new Log_Entries to the display as they are generated, delivered via WebSocket
- **EventLogger**: The structured logging component that records Log_Entries with component tagging, supports query/filter operations, and provides a streaming channel for Live_Tail
- **Confirmation_Dialog**: A modal dialog in the Web_Portal that requires explicit administrator approval before executing a destructive operation (delete pool, remove disk)
- **Notification_Banner**: A persistent visual alert displayed at the top of the Web_Portal when a disk failure is detected, identifying the affected disk and Pool
- **Dashboard**: The main page of the Web_Portal displaying all Pools as summary cards with name, state, capacity, and used capacity
- **EngineService**: The core operations interface established in Phase 1 and extended in Phase 2, which the API_Server wraps as REST endpoints without modification

## Requirements

### Requirement 1: API Server

**User Story:** As a system administrator, I want a REST API server that exposes all PoolForge operations over HTTPS, so that I can manage storage pools from the web portal and integrate with automation tools.

#### Acceptance Criteria

1. THE API_Server SHALL serve HTTPS on a configurable port with a default of 8443.
2. THE API_Server SHALL expose REST API endpoints that map to all EngineService operations: CreatePool, DeletePool, GetPool, ListPools, AddDisk, ReplaceDisk, RemoveDisk, GetPoolStatus, GetArrayStatus, GetDiskStatus, GetRebuildProgress, ExportPool, ImportPool, DetectUnallocated, and ExpandPool.
3. THE API_Server SHALL serve the Web_Portal React SPA as static assets from the same HTTPS server.
4. THE API_Server SHALL require a valid Session_Token for all API endpoints that perform Pool management operations or retrieve Pool data.
5. THE API_Server SHALL expose REST API endpoints for authentication: login (create session), logout (invalidate session).
6. THE API_Server SHALL expose REST API endpoints for user account management: create user, list users.
7. THE API_Server SHALL expose REST API endpoints for Log_Entry queries with support for filtering by Log_Level, time range, source component, and keyword search.
8. THE API_Server SHALL expose a WebSocket endpoint for Live_Tail log streaming.
9. WHEN the API_Server receives a request with an expired or invalid Session_Token, THE API_Server SHALL reject the request with an HTTP 401 status and include a response body indicating the authentication error.
10. WHEN the API_Server receives a request to a protected endpoint without a Session_Token, THE API_Server SHALL reject the request with an HTTP 401 status.
11. THE API_Server SHALL return appropriate HTTP status codes for all responses: 200 for successful reads, 201 for successful creates, 204 for successful deletes, 400 for invalid requests, 401 for authentication errors, 404 for not-found resources, and 500 for internal errors.
12. THE API_Server SHALL return JSON response bodies for all API endpoints, using the response models defined in the master design document.

### Requirement 2: Web Portal Dashboard and Navigation

**User Story:** As a system administrator, I want a browser-based dashboard that shows all my storage pools at a glance, so that I can quickly assess the health of my storage infrastructure.

#### Acceptance Criteria

1. THE Web_Portal SHALL display a Dashboard listing all Pools managed by PoolForge, showing each Pool name, overall state (healthy, degraded, or failed), total capacity, and used capacity.
2. THE Web_Portal SHALL apply Health_Color coding to each Pool summary card on the Dashboard: green for healthy Pools, amber for degraded Pools, and red for failed Pools.
3. THE Web_Portal SHALL provide navigation from the Dashboard to the Pool detail view when the administrator clicks a Pool summary card.
4. THE Web_Portal SHALL display a navigation bar with links to the Dashboard, Log_Viewer, and Configuration pages.
5. WHEN a disk failure is detected, THE Web_Portal SHALL display a Notification_Banner at the top of the page identifying the affected Disk_Descriptor and the Pool containing the failed disk.
6. THE Web_Portal SHALL dismiss the Notification_Banner when the administrator explicitly closes the banner or when the affected disk is replaced and all Rebuilds complete.

### Requirement 3: Storage Map Visualization

**User Story:** As a system administrator, I want an interactive visual topology map of my storage hierarchy, so that I can see at a glance which pools, RAID arrays, and disks are healthy, degraded, or failed, and drill into any component for full details.

#### Acceptance Criteria

1. THE Web_Portal SHALL render a Storage_Map for each Pool that displays the Pool as a visual container, each RAID_Array within the Pool as a distinct block or card inside that container, and each disk within a RAID_Array as an individual icon or block inside the corresponding RAID_Array card.
2. THE Web_Portal SHALL apply Health_Color coding to every disk icon in the Storage_Map: green for healthy disks, amber for degraded or rebuilding disks, and red for failed disks.
3. THE Web_Portal SHALL apply Health_Color coding to every RAID_Array card in the Storage_Map: green for healthy arrays, amber for degraded or rebuilding arrays, and red for failed arrays.
4. THE Web_Portal SHALL apply Health_Color coding to every Pool container in the Storage_Map: green for healthy Pools, amber for degraded Pools, and red for failed Pools.
5. WHEN a RAID_Array is actively rebuilding, THE Web_Portal SHALL display a progress bar on the affected RAID_Array card in the Storage_Map showing the rebuild progress as a percentage and estimated time remaining.
6. WHEN a disk is actively being rebuilt onto, THE Web_Portal SHALL display a progress bar on the affected disk icon in the Storage_Map showing the rebuild progress as a percentage.
7. WHEN a disk is in a failed state, THE Web_Portal SHALL visually highlight that disk icon in red and apply amber highlighting to all RAID_Array cards affected by the failure in the Storage_Map.
8. THE Web_Portal SHALL provide a navigation path from Pool to RAID_Arrays to individual disks, enabling the administrator to drill down through the hierarchy to locate the source of a problem.

### Requirement 4: Detail Panels

**User Story:** As a system administrator, I want to click on any pool, RAID array, or disk in the visual map and see detailed information, so that I can diagnose issues and understand the configuration of each component.

#### Acceptance Criteria

1. WHEN the administrator clicks a Pool container in the Storage_Map, THE Web_Portal SHALL open a Detail_Panel displaying the Pool name, overall state, Parity_Mode, total capacity, used capacity, and a list of all member RAID_Arrays with their states.
2. WHEN the administrator clicks a RAID_Array card in the Storage_Map, THE Web_Portal SHALL open a Detail_Panel displaying the sync state (clean, active, resyncing, recovering, or degraded), the RAID level, the Capacity_Tier, the array capacity, and the member Disk_Descriptors with per-disk state.
3. WHEN the administrator clicks a disk icon in the Storage_Map, THE Web_Portal SHALL open a Detail_Panel displaying the Disk_Descriptor, the disk health (healthy, degraded, or failed), the raw capacity, and a list of all RAID_Arrays to which the disk contributes Slices.
4. WHEN the administrator views a Detail_Panel for a specific Pool, RAID_Array, or disk, THE Web_Portal SHALL display a contextual log section within the Detail_Panel showing recent Log_Entries filtered to that component.

### Requirement 5: Log Viewer

**User Story:** As a system administrator, I want to view, filter, search, and export PoolForge logs directly in the Web_Portal, so that I can diagnose issues and audit system events without accessing the server CLI or external log tools.

#### Acceptance Criteria

1. THE Web_Portal SHALL provide a Log_Viewer page that displays Log_Entries in reverse chronological order, showing for each entry: the timestamp, the Log_Level, the source component identifier (Pool name, RAID_Array identifier, or Disk_Descriptor), and the message.
2. THE Log_Viewer SHALL allow the administrator to filter displayed Log_Entries by one or more Log_Levels (debug, info, warning, or error).
3. THE Log_Viewer SHALL allow the administrator to filter displayed Log_Entries by a time range specified as a start timestamp and an end timestamp.
4. THE Log_Viewer SHALL allow the administrator to filter displayed Log_Entries by a specific source component: a named Pool, a specific RAID_Array, or a specific Disk_Descriptor.
5. THE Log_Viewer SHALL allow the administrator to search displayed Log_Entries by a keyword substring match against the message field.
6. WHEN multiple filters are active simultaneously, THE Log_Viewer SHALL apply all filters as a logical AND, displaying only Log_Entries that satisfy every active filter.
7. THE Log_Viewer SHALL support a Live_Tail mode that streams new Log_Entries to the display in real time via WebSocket as they are generated, without requiring the administrator to refresh the page.
8. WHEN Live_Tail mode is active and filters are applied, THE Log_Viewer SHALL apply the active filters to incoming Log_Entries and display only those that match.
9. THE Log_Viewer SHALL allow the administrator to export the currently displayed (filtered) Log_Entries as a downloadable file.

### Requirement 6: Authentication

**User Story:** As a system administrator, I want the web portal to require login credentials, so that only authorized users can manage storage pools.

#### Acceptance Criteria

1. THE API_Server SHALL authenticate administrators using local username and password credentials stored on the PoolForge system.
2. WHEN an administrator submits valid credentials, THE API_Server SHALL create a Session and return a Session_Token to the Web_Portal.
3. WHEN an administrator submits invalid credentials, THE API_Server SHALL reject the login attempt with an error message and log the failed attempt as a warning-level Log_Entry.
4. THE API_Server SHALL require a valid Session_Token for all API endpoints that perform Pool management operations or retrieve Pool data.
5. IF a request is made with an expired or invalid Session_Token, THEN THE API_Server SHALL reject the request with an authentication error and the Web_Portal SHALL redirect to the login page.
6. THE API_Server SHALL store password credentials using a secure one-way hash (bcrypt) with a per-user salt.
7. THE API_Server SHALL provide API endpoints for creating and listing local user accounts.
8. THE PoolForge CLI SHALL provide a `user create --username <name> --password <password>` command for creating user accounts.
9. THE PoolForge CLI SHALL provide a `user list` command for listing user accounts.
10. THE Web_Portal SHALL display a login page that collects username and password, submits credentials to the API_Server login endpoint, and stores the returned Session_Token for subsequent requests.
11. THE Web_Portal SHALL provide a logout action that invalidates the current Session and redirects to the login page.

### Requirement 7: Pool Management Operations via Web Portal

**User Story:** As a system administrator, I want to perform all pool management operations through the web portal, so that I can manage storage without using the command line.

#### Acceptance Criteria

1. THE Web_Portal SHALL provide a pool creation form that collects the pool name, Parity_Mode selection, and Disk_Descriptor selection, and submits the request to the API_Server CreatePool endpoint.
2. THE Web_Portal SHALL provide an add-disk form accessible from the Pool Detail_Panel that collects a Disk_Descriptor and submits the request to the API_Server AddDisk endpoint.
3. THE Web_Portal SHALL provide a replace-disk form accessible from the Pool Detail_Panel that collects the failed Disk_Descriptor and the replacement Disk_Descriptor, and submits the request to the API_Server ReplaceDisk endpoint.
4. THE Web_Portal SHALL provide a remove-disk action accessible from the Pool Detail_Panel that collects the Disk_Descriptor to remove and submits the request to the API_Server RemoveDisk endpoint.
5. THE Web_Portal SHALL provide an expand action accessible from the Pool Detail_Panel when Unallocated_Capacity is detected, submitting the request to the API_Server ExpandPool endpoint.
6. THE Web_Portal SHALL provide a delete action accessible from the Pool Detail_Panel that submits the request to the API_Server DeletePool endpoint.
7. THE Web_Portal SHALL provide export and import actions for Pool_Configuration serialization, accessible from the Pool Detail_Panel.
8. WHEN the administrator initiates a destructive operation (delete pool, remove disk), THE Web_Portal SHALL display a Confirmation_Dialog requiring explicit approval before submitting the request to the API_Server.
9. WHEN the administrator initiates a remove-disk operation that requires a RAID level Downgrade, THE Web_Portal SHALL display the proposed RAID level changes in the Confirmation_Dialog and require explicit approval.
10. WHEN any pool management operation fails, THE Web_Portal SHALL display the error message returned by the API_Server to the administrator.

### Requirement 8: Configuration Page

**User Story:** As a system administrator, I want a configuration page in the web portal, so that I can view and modify PoolForge settings without editing files on the server.

#### Acceptance Criteria

1. THE Web_Portal SHALL provide a Configuration page accessible from the navigation bar.
2. THE Configuration page SHALL display the current API_Server HTTPS port setting.
3. THE Configuration page SHALL allow the administrator to view and modify configurable PoolForge settings through the Web_Portal.

### Requirement 9: CLI Serve Command

**User Story:** As a system administrator, I want a CLI command to start the API server, so that I can launch the web portal from the command line.

#### Acceptance Criteria

1. THE PoolForge CLI SHALL provide a `serve` command that starts the API_Server.
2. THE `serve` command SHALL accept a `--port <port>` flag to configure the HTTPS listening port, with a default of 8443.
3. WHEN the `serve` command starts the API_Server, THE API_Server SHALL begin serving the Web_Portal static assets and REST API endpoints on the configured HTTPS port.
4. WHEN the `serve` command encounters a startup error (e.g., port already in use, TLS certificate error), THE CLI SHALL display a descriptive error message and exit with a non-zero exit code.

### Requirement 10: Event Logger

**User Story:** As a developer, I want a structured event logging system, so that the API server and web portal can record, query, filter, and stream log entries for all PoolForge operations.

#### Acceptance Criteria

1. THE EventLogger SHALL record Log_Entries with the following fields: timestamp, Log_Level (debug, info, warning, or error), source component identifier, and message.
2. THE EventLogger SHALL support querying Log_Entries with filters for Log_Level, time range, source component, and keyword substring match.
3. THE EventLogger SHALL support streaming new Log_Entries to subscribers in real time for Live_Tail functionality.
4. THE EventLogger SHALL persist Log_Entries to a log file in newline-delimited JSON (NDJSON) format at `/var/log/poolforge/events.log`.
5. WHEN multiple filters are applied to a query, THE EventLogger SHALL apply all filters as a logical AND, returning only Log_Entries that satisfy every active filter.
6. THE EventLogger SHALL support exporting filtered Log_Entries as a downloadable file.

### Requirement 11: Testing (Phase 3 Scope)

**User Story:** As a developer, I want a comprehensive test suite for Phase 3 web portal and API functionality, so that I can verify new functionality and confirm no regressions in Phase 1 and Phase 2.

#### Acceptance Criteria

1. WHEN Phase 3 is completed, THE test suite SHALL include API endpoint tests that validate all REST endpoints introduced in Phase 3: authentication (login, logout, user management), pool CRUD operations, disk operations, log queries, and WebSocket live tail.
2. WHEN Phase 3 is completed, THE test suite SHALL include UI component tests for the Storage_Map, Detail_Panels, Log_Viewer, Dashboard, Confirmation_Dialogs, Notification_Banners, and authentication flows.
3. WHEN Phase 3 is completed, THE test suite SHALL include end-to-end web workflow tests that validate complete user workflows through the Web_Portal: login, view dashboard, create pool, view Storage_Map, drill into Detail_Panels, add disk, replace disk, remove disk, delete pool, view and filter logs, and export logs.
4. WHEN Phase 3 is completed, THE test suite SHALL include regression tests that confirm all Phase 1 functionality (pool creation, status, list, metadata persistence, tier computation) and all Phase 2 functionality (add-disk, replace-disk, remove-disk, delete-pool, self-healing rebuild, expansion, export/import) remain correct.
5. THE API endpoint tests SHALL validate correct HTTP status codes, JSON response structures, authentication enforcement, and error handling for all endpoints.
6. THE UI component tests SHALL validate Health_Color coding, rebuild progress bar rendering, Detail_Panel content completeness, and Confirmation_Dialog behavior.
7. THE end-to-end tests SHALL validate that pool management operations performed through the Web_Portal produce the same results as the equivalent CLI commands.
8. THE test suite SHALL include property-based tests for log filter composition (logical AND of multiple filters) and Health_Color mapping determinism.

## Extensibility Notes

The following interfaces are introduced or extended in Phase 3:

- **API_Server** (`internal/api`): New in Phase 3. Wraps EngineService as REST endpoints. Phase 4 will add SMART-related endpoints (GET /api/smart/:disk, PUT /api/smart/thresholds, GET /api/smart/:disk/history) and configuration endpoints for SMART thresholds.
- **EventLogger** (`internal/logger`): New in Phase 3. Provides structured logging, query/filter, and streaming. Phase 4 will integrate SMART_Events into the same logging pipeline.
- **Authentication**: New in Phase 3. Local username/password with session tokens. Future phases could extend to support additional authentication methods.
- **Web_Portal** (`web/`): New in Phase 3. Phase 4 will add SMART health indicators on disk icons, SMART data display in disk Detail_Panels, and SMART threshold configuration in the Configuration page.
- **EngineService**: No modifications in Phase 3. The API_Server consumes the existing interface as-is.
- **MetadataStore**: Phase 3 adds user account storage (username, password_hash, salt, created_at) to the metadata schema. Schema remains version 1 with backward-compatible additions.
