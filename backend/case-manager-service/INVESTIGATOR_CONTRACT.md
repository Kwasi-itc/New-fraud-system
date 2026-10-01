# Investigator workspace (Phase 3)

## Identity and deployment

The `/cases` pages use the same-origin Next.js `/api/cases` boundary. It forwards the user's signed JWT from an HttpOnly cookie to the case API; it never substitutes the service integration token for a human. Case-manager verifies signature, issuer, audience, expiration, tenant claims, and inbox permissions on every request. Client query keys include tenant and a non-secret session fingerprint; queries are not persisted and revalidate on focus. A tenant switch remounts the workspace.

An identity provider or trusted gateway must establish `__Host-case_session` with `Secure`, `HttpOnly`, `SameSite=Lax` (or stricter), `Path=/`, no Domain, and expiry no later than the signed JWT. Configure `CASE_SESSION_COOKIE` only when integrating an existing secure session-cookie contract. The existing login page does **not** yet establish this session: identity-provider integration is pending provider details. Do not expose a shared token, use unsigned actor headers, or add a development authentication bypass. A missing session returns 401.

Frontend runtime settings:

| Setting | Purpose |
| --- | --- |
| `CASE_MANAGER_SERVICE_URL` | Server-only case API base URL, Compose `http://case-manager-service:8086` |
| `CASE_DEFAULT_TENANT_ID` | Optional initial tenant; never authorization |
| `CASE_SESSION_COOKIE` | Default `__Host-case_session` |
| `APP_ORIGIN` | Exact externally visible frontend origin; required to match POST/PATCH/PUT/DELETE Origin |
| `DECISION_ENGINE_SERVICE_URL` / `SCREENING_SERVICE_URL` | Server-only evidence owner endpoints, ports 8082 / 8085 in Compose |
| `DECISION_ENGINE_AUTH_TOKEN` / `SCREENING_AUTH_TOKEN` | Optional server-only owner service credentials, matching each owner's auth configuration |

In Compose set `FRONTEND_ORIGIN` to the actual origin, including a non-default forwarded port if used. No case credentials are build arguments or `NEXT_PUBLIC_*` variables. Writes accept only same-origin JSON, with a 1 MiB body bound. Redirects are rejected; responses and upstream fetches use no-store.

## Reads

All paths below are under `/v1/tenants/:tenantId` and require a verified user JWT.

| Endpoint | Contract |
| --- | --- |
| `GET /case-session` | Verified subject, tenant ID, and administrator flag |
| `GET /case-queue` | `{cases, counts, next_cursor?}`; counts reflect all matching records before cursor filtering |
| `GET /cases/:caseId/overview` | Case metadata, tags and contributors; histories are separate |
| `GET /cases/:caseId/links/:kind` | `{items,next_cursor?}` for events, decisions, screenings, files |
| `GET /cases/:caseId/evidence-access/:kind/:resourceId` | 204 only for an authorized case with that exact decision/screening link |

Queue filters: repeated `status` and `inbox_id`, literal substring `name`, `assignee_id`, `unassigned`, `include_snoozed`, `tag_id`, `review_level`, RFC3339 `created_from` (inclusive) / `created_to` (exclusive), and `related_to` (an independently authorized source case). Related cases share a nonempty authoritative object identity or a pivot through decision links. Results always enforce the caller's current inbox permissions, including counts.

Queue order is boosted first, then `created_at DESC, id DESC`. The cursor stores all ordering values. Links use `created_at DESC, id DESC`. `limit` is 1–500; the UI requests 50. Next-case navigation carries the original queue filters and requests the first permitted row after the current ordering tuple. Tied timestamps do not omit or repeat rows in a stable dataset. These are live queues, not snapshot exports: concurrent priority/filter changes can move records between pages; refresh to restart the queue.

The queue does one count query, one page query, and one batched tag query. Migration `000005_workspace_indexes` adds ordering and object lookup indexes. The 620-case fixture verifies queue index use without disabling sequential scans. Exact filtered counts still cost work proportional to qualifying data; this is not a production throughput guarantee. Schedule transactional index creation in a maintenance window for large tables.

## Evidence

The Next.js evidence route first asks case-manager to authorize the exact case/resource relationship using the user's JWT, then fetches decision/rule execution details or screening/match details from the owning service with server-only credentials. This is not a generic service proxy. Owner errors appear as unavailable evidence, never as empty history. Network calls happen outside case database transactions. No owner schema is mutated.

Files uploaded through the case workspace support the Phase 4 lifecycle below. Existing screening-source attachments still depend on the screening owner's storage implementation; their metadata is retained and the UI labels them as source storage. The object investigation view is anchored to an authorized source case and displays matching linked decisions; it does not pretend to be a complete customer/KYC directory.

## Mutations and escalation

Existing case/inbox/tag/member APIs power manual creation, rename, assign/unassign, move, comments, tags, snooze/unsnooze, review levels, membership, and archive/restore. Assignment automation is configured in inbox settings; automated reviews remain unsupported. Inbox settings require administrator authority. Moving preserves the assignee, who must be eligible in the destination and have capacity; unassign first if necessary.

## Phase 4 queue operations

Apply migrations `000006_worker_delivery`, `000007_assignment_sla`, and `000008_evidence_uploads`, then the case-owned River migrations (included in `cmd/migrate up`) before starting the new API/worker. Stop workers before downgrade. Migration 000006 refuses in-flight delivery downgrade; 000008 refuses to drop stored uploads/evidence. Empty-schema forward/down/up ordering is covered by integration tests. Schedule index creation and the ALTER TABLE locks in a maintenance window on large databases.

The worker runs River 0.26 in `case_queue` within the same PostgreSQL database. This separates scheduler leadership from other services that do not register case periodic jobs. It does not introduce another application database. The queues are `case_maintenance` and `case_delivery`, each with one running job per replica and uniqueness across active jobs. Separate jobs prevent a slow receiver from delaying wake-ups and assignment. It reconciles persisted state on start and every `WORKER_POLL_INTERVAL` (15 seconds in Compose), with `WORKER_BATCH_LIMIT` (100 in Compose; allowed 1–1000) per operation. Retries are safe because mutations and history/outbox writes commit together. Batch mode runs one bounded cycle and exits with a failure code on error. Compose budgets four pooled worker connections; include River listener connections and every replica in the shared database budget. Mixed-service production load verification remains open.

Automatic assignment considers open, unsnoozed, unassigned cases in active inboxes with auto assignment enabled. Eligible members must also enable auto assignment. It chooses the smallest open-case workload, breaking ties by subject; locked members are skipped by concurrent workers. Capacity is per user **within each inbox**, defaults to 20, and is configured as `capacity` (0–1000) in the membership PUT. Snoozed open cases count toward capacity. Reducing capacity retains existing assignments and prevents additions until workload falls below the limit. A database trigger shares the member lock across automatic/manual assignment, self-assignment on investigation, reopening, and moves. This prevents capacity races. Assignment records `case_auto_assigned` with a system actor and does not impersonate a contributor.

Creation, closing, unassignment, moving, and configuration changes become eligible on the next reconciliation cycle; assignment is eventual, not synchronous. Explicitly unassigning in an enabled inbox can therefore result in reassignment. Under backlog, multiple bounded cycles may be needed. The legacy `/internal/v1/auto-assignment/run` endpoint remains unsupported; periodic River jobs are the supported trigger.

Snooze expiry clears `snoozed_until`, sets `boost_reason=unsnoozed`, and records one `snooze_expired` event in the same transaction. Concurrent workers skip claimed rows. Closed and future-snoozed cases are excluded. Closing a case clears its snooze so it remains visible in closed-case queues. New decision links and ordinary reassignment preserve the current snooze; snoozed cases stay hidden unless explicitly included. Escalation clears snooze and assignment and sets the escalation boost. Expired cases are visible even before the worker runs; the worker adds their attention flag. Queue ordering remains attention first, then newest creation time and UUID. Human investigation actions acknowledge existing attention flags.

Inbox PATCH accepts `sla_days` (1–3650); explicit null disables it. Queue and overview expose `sla_due_at = created_at UTC + sla_days × 24 hours`, following the creation-plus-SLA reference with explicit UTC day semantics. DST and viewer time zone never change the deadline. Moving uses the destination inbox's SLA and editing an SLA immediately changes derived deadlines; there is no reset to move/edit time and no bulk rewrite of cases. Closed cases retain a displayed derived date but are excluded from `overdue=true`. The queue displays deadlines and overdue urgency; the UI renders dates in the viewer's local time. Tests cover offset/date boundaries, leap day, daylight-saving boundary dates, edits, moves, and disabled SLAs.

## Phase 4 file lifecycle

All paths below are beneath `/v1/tenants/:tenantId/cases/:caseId` and require the user JWT and current inbox access:

| Method/path | Contract |
| --- | --- |
| `POST /uploads` | `{file_name,content_type,file_size}` → `{upload}`; creates a session owned by the verified subject, expiring in 24 hours |
| `PUT /uploads/:uploadId/content` | Raw bytes; exact declared length and sniffed type required; identical-content retries accepted before finalization |
| `POST /uploads/:uploadId/finalize` | Creates one attachment, contributor and audit/outbox event atomically; repeat finalize returns the same file |
| `GET /files/:fileId/download` | Finalized bytes only, current case access required; attachment disposition, no-store and nosniff headers |

The case service owns a bounded PostgreSQL byte store in `case_manager.evidence_uploads`. The screening client currently cannot provide working blob storage, so this implementation does not fabricate signed URLs. Allowed types are PDF, PNG, JPEG, plain text and CSV; size is 1 byte–10 MiB. Metadata must match received bytes; SHA-256 is recorded. Only the initiating subject can upload/finalize, even if another user can view the case. Starting/finalizing a new attachment requires an open case; authorized downloads remain available after closing. Binary input is bounded before entering the transaction, and download bytes are materialized before HTTP response delivery, so no network I/O occurs under database locks.

The Next.js proxy accepts bounded binary PUTs only on the content endpoint and streams authorized download responses. It preserves same-origin write enforcement and server-only credentials. The browser retains the upload ID for retry; the worker removes expired unfinished sessions in bounded batches. Finalized evidence is retained. Include byte storage, WAL, backups, and retention in shared database capacity planning; migrating to object storage later must preserve ownership, attachment IDs and hashes. Source screening-file bytes are not copied or invented.

## Bulk actions and tags

`POST /v1/tenants/:tenantId/case-bulk` accepts `{operation_id,case_ids,action,assignee?,inbox_id?,outcome?,comment?}`. Supply a new UUID operation ID for each intended batch and 1–100 unique case IDs. Actions are `assign` (null unassigns), `move`, `close` and `reopen`. Close requires non-unset outcome and findings; reopen requires closed state and sets investigating/unset. Moves retain assignment and require destination access, membership, and capacity.

Each case has its own transaction and current authorization check. The 200 response contains individual `{case_id,success,replayed,error?}` results; it does not imply every case succeeded. Each successful case has one `bulk_<action>` event containing before/after metadata and findings, plus the matching outbox record and contributor. Successful receipts bind operation ID, case, actor and payload. Retrying with the same operation ID and payload skips completed cases and retries failures. Changing that payload conflicts. Cancellation may leave a committed prefix; retry the same operation to determine the outcome. There is no batch-wide rollback. The UI shows results and offers a retry with the saved operation ID.

Tag administrators can PATCH `/tags/:tagId` with `{name,color}` and DELETE the same path to archive. Target cannot change. Archived tags are excluded from active choices and cannot be newly attached; existing case references remain visible, can be detached, and retain previous audit events. Row locks serialize archive against attachment validation. The tag settings page requires administrator authority.

## Outbound delivery operations

Set `CASE_OUTBOX_PUBLISHER_URL` and, when required by the receiver, `CASE_OUTBOX_PUBLISHER_AUTH_TOKEN` for Compose (`OUTBOX_PUBLISHER_URL` / `OUTBOX_PUBLISHER_AUTH_TOKEN` directly). An absent URL explicitly disables delivery and leaves events pending; the worker logs that condition while continuing maintenance. The receiver must durably accept JSON `{id,tenant_id,aggregate_type,aggregate_id,event_type,payload,created_at}` and return 2xx. The same event UUID is the `Idempotency-Key`. Consumers must deduplicate this identity: transport delivery is at least once, not exactly once, and order is not guaranteed across retries.

Claims use a 60-second lease and per-attempt token; a stale worker cannot finalize another worker's claim. HTTP calls have a 10-second timeout, refuse redirects, and occur outside transactions. Failures retry after `2^attempt` seconds, with automatic delivery stopped at 12 failed attempts and `status=failed`; an interrupted in-flight attempt can be reclaimed after lease expiry. Error metadata excludes remote bodies and credentials. Case history remains immutable when delivery state changes.

The operator binary uses database-role access and requires an explicit tenant. It lists up to 100 oldest events of the selected status without their evidence payloads:

```sh
/app/outbox -tenant <tenant-uuid> -status failed
/app/outbox -tenant <tenant-uuid> -status pending
/app/outbox -tenant <tenant-uuid> -replay <event-uuid>
```

Replay accepts failed or delivered events, resets delivery attempts and retains the original identity. Do not replay an active lease; expired leases recover automatically. Check failed rows and pending age during operations. River job state is visible in `case_queue.river_job`; delivery state remains in `case_manager.outbox_events` independently of River job retention. The local HTTP receiver and real River/PostgreSQL integration tests verify delivery and recurrence; a deployed receiver and full-stack operational acceptance remain open.

`POST /cases/:caseId/close` requires `{outcome,comment}` with a non-unset outcome and nonempty findings. Comment and close/audit effects commit together. Reopen uses PATCH to set investigating/unset and retains prior history. Inbox and assignee eligibility are checked on reopening.

`POST /cases/:caseId/escalate` accepts no destination override. It reads the source inbox's configured active destination, authorizes the source, rejects closed cases, locks the case then involved inboxes in UUID order, and rechecks configuration. Destination membership is deliberately not required. It clears assignment/snooze/review level, sets the `escalated` attention flag, adds the actor as a contributor, and records `case_escalated` with previous/new inbox IDs, atomically. It returns 204 so a caller without destination membership receives no destination case data. The UI returns to its permitted queue.

## Verification

Run `go test ./...` and `go vet ./...`; set `CASE_MANAGER_TEST_DATABASE_URL` to enable isolated PostgreSQL tests. Phase 3 tests cover 620-case and 620-event pagination, tie ordering, filters, index use, cross-tenant/private inbox denial, evidence linkage, close/reopen, escalation permission exceptions, and injected audit rollback.

Frontend: `npm run build`, `npm run test:cases`. Browser tests use a controlled HTTP service fixture through the real Next.js proxy. They exercise evidence hydration, comments, close/reload/reopen, empty queues, tenant cache separation, denied inboxes, failed writes, missing sessions, and CSRF protection. They do not certify a live identity provider or deployment; those acceptance checks remain open.


## Internal suspicious activity reports

Migration `000009_reports` builds on the existing report table. Apply migrations through `000008_evidence_uploads` first. Existing report statuses must be `draft` or `completed`; unsupported states fail preflight and require an explicit data repair. Existing rows receive `created_by=legacy_import` rather than fabricated user provenance. Their original JSON remains in `legacy_payload` when the format is unknown, and those reports are read-only. Downgrade refuses new-format or changed reports to prevent loss of provenance/version data.

All routes below are under `/v1/tenants/:tenantId/cases/:caseId`. They require the current tenant and inbox access, including after case moves or membership revocation. Service credentials cannot author human reports. Reports can be prepared after a case is closed; report completion never changes case status or outcome.

| Method and path | Contract |
| --- | --- |
| `POST /reports` | `{id,content}`. Client-generated UUID permits retry of the same initial content by its creator while still at version 1. A conflicting reuse returns 409. |
| `GET /reports?limit=20&cursor=...` | `{reports,next_cursor}`. Limit 1..100, ordered by creation time and UUID descending. |
| `GET /reports/:reportId` | `{report}` with current draft or saved completion snapshot. |
| `PATCH /reports/:reportId` | `{version,content}` replaces draft content at the expected version. Stale or completed reports return 409. |
| `POST /reports/:reportId/complete` | `{version}` completes the saved draft; content must be saved separately. Repeating the successful request by the completing actor returns the same completion without duplicate history. |
| `GET /reports/:reportId/export` | Private attachment download containing the report JSON. Available for drafts and completed reports; status is explicit. |

`content` contains `title`, `subject`, `narrative`, optional RFC3339 `activity_from` and `activity_to`, and `file_ids`. Text limits are 200/1000/50000 UTF-8 bytes respectively. Title is required for a draft. Completion additionally requires nonblank subject/narrative and both activity dates with end >= start. At most 50 unique finalized native case evidence uploads can be attached. Source-owned screening file metadata is not accepted as retained report evidence. Missing/cross-case/unfinalized attachment IDs are rejected with no partial change. The frontend presents dates in UTC.

The supported payload format is `internal_sar_v1`. Completion retains case metadata at completion and evidence IDs, names, sizes, content types and SHA-256 hashes. Evidence bytes remain available through the existing authorized file download endpoint; they are not embedded in report JSON. Linked decision/screening payloads and the full case history are not included in this report export. There is no regulator-specific template, regulator submission, or filing acknowledgment.

Draft edits and completion lock the case and report, check expected versions, and atomically write the report, contributor, full previous/new report history, and matching outbox event. Completed reports have no edit/delete API. History events are `report_created`, `report_updated`, and `report_completed`. Failed audit/outbox writes roll back the report. Authorization is checked before report reads, and completed snapshots do not grant access after inbox membership is removed.

Phase 5 report tests include real PostgreSQL concurrency, audit failure rollback, attachment validation, access revocation, legacy upgrade/downgrade, and indexed pagination with 10,000 report rows. Browser coverage uses the existing service fixture and real Next.js proxy; it does not certify live identity integration or deployed migrations.


## Case analytics

`GET /v1/tenants/:tenantId/case-analytics?from=<RFC3339>&to=<RFC3339>&inbox_id=<optional UUID>` returns totals, per-inbox metrics, daily creation counts/current statuses, and open assignment workload. The frontend entry is `/cases/analytics?tenant=<tenant UUID>`.

The selected population is **cases created in `[from,to)`**. Both timestamps are required, the interval must be positive, and the maximum interval is 366 elapsed days. The UI uses UTC midnight dates; the upper date is exclusive. Daily groups use UTC creation dates and omit days with no cases. Current backlog counts are limited to this population: they do not reconstruct backlog at a historical date or include cases created outside the range.

Permissions, current case state, inbox policy, audit history, and all aggregates are read in one PostgreSQL statement snapshot. `as_of` is the database statement timestamp. Non-admin users see current inbox memberships only. Moving a case changes its reporting inbox and who can see all its metrics, including historical activity. An explicit inaccessible/nonexistent inbox returns 403; an unfiltered user with no permitted cases receives zero totals and empty groups. Client input cannot set the authorization user. No case names, evidence, reports, or investigation narratives are returned.

| Metric | Definition within the selected population |
| --- | --- |
| `total`, `pending`, `investigating`, `closed` | Counts by current lifecycle; total equals the sum of statuses. Open backlog is pending + investigating. |
| `false_positive`, `valuable_alert`, `confirmed_risk`, `unset` | Counts by current outcome across all selected cases; sum equals total. |
| `snoozed` | Open cases with `snoozed_until > as_of`. |
| `sla_configured` | Open cases whose current inbox has an SLA. |
| `overdue` | Open cases with `created_at + current inbox sla_days * 24 hours < as_of`. Snoozing does not pause the deadline. A move or policy change applies immediately. |
| `snooze_events`, `escalations` | Counts of `case_snoozed` and `case_escalated` events in `[from,to)` and no later than `as_of`, for the selected cases, attributed to their current inbox. Repeated actions count separately. This excludes activity for cases created outside the selected range. |
| `measured_closures` | Currently closed cases with a usable latest audited close: `status_updated` to closed or `bulk_close`, no later than `as_of`, no earlier than creation or the latest audited open/reopen. |
| `average_close_seconds` | Arithmetic mean of creation-to-latest-close duration for measured closures, including time before/after any reopenings. It does not subtract snoozing or reset at reopen. Close history can extend beyond the selected creation range. Null if no measured closures; missing history is not substituted with `updated_at`. |
| Assignments | Open-case counts grouped by current inbox and assignee, including a null/unassigned group. Includes snoozed open cases. These are filtered counts, not the full capacity calculation used by assignment. |

Responses reject more than 1000 inbox or assignment groups with a validation error asking for narrower filters. Aggregations cover the full matching population before this check; there is no silent row sampling or partial-success response. The API selects projected metadata and relevant event kinds, not complete case histories. Database/request timeouts remain in effect for expensive date ranges.

Migration `000010_analytics` adds a tenant/creation-date index and a partial index for relevant case-event kinds. Down migration removes these indexes without changing data. Apply through the existing migration command before deployment. Index builds use the existing transaction, five-second lock timeout, and sixty-second statement timeout; validate migration duration and query resource use on production-scale data before rollout.

Verification includes fixture reconciliation for regular and bulk closure, reopening, missing audit history, UTC date boundaries, unknown/empty scopes, forged scope input, tenant/inbox authorization and revocation, current-inbox moves, future audit timestamps, and rejection of oversized groups. The exact production query was explained against 10,000 older cases/events and used both analytics indexes for a selective date range. That check is not a production throughput or wide-range load benchmark. Browser tests exercise filter application, date validation, empty states, and clearing previously displayed metrics after an access failure through the real Next.js proxy and a service fixture.
