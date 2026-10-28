# Application identity and external provider

Status: **NEEDS_REVIEW** (design); implementation **NOT_STARTED**. Primary task: AU-01; prerequisites: SC-01, DB-01. Findings: PG-11/17/21/23/29/35.

## Verified execution path

[Source entry point](../../../backend/auth-service/internal/platform/store.go#L36). Auth owns embedded identity migrations, users/external identities, memberships/roles, sessions/login transactions, audit and invitations. Local SQL authorization routines are used by case/model/investigator mutations. OIDC verifies external identity but does not own local tenant permissions. Full source occurrences are in the [audit catalog](../../11_source_evidence_catalog.md).

## Target implementation

Normalize roles, preserve session digest and membership references, current expiry/version/tenant checks and shared locks. P-06 guards last-admin and issuer/subject races. Invitation pending-email uniqueness is conditional and delivery generations fence stale workers. Port preflight read-only snapshot/limits and embedded migration recovery. AU-02 separately decides Keycloak release, storage and state transfer: current official support includes MySQL8.4, but that does not certify the pinned 26.8.0 deployment or every installed extension.

Use the common [schema contract](../04_schema_and_data_access_design.md), [security requirements](../05_security_and_integrity_requirements.md) and [transaction protocols](../07_transaction_and_recovery_protocols.md). These are required acceptance inputs, not already implemented helpers.

## Implementation checklist

- [ ] **AU-01.1 ? NOT_STARTED:** Map embedded schema versions and authorization routines, every role check and digest/issuer/subject/invitation key into SC-01.
- [ ] **AU-01.2 ? NOT_STARTED:** Implement current locked authorization, session/login/invitation repositories, narrow admin/identity guards and bounded preflight.
- [ ] **AU-01.3 ? NOT_STARTED:** Retain trusted actor integration for other modules; validate role/version invalidation, cross-tenant membership and token expiry after waits.
- [ ] **AU-01.4 ? NOT_STARTED:** Complete AU-02 provider plan: supported migration, namespace/privileges, issuer/subject continuity, omitted session/revocation state and explicit reauthentication policy.

## Tests and acceptance

POC-08; reuse internal/platform/sessions_test.go and login_tenant_test.go plus data-model platform_auth_integration_test.go and case identity_integration_test.go. Add simultaneous last-admin removal, membership revoke-vs-review, expired lock-wait sessions, stale invitation generation and provider refresh/logout tests.

No authorization cache weakens revocation. Provider export is not treated as full state backup. A separate provider database needs explicit architecture approval; no vendor table renaming without supported configuration.

## Delivery and rollback dependency

Implement and validate in disposable environments. All shared consumers must be compatible before live activation; an individual module passing its suite does not authorize a partial cross-engine production cutover. Preserve old adapters/compatible API artifacts through rehearsal as needed, with a single chosen runtime authority. The [roadmap](../10_implementation_roadmap.md) owns task dependencies and [cutover plan](../11_data_transfer_and_cutover.md) owns data rollback. Module schema rollback cannot undo externally delivered effects.
