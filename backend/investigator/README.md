# Investigator review contract

Platform identity update (2026-10-06): Compose sets `USER_REQUIRE_SESSION=true`.
All human credentials require a platform UUID `sid` and positive `authz_version`.
After signature verification, the transaction invokes the auth-owned
`identity.authorize_case_v1` contract before receipt/case/inbox/resource locks.
It checks active user, tenant, membership/current role/version and unrevoked,
unexpired session; identity dependency errors return 503. Shared locks make
committed suspension/logout authoritative for subsequent actions and replays.
Apply auth schema migration before strict-mode consumers. Legacy session-less
verification is retained only for explicitly non-strict integrations/test fixtures.
See [runtime/provisioning contract](../auth-service/PLATFORM_AUTH.md).

This local Go module provides the shared human JWT verifier and the explicit case-authority transaction used by the decision and screening owners. It is independent of Marble. Case-manager retains ownership of case history through `case_manager.record_investigator_review`; owners do not issue arbitrary case mutations. The deployment currently uses shared database credentials, so these are service ownership boundaries, not database-role isolation.

## HTTP contract

Both owner APIs expose `GET` and `POST /v1/investigator/tenants/:tenantId/cases/:caseId/review`. The decision owner accepts decision UUIDs; the screening owner accepts match IDs. GET uses `?resource_id=...`. POST accepts only:

```json
{"operation_id":"UUID","resource_id":"owner-resource-ID","expected_version":"version from GET","action":"approve","findings":"Investigator findings"}
```

The response is `{status, version, eligible, actions, machine_outcome?}`. Decision actions are `approve`/`decline`, initially eligible only for machine `review` or `block_and_review`. Human status is stored separately; machine outcome is never rewritten. Match actions are `confirmed_hit`/`no_hit`, initially eligible only while pending. Findings are required, trimmed, and limited to 10000 UTF-8 bytes; the request is limited to 16384 bytes. Unknown fields, including actor IDs or whitelist flags, are rejected. No correction, dismissal or whitelist command is exposed by this contract.

The original signed human bearer token is required even if the owner's legacy service authentication is disabled. Configure `USER_JWT_KEYS_FILE` (JSON key-ID to RSA public PEM map), `USER_JWT_ISSUER`, and `USER_JWT_AUDIENCE` identically to case-manager. All absent disables these routes with 401; partial/invalid configuration prevents startup. Accepted roles are `case_investigator` and `case_manager_admin`. The tenant claim must match, including for admins. The Next proxy forwards the session token, never the owner service credential, and enforces same-origin writes.

Every read, write and replay verifies current case access and the exact current resource relationship. New writes require an open case in an active inbox. An administrator may bypass inbox membership, not tenant/link/lifecycle validation. A replay after closure can acknowledge the already committed receipt; removal of access or transfer away from that case still denies it. Review does not assign, wake, transition or close a case.

Errors: 400 malformed command/ineligible case/action; 401 invalid user JWT; 403 current tenant/case/inbox/link access denied; 409 stale state, already reviewed resource or operation conflict; 500 database/publication failure. A 500 or lost response does not establish whether commit occurred: retry the **same operation ID and payload**. Operation IDs bind actor, case, resource, expected version, action and normalized findings. Another payload conflicts. Owner receipts are immutable and unique per tenant/resource for this initial-review contract.

## Atomicity and synchronization

The short database transaction takes operation, case, inbox, then owner locks; it checks membership/link while locked, applies owner business logic, records the owner receipt, and invokes the case-owned SQL function. Owner change, actor/findings history, case link status and case notification outbox either all commit or all roll back. The SQL function does not elevate privileges. Case lifecycle/capacity logic remains with existing case commands. No network calls occur under these locks.

Screening aggregates sibling statuses under its existing screening-row lock. Decision-linked source changes now atomically enqueue durable projection work. The worker reconciles current owner state through a decision-owned SQL function; stale callbacks cannot overwrite it. Investigator success means source review and case history committed, with secondary execution synchronization queued. The browser no longer retries an unrelated callback failure. Continuous/freeform screenings enqueue no decision execution projection. See [projection operations and rollout](../screening-service/EXECUTION_PROJECTION.md).

Legacy screening service-token review remains a separate trusted integration endpoint with its previous correction/whitelist semantics. It is not exposed through the investigator proxy and does not become a verified human command. Disabling/replacing that integration surface requires a separate compatibility decision.

## Build and rollout

The three consuming Go modules use `replace .../backend/investigator => ../investigator`. Build Docker images from **backend**, e.g. `docker build -f screening-service/Dockerfile .`; Compose uses this context for APIs, workers and migrators. Local `go test ./...` still runs from each service directory.

Apply decision-engine migrations through `000019`, screening through `000007`, then case-manager through `000013`, after their existing prerequisites, in the same logical database. Deploy schema before owner APIs and deploy owner APIs before the frontend. Compose mounts trusted public keys into all three APIs. Prior membership/data preflights and intended-environment release gates still apply. No historical reviews are backfilled as verified human actions. Owner down migrations refuse populated review history; drain new commands before removing the case function. Do not drop audit data to force rollback.

Verification lives in decision service eligibility tests, existing case JWT tests, screening `investigator_journey_integration_test.go` (both real owner servers against disposable shared PostgreSQL), and frontend `tests/cases/workspace.spec.ts`. The browser fixture verifies UI/proxy behavior; it is not a real identity provider or production backend.
