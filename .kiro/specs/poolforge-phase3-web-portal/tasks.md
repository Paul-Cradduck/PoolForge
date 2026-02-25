# Implementation Plan: Phase 3 — Web Portal, API Server, and Visualization

## Overview

Phase 3 layers the web management interface on top of the existing PoolForge engine. Implementation proceeds bottom-up: EventLogger and Auth module first (no external dependencies), then the API Server wrapping EngineService, then the React Web Portal consuming the API. Each task builds on the previous, and property-based tests are placed close to the code they validate. Phase 3 MUST NOT modify EngineService or break any Phase 1/Phase 2 functionality.

## Tasks

- [ ] 1. Implement EventLogger (`internal/logger`)
  - [ ] 1.1 Create EventLogger interface and types
    - Define `EventLogger` interface, `LogEntry` struct, `LogLevel` constants, and `LogFilter` struct in `internal/logger/logger.go`
    - Implement the NDJSON file-backed logger: `Log()` appends JSON lines to `/var/log/poolforge/events.log` with `O_APPEND`
    - Implement `Query()` that reads the NDJSON file, parses entries, applies filters (level, time range, source, keyword) as logical AND, returns results in reverse chronological order
    - Implement `Export()` that returns filtered entries as an `io.Reader` in NDJSON format
    - Implement `Stream()` that returns a channel of new log entries matching a filter, with subscriber tracking and non-blocking sends
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6_

  - [ ]* 1.2 Write property test P28: Log entries in reverse chronological order
    - **Property 28: Log entries are returned in reverse chronological order**
    - Use `rapid` to generate random sets of LogEntry with varying timestamps, log them, query with empty filter, assert descending timestamp order
    - **Validates: Requirements 5.1**

  - [ ]* 1.3 Write property test P29: Log filter composition is logical AND
    - **Property 29: Log filter composition is logical AND**
    - Use `rapid` to generate random LogEntry sets and random LogFilter combinations, verify that applying all filters simultaneously equals the intersection of applying each filter individually
    - **Validates: Requirements 5.6, 5.8, 10.5**

  - [ ]* 1.4 Write property test P30: Log export matches filtered display
    - **Property 30: Log export matches filtered display**
    - Use `rapid` to generate random entries and filters, verify `Export()` output contains exactly the same entries as `Query()` with the same filter, in the same order
    - **Validates: Requirements 5.9, 10.6**

  - [ ]* 1.5 Write property test P56: EventLogger NDJSON persistence round-trip
    - **Property 56: EventLogger NDJSON persistence round-trip**
    - Use `rapid` to generate random valid LogEntry values, log them via `Log()`, then query with a matching filter, assert all fields (timestamp, level, source, message) are preserved
    - **Validates: Requirements 10.1, 10.4**

  - [ ]* 1.6 Write unit tests for EventLogger
    - Test `Log()` writes valid NDJSON line to file
    - Test `Query()` with each individual filter type (level, time range, source, keyword)
    - Test `Query()` on empty log file returns empty slice
    - Test `Stream()` delivers new entries to subscriber and applies filter
    - Test `Stream()` context cancellation closes channel and removes subscriber
    - Test `Export()` returns NDJSON reader with correct content
    - Test concurrent `Log()` calls produce valid NDJSON
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6_

- [ ] 2. Implement Authentication Module (`internal/auth`)
  - [ ] 2.1 Create AuthService interface and types
    - Define `AuthService` interface, `User`, `UserSummary`, `Session` structs in `internal/auth/auth.go`
    - Implement `CreateUser()`: generate 16-byte random salt, prepend salt to password, hash with bcrypt cost 12, store in metadata under `users` section
    - Implement `ListUsers()`: return all users as `UserSummary` (no password hashes)
    - Implement `Login()`: verify credentials against stored bcrypt hash, create session with 32-byte random token (hex-encoded), 24-hour expiry, store in `sync.Map`
    - Implement `Logout()`: remove session from `sync.Map`
    - Implement `ValidateSession()`: check token exists and `ExpiresAt > now()`
    - Extend MetadataStore schema to include `users` section (backward-compatible, missing section defaults to empty map)
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.7_

  - [ ]* 2.2 Write property test P25: Valid credentials produce session token
    - **Property 25: Valid credentials produce session token**
    - Use `rapid` to generate random valid username/password pairs, create user, login, assert token is non-empty string and expiration is in the future, assert token is usable for `ValidateSession()`
    - **Validates: Requirements 6.2**

  - [ ]* 2.3 Write property test P26: Invalid credentials are rejected
    - **Property 26: Invalid credentials are rejected**
    - Use `rapid` to generate random username/password pairs, create user with one password, attempt login with a different password, assert error returned; also test nonexistent username
    - **Validates: Requirements 6.3**

  - [ ]* 2.4 Write property test P27: Passwords are stored as salted hashes
    - **Property 27: Passwords are stored as salted hashes**
    - Use `rapid` to generate random passwords, create two users with the same password, assert stored hashes differ, assert stored hash does not equal plaintext password
    - **Validates: Requirements 6.6**

  - [ ]* 2.5 Write unit tests for AuthService
    - Test `CreateUser()` with valid input creates user with bcrypt hash
    - Test `CreateUser()` with duplicate username returns error
    - Test `CreateUser()` with empty username or password returns error
    - Test `Login()` with valid credentials returns session token
    - Test `Login()` with wrong password returns error
    - Test `Login()` with unknown user returns error
    - Test `ValidateSession()` with valid token returns OK
    - Test `ValidateSession()` with expired token returns error
    - Test `ValidateSession()` with unknown token returns error
    - Test `Logout()` invalidates session, subsequent `ValidateSession()` fails
    - Test bcrypt cost factor is 12
    - Test salt uniqueness across users
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 6.7_

- [ ] 3. Checkpoint — EventLogger and Auth
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 4. Implement API Server (`internal/api`)
  - [ ] 4.1 Create API Server structure and middleware
    - Define `Server` struct, `ServerOptions`, and `NewServer()` constructor in `internal/api/server.go`
    - Implement middleware chain: request logger (logs to EventLogger), CORS (same-origin), session middleware (validates `session_token` cookie on `/api/*` except `/api/auth/login`, returns 401 on invalid/expired/missing token)
    - Implement `Start()` method that configures TLS, registers routes, and starts HTTPS listener
    - Implement `writeJSON()` and `writeError()` helper methods for consistent JSON responses
    - _Requirements: 1.1, 1.4, 1.9, 1.10, 1.11, 1.12_

  - [ ] 4.2 Implement authentication API handlers
    - `POST /api/auth/login`: parse credentials, call `AuthService.Login()`, set session cookie (HttpOnly, Secure, SameSite=Strict), return token and expiry
    - `POST /api/auth/logout`: call `AuthService.Logout()`, clear session cookie, return 204
    - `POST /api/auth/users`: parse username/password, call `AuthService.CreateUser()`, return 201 with user summary
    - `GET /api/auth/users`: call `AuthService.ListUsers()`, return 200 with user list
    - _Requirements: 1.5, 1.6, 6.2, 6.3, 6.7_

  - [ ] 4.3 Implement pool management API handlers
    - `GET /api/pools`: call `EngineService.ListPools()`, return pool summaries
    - `POST /api/pools`: parse create request, call `EngineService.CreatePool()`, return 201
    - `GET /api/pools/:id`: call `EngineService.GetPool()`, return pool detail
    - `DELETE /api/pools/:id`: call `EngineService.DeletePool()`, return 204
    - `GET /api/pools/:id/status`: call `EngineService.GetPoolStatus()`, return status
    - `POST /api/pools/:id/disks`: parse disk, call `EngineService.AddDisk()`, return 200
    - `DELETE /api/pools/:id/disks/:dev`: call `EngineService.RemoveDisk()`, return 204
    - `POST /api/pools/:id/replace-disk`: parse old/new disk, call `EngineService.ReplaceDisk()`, return 200
    - `POST /api/pools/:id/expand`: call `EngineService.ExpandPool()`, return 200
    - `GET /api/pools/:id/unallocated`: call `EngineService.DetectUnallocated()`, return report
    - `GET /api/pools/:id/export`: call `EngineService.ExportPool()`, return config JSON
    - `POST /api/pools/import`: parse config, call `EngineService.ImportPool()`, return 201
    - `GET /api/pools/:id/rebuild-progress/:arrayId`: call `EngineService.GetRebuildProgress()`, return progress
    - `GET /api/pools/:id/arrays/:arrayId`: call `EngineService.GetArrayStatus()`, return array detail
    - `GET /api/pools/:id/disks/:dev`: call `EngineService.GetDiskStatus()`, return disk detail
    - Map EngineService errors to appropriate HTTP status codes (404 for not found, 400 for client errors, 500 for internal errors)
    - _Requirements: 1.2, 1.11, 1.12_

  - [ ] 4.4 Implement log API handlers and WebSocket Live_Tail
    - `GET /api/logs`: parse query params (level, start, end, source, keyword), call `EventLogger.Query()`, return entries with total count
    - `GET /api/logs/export`: parse same query params, call `EventLogger.Export()`, return NDJSON file download with `Content-Disposition` header
    - `GET /api/logs/stream`: validate session token, upgrade to WebSocket, parse initial filter from query params, call `EventLogger.Stream()`, forward entries as JSON messages, handle client filter update messages, send ping every 30s, clean up on close
    - _Requirements: 1.7, 1.8, 5.7_

  - [ ] 4.5 Implement configuration API handlers
    - `GET /api/config`: return current configuration (HTTPS port, log level, session timeout, paths)
    - `PUT /api/config`: parse config update, validate, persist, return updated config
    - _Requirements: 8.2, 8.3_

  - [ ] 4.6 Implement static asset serving for React SPA
    - Serve files from `StaticDir` for all non-`/api/*` routes
    - Implement SPA fallback: return `index.html` for any non-file route to support client-side routing
    - _Requirements: 1.3_

  - [ ]* 4.7 Write property test P24: Unauthenticated requests are rejected
    - **Property 24: Unauthenticated requests are rejected**
    - Use `rapid` to generate random protected endpoint paths and HTTP methods, send requests without session token, with expired token, and with invalid token, assert all return HTTP 401 with JSON error body
    - **Validates: Requirements 1.4, 1.9, 1.10, 6.4, 6.5**

  - [ ]* 4.8 Write property test P60: API returns JSON responses
    - **Property 60: API returns JSON responses**
    - Use `rapid` to generate random valid API requests (with valid session), assert all responses have `Content-Type: application/json` and valid JSON body (excluding static assets and WebSocket)
    - **Validates: Requirements 1.12**

  - [ ]* 4.9 Write property test P61: API returns correct HTTP status codes
    - **Property 61: API returns correct HTTP status codes**
    - Use `rapid` to generate various API scenarios (successful reads, creates, deletes, invalid requests, auth errors, not-found), assert HTTP status codes match the mapping: reads→200, creates→201, deletes→204, invalid→400, auth→401, not-found→404, internal→500
    - **Validates: Requirements 1.11**

  - [ ]* 4.10 Write unit tests for API Server
    - Test route registration: verify all expected routes are registered on the mux
    - Test each handler with mocked EngineService: create pool→201, get pool→200, delete pool→204, list pools→200
    - Test session middleware: valid token→pass through, missing token→401, expired token→401
    - Test JSON serialization: verify response models match expected structure
    - Test error mapping: EngineService errors→correct HTTP status codes
    - Test WebSocket upgrade: valid session→upgrade, invalid session→401
    - Test request logger middleware logs method, path, status, duration
    - _Requirements: 1.1, 1.2, 1.4, 1.5, 1.7, 1.8, 1.9, 1.10, 1.11, 1.12_

- [ ] 5. Implement CLI Extensions (`cmd/poolforge`)
  - [ ] 5.1 Implement `poolforge serve` command
    - Add `serve` subcommand to CLI that accepts `--port` flag (default 8443)
    - Initialize EngineService, EventLogger, AuthService
    - Create and start API Server with all dependencies
    - Handle SIGINT/SIGTERM for graceful shutdown
    - Display descriptive error messages for startup failures (port in use, TLS cert errors, metadata inaccessible, log dir not writable)
    - _Requirements: 9.1, 9.2, 9.3, 9.4_

  - [ ] 5.2 Implement `poolforge user create` and `poolforge user list` commands
    - Add `user create --username <name> --password <password>` subcommand that calls `AuthService.CreateUser()` and prints confirmation
    - Add `user list` subcommand that calls `AuthService.ListUsers()` and prints table of username and created_at
    - _Requirements: 6.8, 6.9_

  - [ ]* 5.3 Write unit tests for CLI extensions
    - Test `serve` command parses `--port` flag correctly
    - Test `serve` command uses default port 8443 when no flag provided
    - Test `user create` with valid input prints success message
    - Test `user list` prints formatted table
    - Test `serve` startup error messages for each error condition
    - _Requirements: 9.1, 9.2, 9.4, 6.8, 6.9_

- [ ] 6. Checkpoint — API Server and CLI
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 7. Set up React Web Portal project (`web/`)
  - [ ] 7.1 Initialize React project with Vite and configure tooling
    - Initialize React + TypeScript project with Vite in `web/` directory
    - Install dependencies: React Router, fast-check, @testing-library/react, vitest, jsdom
    - Configure Vite proxy for development (proxy `/api` to Go backend)
    - Set up Vitest configuration with jsdom environment
    - Create `web/src/api/client.ts` — API client module with fetch wrapper that includes session cookie, handles 401 redirects to login, and exposes typed functions for all API endpoints
    - Create `web/src/api/types.ts` — TypeScript interfaces for all API response models (PoolSummary, PoolDetail, ArrayStatus, DiskStatusInfo, LogEntry, Config, etc.)
    - Create `web/src/utils/healthColor.ts` — `mapHealthColor()` pure function and CSS class mapping
    - _Requirements: 1.3, 2.2, 3.2, 3.3, 3.4_

  - [ ] 7.2 Implement App shell, routing, and AuthLayout
    - Create `web/src/App.tsx` with React Router: `/login` → LoginPage, `/` → DashboardPage, `/pools/:id` → PoolDetailPage, `/logs` → LogViewerPage, `/config` → ConfigPage
    - Create `web/src/components/AuthLayout.tsx` — wraps protected routes, checks session validity, redirects to `/login` if unauthenticated
    - Create `web/src/components/NavBar.tsx` — navigation links to Dashboard, Logs, Config, and Logout action
    - _Requirements: 2.4, 6.5, 6.10, 6.11_

- [ ] 8. Implement Authentication UI
  - [ ] 8.1 Create LoginPage component
    - Create `web/src/pages/LoginPage.tsx` — username/password form, submits to `/api/auth/login`, stores session token, redirects to `/` on success, displays error on failure
    - Implement logout action in NavBar that calls `/api/auth/logout`, clears session, redirects to `/login`
    - _Requirements: 6.10, 6.11_

- [ ] 9. Implement Dashboard and Notification Banner
  - [ ] 9.1 Create DashboardPage with PoolCard components
    - Create `web/src/pages/DashboardPage.tsx` — fetches pool list from `/api/pools`, renders PoolCardList
    - Create `web/src/components/PoolCard.tsx` — displays pool name, state, total capacity, used capacity with Health_Color badge; clickable to navigate to `/pools/:id`
    - Create `web/src/components/PoolCardList.tsx` — renders array of PoolCard components
    - _Requirements: 2.1, 2.2, 2.3_

  - [ ] 9.2 Create NotificationBanner component
    - Create `web/src/components/NotificationBanner.tsx` — displayed at top of page when any pool has a failed disk, identifies the Disk_Descriptor and Pool name, dismissible by user or auto-dismissed when disk replaced and rebuild completes
    - Wire NotificationBanner into DashboardPage (and AuthLayout for global visibility)
    - _Requirements: 2.5, 2.6_

  - [ ]* 9.3 Write property test P51: Dashboard renders all pools
    - **Property 51: Dashboard renders all pools**
    - Use `fast-check` to generate random arrays of PoolSummary data, render DashboardPage with mocked API, assert rendered card count equals pool count and each card shows name, state, capacity
    - **Validates: Requirements 2.1**

  - [ ]* 9.4 Write property test P54: Notification_Banner on disk failure
    - **Property 54: Notification_Banner on disk failure**
    - Use `fast-check` to generate pool data with and without failed disks, render Dashboard, assert NotificationBanner appears when any pool has a failed disk and identifies the correct disk and pool
    - **Validates: Requirements 2.5**

  - [ ]* 9.5 Write unit tests for Dashboard components
    - Test PoolCard renders name, state, capacity, correct Health_Color CSS class
    - Test PoolCard click navigates to pool detail route
    - Test DashboardPage renders correct number of cards from API data
    - Test NotificationBanner appears when pool has failed disk
    - Test NotificationBanner dismiss button hides the banner
    - _Requirements: 2.1, 2.2, 2.3, 2.5, 2.6_

- [ ] 10. Implement Storage_Map visualization
  - [ ] 10.1 Create StorageMap component hierarchy
    - Create `web/src/components/StorageMap.tsx` — renders pool topology as nested visual hierarchy
    - Create `web/src/components/PoolContainer.tsx` — pool-level container with Health_Color border, pool name and state label
    - Create `web/src/components/ArrayCard.tsx` — RAID array card with Health_Color background, array device, RAID level, tier info, member disk icons
    - Create `web/src/components/DiskIcon.tsx` — disk icon with Health_Color fill, device label
    - Create `web/src/components/RebuildProgressBar.tsx` — progress bar showing rebuild percentage and ETA, rendered on ArrayCard when rebuilding and on DiskIcon when disk is rebuild target
    - Wire click handlers: clicking PoolContainer, ArrayCard, or DiskIcon opens the corresponding DetailPanel
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8_

  - [ ]* 10.2 Write property test P21: Health_Color mapping is deterministic
    - **Property 21: Health_Color mapping is deterministic**
    - Use `fast-check` to generate random HealthState values from {healthy, degraded, rebuilding, failed}, assert `mapHealthColor()` returns green for healthy, amber for degraded/rebuilding, red for failed, and the result is the same regardless of call order or repetition
    - **Validates: Requirements 2.2, 3.2, 3.3, 3.4**

  - [ ]* 10.3 Write property test P23: Failed disk cascading visual state
    - **Property 23: Failed disk cascading visual state**
    - Use `fast-check` to generate pool data with random disk failure patterns, render StorageMap, assert failed disk icons have red class, and all ArrayCards containing a failed disk have amber or red class
    - **Validates: Requirements 3.7**

  - [ ]* 10.4 Write property test P52: Storage_Map hierarchy rendering
    - **Property 52: Storage_Map hierarchy rendering**
    - Use `fast-check` to generate random pool data with N arrays and varying member counts, render StorageMap, assert exactly 1 PoolContainer, N ArrayCards, and correct DiskIcon count per array
    - **Validates: Requirements 3.1**

  - [ ]* 10.5 Write property test P53: Rebuild progress bar rendering
    - **Property 53: Rebuild progress bar rendering**
    - Use `fast-check` to generate array data with and without active rebuilds, render ArrayCard, assert progress bar appears only when rebuild is active and shows correct percentage
    - **Validates: Requirements 3.5, 3.6**

  - [ ]* 10.6 Write unit tests for StorageMap components
    - Test PoolContainer renders with correct Health_Color border class
    - Test ArrayCard renders with correct Health_Color background class
    - Test DiskIcon renders with correct Health_Color fill class for each state
    - Test RebuildProgressBar renders percentage and ETA
    - Test RebuildProgressBar is not rendered when no rebuild active
    - Test click on DiskIcon triggers detail panel callback
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7_

- [ ] 11. Checkpoint — Dashboard and Storage_Map
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 12. Implement Detail Panels
  - [ ] 12.1 Create DetailPanel components
    - Create `web/src/components/DetailPanel.tsx` — container component that renders the appropriate detail content based on selected entity type (pool, array, disk)
    - Create `web/src/components/PoolDetailContent.tsx` — displays pool name, state, Parity_Mode, total capacity, used capacity, list of member RAID_Arrays with states
    - Create `web/src/components/ArrayDetailContent.tsx` — displays sync state, RAID level, Capacity_Tier, array capacity, member Disk_Descriptors with per-disk state, rebuild progress if active
    - Create `web/src/components/DiskDetailContent.tsx` — displays Disk_Descriptor, health state, raw capacity, list of RAID_Arrays the disk contributes to
    - Create `web/src/components/ContextualLogSection.tsx` — fetches and displays recent log entries filtered to the selected component's source identifier (e.g., `pool:mypool`, `array:md0`, `disk:/dev/sdb`)
    - _Requirements: 4.1, 4.2, 4.3, 4.4_

  - [ ]* 12.2 Write property test P22: Detail_Panel contains all required fields
    - **Property 22: Detail_Panel contains all required fields**
    - Use `fast-check` to generate random pool/array/disk data, render each DetailPanel variant, assert all required fields are present in the rendered output
    - **Validates: Requirements 4.1, 4.2, 4.3**

  - [ ]* 12.3 Write property test P55: Contextual log section in Detail_Panel
    - **Property 55: Contextual log section in Detail_Panel**
    - Use `fast-check` to generate random component identifiers and log entries, render DetailPanel with mocked log API, assert contextual log section displays entries filtered to the correct source
    - **Validates: Requirements 4.4**

  - [ ]* 12.4 Write unit tests for DetailPanel components
    - Test PoolDetailContent renders all required fields from pool data
    - Test ArrayDetailContent renders sync state, RAID level, tier, capacity, members
    - Test ArrayDetailContent renders rebuild progress when active
    - Test DiskDetailContent renders device, health, capacity, array list
    - Test ContextualLogSection fetches logs with correct source filter
    - _Requirements: 4.1, 4.2, 4.3, 4.4_

- [ ] 13. Implement Pool Detail Page with management actions
  - [ ] 13.1 Create PoolDetailPage and pool management forms
    - Create `web/src/pages/PoolDetailPage.tsx` — fetches pool detail from `/api/pools/:id`, renders StorageMap, DetailPanel, and PoolActions
    - Create `web/src/components/PoolActions.tsx` — action buttons/forms for add disk, replace disk, remove disk, expand, delete, export, import
    - Create `web/src/components/CreatePoolForm.tsx` — form collecting pool name, Parity_Mode, disk selection; submits to `POST /api/pools`; accessible from Dashboard
    - Create `web/src/components/AddDiskForm.tsx` — form collecting Disk_Descriptor; submits to `POST /api/pools/:id/disks`
    - Create `web/src/components/ReplaceDiskForm.tsx` — form collecting failed and replacement Disk_Descriptors; submits to `POST /api/pools/:id/replace-disk`
    - Create `web/src/components/ConfirmationDialog.tsx` — modal dialog for destructive operations (delete pool, remove disk); displays operation details and requires explicit approval before submitting
    - For remove-disk with RAID downgrade: display proposed RAID level changes in ConfirmationDialog
    - Display API error messages to user when any operation fails
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6, 7.7, 7.8, 7.9, 7.10_

  - [ ]* 13.2 Write property test P58: Confirmation_Dialog for destructive operations
    - **Property 58: Confirmation_Dialog for destructive operations**
    - Use `fast-check` to generate random destructive operation types (delete pool, remove disk), simulate initiating the operation, assert ConfirmationDialog is rendered and the API request is NOT sent until explicit approval
    - **Validates: Requirements 7.8**

  - [ ]* 13.3 Write property test P59: API error messages displayed to user
    - **Property 59: API error messages displayed to user**
    - Use `fast-check` to generate random error response bodies and HTTP status codes (4xx, 5xx), simulate a failed pool management operation, assert the error message from the API response is displayed in the UI
    - **Validates: Requirements 7.10**

  - [ ]* 13.4 Write unit tests for pool management components
    - Test CreatePoolForm submits correct payload to API
    - Test AddDiskForm submits correct payload
    - Test ReplaceDiskForm submits correct payload
    - Test ConfirmationDialog appears for delete pool action
    - Test ConfirmationDialog appears for remove disk action
    - Test ConfirmationDialog blocks submission until confirmed
    - Test ConfirmationDialog cancel does not submit request
    - Test remove-disk ConfirmationDialog shows RAID downgrade info when applicable
    - Test error message display when API returns error
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6, 7.7, 7.8, 7.9, 7.10_

- [ ] 14. Implement Log Viewer
  - [ ] 14.1 Create LogViewerPage with filter, table, Live_Tail, and export
    - Create `web/src/pages/LogViewerPage.tsx` — main log viewer page layout
    - Create `web/src/components/LogFilterBar.tsx` — filter controls: Log_Level multi-select checkboxes, time range start/end datetime pickers, source component dropdown, keyword search text input
    - Create `web/src/components/LogTable.tsx` — displays log entries in reverse chronological order with columns: Timestamp, Level, Source, Message; Level column styled with color (info=default, warning=amber, error=red)
    - Create `web/src/components/LiveTailToggle.tsx` — button to enable/disable WebSocket streaming; when active, new entries appear at top of table; active filters apply to streamed entries
    - Create `web/src/components/LogExportButton.tsx` — downloads currently filtered entries as NDJSON file via `/api/logs/export`
    - Implement WebSocket connection to `/api/logs/stream` with filter params, handle incoming log entries, apply client-side filters, handle disconnection with reconnect option
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7, 5.8, 5.9_

  - [ ]* 14.2 Write unit tests for LogViewer components
    - Test LogTable renders entries in reverse chronological order
    - Test LogTable applies correct color styling per log level
    - Test LogFilterBar renders all filter controls
    - Test LogFilterBar emits correct filter values on change
    - Test LiveTailToggle enables/disables WebSocket connection
    - Test LogExportButton triggers file download
    - Test multiple active filters are applied as logical AND
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7, 5.8, 5.9_

- [ ] 15. Implement Configuration Page
  - [ ] 15.1 Create ConfigPage component
    - Create `web/src/pages/ConfigPage.tsx` — fetches config from `GET /api/config`, displays current HTTPS port and other settings, provides form to modify settings via `PUT /api/config`
    - _Requirements: 8.1, 8.2, 8.3_

- [ ] 16. Checkpoint — Web Portal complete
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 17. Integration tests
  - [ ]* 17.1 Write API authentication integration tests
    - Test full auth flow: create user via CLI → login via API → use session token for protected request → logout → verify token invalid
    - Test login with invalid credentials returns 401
    - Test protected endpoints without token return 401
    - File: `test/integration/api_auth_test.go`
    - _Requirements: 1.4, 1.5, 1.9, 1.10, 6.1, 6.2, 6.3, 6.4, 6.5_

  - [ ]* 17.2 Write API pool CRUD integration tests
    - Test create pool via `POST /api/pools` → get pool via `GET /api/pools/:id` → get status → delete pool via `DELETE /api/pools/:id`
    - Verify correct HTTP status codes and JSON response structures
    - File: `test/integration/api_pool_test.go`
    - _Requirements: 1.2, 1.11, 1.12_

  - [ ]* 17.3 Write API disk operations integration tests
    - Test add disk via `POST /api/pools/:id/disks` → replace disk → remove disk
    - Verify operations produce same results as CLI equivalents
    - File: `test/integration/api_disk_test.go`
    - _Requirements: 1.2_

  - [ ]* 17.4 Write API log query and WebSocket integration tests
    - Test log query with various filter combinations via `GET /api/logs`
    - Test log export via `GET /api/logs/export`
    - Test WebSocket Live_Tail: connect, perform operation, verify log entry received in real time
    - File: `test/integration/api_log_test.go`
    - _Requirements: 1.7, 1.8, 5.7_

  - [ ]* 17.5 Write API export/import integration tests
    - Test export pool config via `GET /api/pools/:id/export` → import via `POST /api/pools/import` → verify pool recreated
    - File: `test/integration/api_export_import_test.go`
    - _Requirements: 1.2_

  - [ ]* 17.6 Write Phase 1 + Phase 2 regression tests
    - Verify all existing CLI commands (pool create, status, list, add-disk, replace-disk, remove-disk, delete, expand, export, import) continue to work unchanged
    - Verify metadata persistence, self-healing, and test infrastructure are unaffected
    - _Requirements: 11.4_

- [ ] 18. End-to-end tests
  - [ ]* 18.1 Write E2E login/logout workflow test
    - Navigate to `/` → verify redirect to `/login` → enter credentials → verify redirect to Dashboard → logout → verify redirect to login → verify protected pages inaccessible
    - File: `test/e2e/login.spec.ts`
    - _Requirements: 6.5, 6.10, 6.11, 11.3_

  - [ ]* 18.2 Write E2E dashboard and Storage_Map drill-down test
    - Verify all pools displayed on Dashboard with correct Health_Color
    - Click pool card → verify Storage_Map renders → click array → verify Detail_Panel → click disk → verify Detail_Panel
    - File: `test/e2e/dashboard.spec.ts` and `test/e2e/storage_map.spec.ts`
    - _Requirements: 2.1, 2.2, 2.3, 3.1, 3.8, 4.1, 4.2, 4.3, 11.3_

  - [ ]* 18.3 Write E2E pool management workflow test
    - Create pool via form → verify on Dashboard → add disk → replace disk → remove disk with confirmation dialog → delete pool with confirmation dialog → verify removed from Dashboard
    - File: `test/e2e/pool_management.spec.ts`
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.6, 7.8, 11.3, 11.7_

  - [ ]* 18.4 Write E2E log viewer workflow test
    - Navigate to `/logs` → apply level filter → apply time range → apply source filter → apply keyword search → verify filtered results → enable Live_Tail → verify streaming → export logs → verify downloaded file
    - File: `test/e2e/log_viewer.spec.ts`
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7, 5.8, 5.9, 11.3_

- [ ] 19. Final checkpoint — All Phase 3 tests pass
  - Ensure all unit tests, property tests, integration tests, and E2E tests pass
  - Verify no Phase 1 or Phase 2 regressions
  - Ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties (P21–P30, P51–P56, P58–P61)
- Go property tests use `rapid` (flyingmutant/rapid); React property tests use `fast-check`
- React component tests use React Testing Library + Vitest; E2E tests use Playwright or Cypress
- Phase 3 MUST NOT modify EngineService or break any Phase 1/Phase 2 functionality
- All existing CLI commands continue to work unchanged
