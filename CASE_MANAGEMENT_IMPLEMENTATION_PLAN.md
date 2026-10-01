# Case management implementation plan

Reviewed: 2026-09-29

Implementation updated: 2026-09-30. The assessment and confirmed issues below retain the original review baseline; use the phase checklists and verification notes for current implementation status.

## Purpose and review scope

Complete case management in `New-fraud-system`, using the existing Marble implementation in `../api` and `../front` as the behavioral reference. This plan covers cases, inboxes, investigation, routing, evidence, automation, and their decision-engine and screening integrations. It does not propose replacing the new service architecture with Marble's monolith.

This is a source-code review, not a runtime certification. The service, database migrations, worker, HTTP handlers, frontend Cases page, Compose configuration, and producer-side integration clients were inspected. No running stack or end-to-end behavior was tested for this review. “Present” below means code exists; it does not mean production-ready.

## Standing architecture and quality requirements

All services use the same logical PostgreSQL database. The separate services retain ownership of their tables and business rules; database boundaries are not per-service deployment boundaries. Follow [the workspace engineering requirements](AGENTS.md): maintainable implementations, no quick hacks, and performance considered during design and verified where relevant.

Cross-service reference validation must evaluate direct tenant-scoped database queries or stable read views alongside service APIs. Select and document the appropriate contract; do not assume an HTTP round trip is required for data already in the shared database. Coordinate cross-schema constraints with migration ordering and ownership. Shared data access never replaces authorization.

Connection limits, worker concurrency, locks, migration duration, and query load must be assessed across every service and replica using this database. Disposable test databases remain appropriate for isolation; production services share the application database.

## Assessment

The new system has a useful case-management backend foundation. It is not yet a usable end-to-end investigation product. Most basic mutations exist, but transaction safety, access control, integration reliability, and the frontend remain unfinished. Several database tables and HTTP endpoints are scaffolds only.

The recommended first release is a reliable manual investigation flow plus reliable decision-to-case routing. Advanced AI review, suspicious activity reports, and analytics should follow that release rather than block it.

## Existing implementation and remaining gaps

| Capability | Current evidence | Remaining work |
| --- | --- | --- |
| Service and persistence | Standalone Go service; migrations; `case_manager` schema; API and worker entrypoints | Enable runtime wiring and meaningful worker execution; expand operational documentation |
| Case creation/detail/update | Implemented handlers, service methods, repositories | Transaction boundaries, validation, authorization, reliable relationship loading, concurrent-update protection |
| Lifecycle and outcomes | Pending, investigating, closed; four outcomes including unset; transition helper and database checks | Validate enums before writing, action side effects, contributors, explicit close/reopen behavior in UI |
| Case list | Name/status/inbox/assignee/snooze filters; bounded SQL limit | Cursor pagination, unassigned/date/tag/review filters, counts, next-case navigation, frontend |
| Inboxes | Create/list/get/update; escalation/assignment/review settings stored | Membership APIs and authorization, archive rules, nullable-setting clearing, SLA settings, settings UI |
| Assignment | Manual assign/unassign endpoints | Assignee eligibility, previous-value audit, self-assignment on investigation actions, automatic assignment |
| Snooze | Set/clear timestamp and list exclusion | Input/state rules, expiry processing, attention flags and UI |
| Escalation | Endpoint delegates to ordinary inbox update with caller-provided inbox ID | Configured destination, eligibility checks, dedicated event, assignment/attention side effects |
| Comments/events | Comments and events stored and listed | Atomic audit trail, complete history pagination, contributors, actor integrity |
| Tags | Create/list and attach/detach routes; repository update/delete methods | Expose supported tag administration, validate targets/ownership, UI |
| Files | File metadata and add-file endpoint | Actual upload/download lifecycle, storage ownership validation, UI |
| Workflow intake | Create-case and pivot-based reuse logic; decision-engine HTTP dispatcher | Shared action contract, idempotency, concurrency-safe grouping, metadata hydration, reliable routing |
| Screening integration | Review callback, screening links, screening publisher client | Deterministic routing, durable delivery, match identity, functional evidence callback and investigation UI |
| Automatic assignment | Settings/table and internal endpoint exist | Endpoint currently acknowledges only; worker has no assignment logic |
| AI review | Inbox flags, review-level field, table, internal endpoint | Queue, provider integration, results/history/feedback APIs and UI; endpoint currently acknowledges only |
| Suspicious activity reports | Table exists | Service/repository/API, document storage, export and UI |
| Outbound events | Outbox table and publisher URL config exist | Transactional enqueue, delivery worker, retry/deduplication and observability |
| Frontend | `/cases` renders three hardcoded rows | Typed client, query integration, list, detail, actions and inbox settings |
| Testing | Case service has one domain test file covering transitions/review levels | Service, repository, HTTP, integration, concurrency and browser coverage |

## Confirmed issues to resolve before enabling operational use

1. **Case management is disabled in the default stack.** Case migration, API, worker, and relevant producer URLs are commented out in `docker-compose.yml`.
2. **Writes and audit events are not atomic.** The service ignores errors from many relationship/event writes. `UpdateCase` creates some events before all validation and the final update. A failed update can leave misleading events; a successful mutation can lack its event. Detail loading also suppresses relationship-read failures.
3. **Tenant filtering is not authorization.** Middleware checks an optional shared token; tenant selection comes from request input and actor identity from `X-Actor-ID`. Inbox membership is not enforced. Foreign keys reference resource IDs without enforcing matching tenant IDs, and service methods do not consistently validate referenced resources. Shared-token callers therefore need an explicit trusted identity and tenant boundary.
4. **Workflow retries can create duplicate cases.** `WorkflowExecutionID` is accepted but never used for deduplication. Pivot lookup and case creation are separate operations without a grouping lock.
5. **Producer and consumer action types disagree.** The decision-engine workflow model permits `create_case`, `add_tag`, and `emit_event`; case intake also branches on `add_to_case` and `add_to_case_if_possible`. Other action types can fall through to case creation when an inbox is supplied. Reject unsupported actions and route each supported action deliberately.
6. **Escalation is currently just a move.** It does not resolve the source inbox's configured escalation destination or reject closed cases/inactive destinations.
7. **Screening callbacks are incomplete.** Review handling scans at most 500 non-snoozed cases for a decision and otherwise uses the first inbox. A missed match can create another case. Link uniqueness is per screening per case, although callbacks include a match ID. The evidence callback returns `202` without saving anything. The screening service ignores publication errors, and its case/inbox HTTP clients do not send the token required by case-manager token mode.
8. **Worker-backed behavior is not implemented.** The worker only logs each cycle; auto-assignment and AI routes return `202` without durable work acceptance. Existing snooze filtering does make expired cases visible again, but it does not implement wake-up events or attention processing.
9. **Validation and error handling need hardening.** Pending-to-arbitrary-status passes the domain helper and relies on a database check; invalid inbox filter UUIDs are silently dropped; assignment/snooze writes do not check affected-row counts; most errors become raw `400` responses. Empty review-level clearing conflicts with the database constraint, while nullable escalation settings cannot be explicitly cleared through the current pointer-only update model.

## Delivery order

Priority meanings: **P0** protects correctness/access and enables delivery; **P1** completes the investigator workflow; **P2** supplies advanced parity.

Dependencies: Phase 1 precedes enabling operational traffic. Phase 2 depends on Phase 1's identity and transaction contracts. Phase 3 depends on the stable core APIs and working decision routing. Phase 4 builds on Phases 1–3. Phase 5 follows the core release.

### Identity-provider dependency: deferred for development

Proceed with Phase 4 and independent Phase 5 implementation without provisioning an identity provider. The existing identity contract remains required: user JWT verification, tenant isolation, inbox membership, and trusted audit actors must remain enforced. Controlled test identities and test signing keys can verify these contracts locally; they do not establish production sign-in readiness.

- [x] Separate provider provisioning from implementation of queue operations, evidence, bulk actions, tags, reporting, analytics, and exports.
- [ ] Integrate a real identity provider or trusted identity gateway, stable user subjects, and the browser session flow before enabling live investigator access.
- [ ] Complete the Phase 3 investigator journey against the running stack with that identity source. Keep this acceptance item open while later implementation proceeds.

Provider integration is a release dependency, not a reason to disable authorization or mark unverified acceptance checks complete. AI execution and evidence storage have their own provider/storage dependencies; deferring identity does not resolve those dependencies.

### Phase 1 — Make the backend trustworthy (P0)

Primary files: `backend/case-manager-service/internal/{service,ports,store/postgres,httpapi,domain/case}` and new forward migrations.

Implementation progress (2026-09-29): Phase 1 backend functionality is implemented and verified locally. This includes transaction-scoped commands, narrow row locks, atomic audit/outbox writes, validation and nullable PATCH handling, typed errors, verified JWT identity, inbox membership authorization, assignee eligibility, shared-database reference validation, and investigation side effects. Existing review findings above describe the original baseline. Deployment-specific operational verification remains explicitly open below; this is not a production-readiness claim.

Identity contract: investigator routes require RS256 JWTs from a configured issuer/audience and pinned public keys, with tenant, subject, issued/expiry times, and case roles. Service integration routes use a separate token boundary with an explicit tenant allowlist. Neither actor headers nor callback reviewer fields establish a human identity. Configuration, memberships, database ownership contracts, and pool allocation are documented in the [service README](backend/case-manager-service/README.md).

Shared-database review follow-ups (required before operational readiness):

- [x] Replace broad transaction-scoped inbox `FOR UPDATE` reads with explicit lock modes appropriate to validation versus mutation. Shared validation locks permit unrelated creates; moves/settings use ordered inbox locks. Concurrent archive/create and archive/move tests pass.
- [x] Batch case-list tag enrichment instead of issuing a query per case. Tests cover 10/100/500 case queues with three tags per case and assert two queries; reference lookup EXPLAIN uses the decision-link index.
- [x] Replace screening's bounded case scan with indexed relationship lookups. The integration test locates a snoozed case beyond a 1,200-case queue without creating a duplicate.
- [x] Define API/worker pool allocation and bounded request, statement, lock, and idle-transaction timeouts. Verify pool ceilings, canceled acquisition waits, lock timeouts, and concurrent shared-schema workloads locally. The worker remains disabled; its future allocation is included in the documented budget.
- [ ] Validate the deployment's actual replica/pool allocation and measure pool/lock waits under concurrently running service APIs and workers. Local synthetic database contention across five owner pools is covered, but is not a deployed multi-service load test.
- [x] Verify reference-validation contracts and all-service migration compatibility against the shared database. Integration fixtures apply all current data-model, ingestion, decision-engine, screening, and case migrations to the same disposable database, and exercise tenant-scoped cross-schema references and migration preflight/down/up.

- [x] Introduce transaction-scoped repositories/unit of work. Commit case changes, links, contributors, events, and outbound notification records together. Injected failures roll back the command; delivery remains Phase 2/4 work.
- [x] Validate command values and referenced resources before writing. Validate names, status/outcome/type/review enums, UUIDs, snooze dates, tags, active inboxes, and supported action types. Workflow metadata is checked against its decision before case creation. Invalid commands leave no persisted changes/events; nullable PATCH semantics are explicit.
- [x] Protect simultaneous mutations using row locks or a version/conditional-update contract. Avoid overwriting a concurrent assignment when saving an unrelated case field.
- [x] Return typed validation, not-found, forbidden, conflict, and internal errors; do not expose raw database errors. Check rows affected on case assignment/snooze operations.
- [x] Establish a trusted actor/tenant context from authenticated identity. Separate service integration permissions from human investigator permissions. Forged actor headers, invalid JWTs, wrong tenants, and use of service tokens on user routes are tested.
- [x] Implement inbox memberships using `inbox_users`; enforce tenant and inbox access on lists, reads, writes, links, files, and destination changes. Assignees must be members; revocation and reopening eligibility are tested.
- [x] Validate local tenant relationships and authoritative decision/screening/match/file ownership through shared-database reads. Local composite foreign keys include escalation destinations. Preflight rejects orphan/cross-tenant historical links. New file attachments require a verified screening `source_file_id`; arbitrary blob keys are rejected.
- [x] Implement contributor recording and shared investigation-action side effects: pending-to-investigating, eligible self-assignment, and attention clearing. Manual creation starts investigating; automated creation remains pending. Explicit assignment/unassignment preserves the requested assignee. All effects share the command transaction.
- [x] Fix partial detail reads: fail explicitly or expose a documented partial-response contract; do not silently present failed reads as empty evidence.

Acceptance:

- [x] A forced event/link/contributor/outbox failure rolls back its case mutation, and failed validation persists no events.
- [x] Tenant A cannot read or attach Tenant B's cases, inboxes, tags, files, decisions, or screenings; users without inbox membership cannot access its cases.
- [x] Concurrent name edits and assignment changes do not silently overwrite each other.
- [x] Invalid command values produce validation errors; missing resources return not-found; nullable fields clear correctly.

Verification evidence: `go test ./...` with `CASE_MANAGER_TEST_DATABASE_URL` runs real PostgreSQL tests, including signed HTTP requests. Queue reads remain two queries at 10/100/500 records with tags; the measured reference lookup uses `case_decisions_lookup_idx`. A local shared-schema contention test completes 300 commands across five two-connection pools and records pool acquisition waits without exceeding its ten-connection workload ceiling. These are local correctness/contention checks, not production throughput measurements. See [completion tests](backend/case-manager-service/internal/store/postgres/phase1_completion_integration_test.go), [access tests](backend/case-manager-service/internal/store/postgres/phase1_access_integration_test.go), and [shared-schema load test](backend/case-manager-service/internal/store/postgres/shared_load_integration_test.go).

### Phase 2 — Wire runtime and reliable alert intake (P0)

Implementation progress (2026-09-29): Phase 2 code and configuration are implemented and verified locally. The case API/migrations and dedicated screening callback worker are wired in Compose; the placeholder case worker remains disabled. See [the intake contract](backend/case-manager-service/INTAKE_CONTRACT.md) for payloads, grouping, receipts, delivery recovery, and migration sequencing. These implementation ticks do not establish full-stack or deployment readiness.

Primary files: `docker-compose.yml`, case service integration handlers/repositories, decision-engine workflow domain/dispatcher, screening clients and service.

- [x] Enable the case migration and API services with health checks, dependency ordering, documented port configuration, and matching database credentials. Enable the worker only once it executes real jobs.
- [x] Wire `WORKFLOW_ACTION_URL` for the decision API and worker, plus `CASE_SERVICE_URL` and `INBOX_SERVICE_URL` for screening producers. Cover database-scale deployment configuration where services are used separately.
- [x] Define a shared workflow payload contract and update producer validation, documentation, and any workflow editor. Support create versus reuse explicitly; keep add-tag/event behavior explicit and reject unrelated action types.
- [x] Persist a receipt keyed by tenant and workflow execution ID, including resulting case ID and payload identity. Atomically deduplicate retries; reject conflicting reuse of an ID.
- [x] Serialize pivot lookup/create using a transaction lock with a documented tenant/pivot/inbox scope. Handle `any_inbox` consistently with inbox-specific grouping. Define deterministic case selection and closed-case exclusion.
- [x] Resolve authoritative decision object/scenario/pivot metadata, rather than depending on callers to populate trusted linkage fields. Specify case title/template behavior and verify it against Marble workflows.
- [x] Replace the screening case scan with indexed decision/screening/match lookups and explicit configured inbox routing. Persist the intended relationship between a screening and its matches without losing match-level history.
- [x] Give screening callbacks stable event IDs and durable retry delivery; remove ignored publication failures. Add service authentication to the screening case/inbox clients. Treat an absent URL as a documented disabled feature or configuration error, not silent successful delivery.
- [x] Make evidence callbacks persist a relationship and audit event, or report an explicit unsupported response until implemented. Return `202` only after durable acceptance.
- [x] Document the actual stack and case endpoints in the root/service READMEs; the root README still describes the frontend as a placeholder despite the wider application.

Acceptance:

- [ ] A fresh local stack migrates and serves case health checks. A real decision workflow creates a case with correct decision metadata. Both Compose configurations validate, but Docker Desktop's daemon is unavailable on this host; fresh-container startup and an end-to-end decision-worker dispatch remain unverified. PostgreSQL-backed HTTP intake and dispatcher contract tests pass separately.
- [x] Replaying one execution produces one logical case/action; concurrent alerts for the same grouping key follow the declared grouping policy.
- [x] Screening callbacks find a linked case beyond 500 records and when snoozed; retrying callbacks does not create duplicate cases/events.
- [x] Token-protected producer/receiver contract tests succeed; simulated HTTP failure/recovery delivers queued work with stable IDs and observable retry state. The real PostgreSQL tests also cover lease recovery/fencing and rollback of the review if enqueue fails. Container-level outage/restart verification remains part of the open full-stack acceptance check.

Verification: `go test ./...` and `go vet ./...` pass for case-manager, screening, and decision-engine; case/screening tests use isolated PostgreSQL databases. Six database-scale suite tests pass. Default and database-scale Compose configuration validation passes using placeholder configuration. Evidence callbacks deliberately return 501; evidence persistence remains Phase 4 work. Existing decision-status callback delivery is outside this case-intake increment.

### Phase 3 — Ship the investigator experience (P1)

Implementation progress (2026-09-29): Phase 3 workspace code is implemented and locally verified. The Cases UI now uses live authorized APIs through a same-origin server boundary, with paginated queues/history, investigator actions, owner-service evidence, inbox administration, configured escalation, and permission-filtered related cases. See [the investigator contract](backend/case-manager-service/INVESTIGATOR_CONTRACT.md). The user identity provider/gateway must still establish the signed HttpOnly session; provider details have been requested. The existing login form is not a completed authentication integration.

Primary areas: `frontend/src/app/(authenticated)/cases`, proposed `frontend/src/components/cases`, proposed `frontend/src/lib/case-manager-api.ts` and query helpers; corresponding case read/mutation APIs.

- [x] Follow `frontend/AGENTS.md` and read the installed Next.js guides relevant to routes/data loading before implementation. Reuse the existing UI and query conventions.
- [x] Add typed case/inbox/tag/event contracts and tenant-aware query keys. Choose an authenticated same-origin/server boundary for service credentials; never publish shared service tokens to the browser. Wire service URLs and Docker build/runtime settings accordingly.
- [x] Replace the hardcoded Cases page with live inbox queues, loading/error/empty states, search, filters, stable sorting, and cursor pagination. Remove sample severity/stage labels unless backed by a defined domain field.
- [x] Extend APIs with date/tag/review/unassigned filtering, queue counts, paginated events and decision links, and deterministic next-case navigation. Batch tag enrichment instead of a query for each case.
- [x] Build case detail with lifecycle/outcome, inbox, assignee, contributors, tags, linked decisions/screenings, comments, evidence, and a chronological activity timeline.
- [x] Hydrate decision details/rule evidence and screening matches from their owning services. Distinguish unavailable upstream data from “no evidence.” Reuse existing decision-detail navigation where useful.
- [x] Add manual create, rename, assign/unassign, move, comment, tag, snooze/unsnooze, close with outcome/comment, and reopen controls. Refresh affected detail, queue, and counts after success; display actionable mutation errors.
- [x] Add inbox settings for membership, archive/restore policy, escalation destination, and supported options. Hide or mark unsupported automation options until their jobs work.
- [x] Expose dedicated escalation behavior: resolve the configured active destination in the same tenant, disallow closed cases, record `case_escalated`, and apply assignment/attention policy. Preserve the authorized escalation exception for users without destination inbox membership.
- [x] Add related cases by pivot/object and customer/object navigation with server-enforced permissions.

Acceptance:

- [ ] Verify the complete investigator journey against a running deployment and live identity provider. The UI controls are implemented; browser fixtures verify evidence, findings, close/reload/reopen, while PostgreSQL tests independently verify real mutations and escalation. Provider integration and full-stack runtime verification remain open.
- [x] Reload preserves state/history in browser contract tests; real PostgreSQL tests page through 620 cases and 620 history entries with tied timestamps without duplicates/omissions. Browser tenant switching does not expose another tenant's cached cases.
- [x] The UI distinguishes lifecycle (`pending`, `investigating`, `closed`) from derived snoozed/waiting-for-action state and investigation outcome.
- [x] Four Chromium browser tests pass through the real Next.js proxy with a controlled HTTP service fixture: permitted/denied inboxes, evidence hydration, failed writes, empty queues, tenant isolation, close/reload/reopen, missing sessions, and cross-origin write denial. These are contract tests, not a live identity-provider test.

Verification: case-manager `go test ./...` with isolated PostgreSQL and `go vet ./...` pass. The production frontend build, TypeScript checks, targeted ESLint checks, and four browser tests pass. Default/database-scale Compose configurations validate. Migration `000005_workspace_indexes` is tested up/down/up, with representative queue index use. Existing Phase 1/2 deployment checks remain open. Evidence upload/download and complete customer/KYC enrichment remain in their later phases.

### Phase 4 — Complete queue operations and evidence (P1)

- [x] Implement automatic assignment with eligible inbox members, per-user capacity, stable tie-breaking, atomic claiming, audit events, and retry safety. Periodic state reconciliation picks up create/close/unassign/move/configuration changes; capacity is per inbox membership and enforced across manual assignment and reopening too.
- [x] Implement snooze expiry processing with one wake-up event, attention boost, and safe concurrent handling. Document how new alerts, reassignment, and escalation affect snoozed cases and queue ordering. Closing clears snooze.
- [x] Add inbox SLA configuration and due-date calculation. Creation time plus the current inbox SLA uses fixed UTC days; moves and SLA edits change the derived deadline. Add urgency display and an overdue filter.
- [x] Implement real file upload, finalize, and authorized download for native case evidence. Validate ownership, size/type, metadata, and completion; handle abandoned uploads. Bounded PostgreSQL storage supplies actual bytes because the screening blob client is a placeholder. Existing screening-source metadata remains visible; its callback/download integration is still dependent on owner storage, as documented below.
- [x] Complete transactional outbox writes and delivery with leases, retries/backoff, delivery identity, failure visibility, and replay. Keep persisted case events as investigation history, distinct from outbound delivery state. The operator outbox command supports status inspection and replay.
- [x] Replace the logging-only worker with tested River jobs. Separate maintenance and delivery queues prevent a slow receiver from delaying case operations. Update `backend/RIVER_QUEUE_MIGRATION_PLAN.md`; enable the worker in both Compose configurations without implying it is deployed.
- [x] Add mass close/reopen/assign/move operations and UI with per-case authorization, 1–100-case batches, explicit partial-failure results, one audit record per successful case, and operation-ID retry receipts.
- [x] Finish tag update/archive APIs and UI using the existing repository methods, preserving historical case references. Archived tags remain visible on existing cases and cannot be newly attached.

Acceptance:

- [x] Concurrent-worker PostgreSQL tests enforce investigator capacity and exactly one snooze-expiry event. A 10,000-case full-inbox backlog does not starve another eligible inbox; workload counting uses the assignment index.
- [x] Native uploaded evidence requires current tenant/inbox access, survives service/browser reload, and finalizes once under concurrent retries. Tests cover ownership, incomplete data, audit/outbox rollback, and retention during abandoned-upload cleanup.
- [x] Bulk actions report individual failures and preserve audit history. Tests exercise all four operations, destination denial, replay/payload conflicts, and rollback. Outbox tests verify lease recovery, stale completion rejection, backoff, stable-ID replay and real HTTP delivery. Consumers must deduplicate redelivery.
- [x] SLA dates follow the documented UTC policy, with offset/date boundaries, leap day, daylight-saving boundary dates, moves and policy-edit tests.

Verification: the full case-manager Go suite passes with isolated PostgreSQL, as does `go vet ./...`. The production frontend build (including TypeScript), targeted ESLint, and seven Chromium contract tests pass. Default/database-scale Compose configurations validate. Migrations 000006–000008 are tested in forward/down/up order; evidence downgrade refuses retained data. Real River tests verify recurring work and continued maintenance while outbound HTTP is blocked. Runtime policies and recovery commands are in [the investigator contract](backend/case-manager-service/INVESTIGATOR_CONTRACT.md#phase-4-queue-operations).

Remaining release/integration checks (not implied complete by implementation):

- [ ] Apply migrations and verify the API/workers, connection budget, storage growth, and mixed-service load in the target deployment.
- [ ] Configure and verify the actual outbound consumer. Without its URL, delivery is explicitly disabled and events remain pending.
- [ ] Integrate investigator sign-in and perform live full-stack acceptance, deferred by agreement.
- [ ] Complete screening-owned source storage and its evidence callback/download integration. Native case uploads work; the existing screening evidence callback still returns 501 and source files expose metadata only. This dependency is not silently treated as successful storage.

### Phase 5 — Advanced Marble parity (P2)

- [ ] AI assistance: durable review requests, job execution, provider configuration, status/error handling, stored results/history, human feedback, and manual/create/escalation triggers. Keep model recommendations separate from human case outcomes.
- [x] Suspicious activity reports: use the existing table to implement draft/completed state, report/document attachment, retrieval/export, access checks, history, and UI. Define supported report format; do not imply external regulatory submission exists.
- [x] Case analytics: backlog/status/outcome by inbox and date, assignment workload, time-to-close, snooze/escalation and SLA metrics, with documented event/time semantics.
- [ ] Case export: authorized evidence and history export with completeness and large-case handling.
- [ ] Review secondary dependencies for exact Marble parity: decision review operations, customer/KYC enrichment, rule-snooze history, and case-outcome feedback to scoring. Implement against the owning services rather than duplicating their data models.

Phase 5 report increment (2026-09-30):

- [x] Migration `000009_reports` adds lifecycle versions, actor/completion provenance, valid-state checks, and indexed cursor pagination. Unknown legacy payloads remain downloadable without invented provenance; incompatible legacy reports cannot be edited through the new format. Downgrade refuses reports created or changed through the new workflow.
- [x] Tenant/inbox-authorized draft creation, versioned editing, immutable completion, retrieval, and private JSON download. Completion snapshots case metadata and selected finalized native evidence IDs, names, sizes, media types, and SHA-256 hashes. Report content changes, contributor updates, history, and outbox writes share one transaction. Completing a report does not change the case outcome.
- [x] Investigator report editor with attachment selection, UTC activity dates, required completion fields, pagination, visible errors, and download. Supported format is `internal_sar_v1`, an internal report rather than a regulator-specific submission.
- [x] Verification: full case-manager Go/PostgreSQL suite, `go vet`, frontend production build, targeted case/frontend lint, and browser report create/edit/complete/reload/download flow. The seven existing browser scenarios also passed. PostgreSQL tests cover conflicting edits, completion/create retries, audit rollback, revoked access, tenant/inbox isolation, attachment validation, legacy upgrade/rollback, and index-backed pagination over 10,000 reports.
- [ ] Remaining Phase 5 implementation: AI review workflow/provider configuration, full case evidence/history export, and secondary owner-service integrations. Report JSON download does not represent the complete case export. Live identity-provider and deployment gates remain open; no live AI provider or external regulatory filing has been configured.


Phase 5 analytics increment (2026-09-30):

- [x] Authorized `/case-analytics` API and `/cases/analytics` investigator page: inbox/date filters, current lifecycle/outcome totals, open backlog and assignment workload, daily UTC creation groups, snooze/escalation activity, current SLA/overdue counts, and audited time-to-close with explicit missing-history counts.
- [x] Documented semantics in `backend/case-manager-service/INVESTIGATOR_CONTRACT.md`: select cases created in `[from,to)`; show current status/current inbox, not historical backlog. Bulk closes and reopenings are accounted for; missing close history does not use `updated_at` as a substitute. All aggregates and membership checks use one database statement snapshot.
- [x] Migration `000010_analytics` adds creation-range and relevant-event indexes with reversible, data-preserving rollback. Queries require a positive date interval of at most 366 days and reject more than 1000 inbox/assignment groups without silently truncating totals.
- [x] Verification: full case-manager Go/PostgreSQL suite and `go vet`, frontend build and targeted lint, eight existing browser scenarios plus the analytics filter/validation/empty/access-loss scenario. PostgreSQL checks reconcile known histories, regular/bulk close/reopen, date boundaries, tenant/private-inbox isolation, revoked membership and moved cases, future timestamps, forged authorization scope, migration rollback, and oversized-group rejection. The exact production query used both indexes on a selective range against 10,000 older cases/events.
- [ ] Analytics release checks: apply migration to the running environment and verify representative wide-range/shared-database load. The local index-plan test is not a production capacity measurement. Identity-provider and deployed full-stack acceptance remain open.


Acceptance:

- AI work has observable queued/running/completed/failed behavior and never reports success without saved results.
- Reports and exports retain tenant/inbox authorization and reproducible source references.
- Analytics totals reconcile to fixtures and known case-event histories.

## Data and contract decisions to settle during implementation

These are design tasks, not reasons to delay Phases 1–2 preparation:

| Decision | Recommended starting point |
| --- | --- |
| Identity provider and user directory | Integrate the system's chosen identity source; case-manager owns inbox membership, not credentials. Establish this before production access. |
| Decision membership | Match Marble's validation behavior where required; explicitly choose whether a decision can belong to multiple cases, then enforce the choice transactionally. The current unique index only prevents duplicates within one case. |
| Screening/match identity | Keep screening linkage and match review history distinct; use stable source event IDs for retries. |
| Grouping selection | Document a deterministic policy and compare with Marble's `findBestMatchCase`; the new code currently selects the newest matching case. |
| Close requirements | Define allowed unset outcomes for each case type, required comments, and unresolved screening behavior before finalizing UI validation. |
| Queue implementation | Align with the existing River migration direction after checking actual adoption; avoid a second ad hoc retry mechanism. |
| Files and retention | Choose the shared blob/storage interface and retention policy before enabling uploads. |
| First release boundary | Phases 1–3 provide the first functional investigation release. Phase 4 completes operational workflows; Phase 5 is advanced parity. |

Use forward migrations rather than rewriting `000001_init` for databases already initialized. Preflight existing orphan/cross-tenant/duplicate relationships, then add constraints and indexes. Include rollback and compatibility notes for each schema/API change.

## Verification and release gates

The following checks are release targets. The original document-only review did not run them. The Phase 1 increment above adds and runs the core unit/HTTP and isolated PostgreSQL checks; Phase 2 adds producer/consumer contract and durable-delivery tests; browser and full-stack release checks remain pending.

1. Service tests: validation, complete rollback on injected failures, authorization, mutation side effects, idempotency and concurrent updates.
2. PostgreSQL integration tests: fresh and upgraded schemas, tenant-consistent foreign keys, pagination, grouping locks, assignment claims, and query plans at realistic case volumes.
3. Producer/consumer contract tests: decision workflow dispatch, authenticated screening callbacks, missing configuration, retry/replay and evidence persistence.
4. Browser tests: inbox setup, manual/automatic creation, investigation evidence, comment/tag/assignment, snooze/escalate, close/reopen, inaccessible cases and tenant switching.
5. Operational checks: Compose startup, readiness, graceful worker shutdown, database/service outages, retry visibility, and audit/outbox reconciliation.

Run `go test ./...` from each changed Go service. Use the frontend's existing `npm run lint` and `npm run build`; add a browser-test harness because its current package scripts do not include one. Avoid treating passing domain tests alone as proof that case management works.

First release gate: no hardcoded case rows; no silent relationship/audit failures; correct tenant/inbox access; replay-safe workflow intake; working decision-to-case-to-closure journey in the default local stack. Advanced options must not appear operational until their handlers and workers are implemented.

## Source map

Paths below are relative to this document. Proposed frontend files above do not exist yet.

- [New case domain](backend/case-manager-service/internal/domain/case/model.go) and [existing tests](backend/case-manager-service/internal/domain/case/model_test.go).
- [Business operations](backend/case-manager-service/internal/service/case_service.go), [repositories](backend/case-manager-service/internal/store/postgres/repositories.go), and [initial schema](backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql).
- [Routes](backend/case-manager-service/internal/httpapi/router.go), [case handlers](backend/case-manager-service/internal/httpapi/handlers/cases.go), [integration handlers](backend/case-manager-service/internal/httpapi/handlers/integrations.go), [authentication](backend/case-manager-service/internal/httpapi/middleware.go), and [actor/error helpers](backend/case-manager-service/internal/httpapi/handlers/helpers.go).
- [Worker scaffold](backend/case-manager-service/cmd/worker/main.go) and [Compose wiring](docker-compose.yml).
- [Current Cases page](frontend/src/app/(authenticated)/cases/page.tsx) and [frontend instructions](frontend/AGENTS.md).
- [Decision workflow model](backend/decision-engine-service/internal/domain/workflow/model.go) and [dispatcher](backend/decision-engine-service/internal/clients/dispatch/http_client.go).
- [Screening case client](backend/screening-service/internal/clients/case/http_client.go), [inbox client](backend/screening-service/internal/clients/inbox/http_client.go), and [screening publisher call sites](backend/screening-service/internal/service/screening_service.go).
- [Marble lifecycle model](../api/models/case.go), [business rules](../api/usecases/case_usecase.go), [automatic grouping](../api/usecases/decision_workflows/actions_case.go), [automatic assignment](../api/usecases/auto_assignment_usecase.go), and [case access checks](../api/usecases/security/enforce_security_case.go).
- [Marble frontend operation surface](../front/packages/app-builder/src/repositories/CaseRepository.ts).
