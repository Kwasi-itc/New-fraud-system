# Auth planning coverage review

Reviewed 2026-10-06 against current repository source. This is a documentation
cross-check, not a security certification or runtime verification. The provider
and organization policies remain undecided. No application code was changed.

## Findings and follow-up

| Evidence | Finding / correction | Tracked work |
| --- | --- | --- |
| [Tenant schema](../data-model-service/internal/migrations/metadata/000001_init.up.sql), [tenant service](../data-model-service/internal/service/tenant_service.go), [case FK](../case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql) | Data Model already owns `core.tenants`; create and provision are distinct operations. Bootstrap must reuse this owner and define readiness. | X01, U02, M01 |
| [Case proxy](../../frontend/src/lib/server/case-proxy.ts) | Opaque sessions require a private resolution/token contract, not just replacing the cookie value. | X02, S01 |
| [Data Model client](../../frontend/src/lib/data-model-api.ts), [ingestion client](../../frontend/src/lib/ingestion-api.ts), [decision client](../../frontend/src/lib/decision-engine-api.ts), [Data Model auth](../data-model-service/internal/httpapi/auth.go) | Browser clients support public service-token variables; owner middleware supports disabled/shared-token modes. Actual deployed secrets were not inspected. A login-only frontend guard cannot secure alternate endpoints. | X03, D12, W08 |
| [Evidence proxy](../../frontend/src/app/api/case-evidence/[tenant]/[caseId]/[kind]/[resource]/route.ts) | Human Case authorization and service-authorized evidence retrieval occur in separate requests. Specify read consistency and revocation races explicitly. | X04, D10 |
| [Invitation tasks](IMPLEMENTATION_TODO.md) I01-I04 | Hash-only acceptance secrets cannot be recovered for deferred mail. Delivery needs a separately protected transient secret strategy and retry tests. | X05 |
| [Tracker](IMPLEMENTATION_TODO.md) F06/T06 | Rate limits were mentioned in errors/tests without a concrete multi-replica implementation task. | X06 |
| [Tracker](IMPLEMENTATION_TODO.md) L08/S04/U05 | Platform suspension and provider-side account/session changes are distinct. Define synchronization and limitations. | X07 |
| [Verifier](../investigator/jwt.go) | Verification currently also requires case-role membership. Reuse cryptographic verification carefully; do not force identity admins or every platform user into a case role. | X08 |
| [Page context](../../frontend/src/lib/server/case-page.ts) | Tenant comes from URL/environment fallback and session partition uses token hashing. Review both when switching to opaque sessions. | X09 |
| [README](README.md) and [tracker](IMPLEMENTATION_TODO.md) | Optional enterprise/native-credential features need explicit scope dispositions rather than silent omission or assumed MVP inclusion. | X10 |
| [Verifier](../investigator/jwt.go), [case auth wiring](../case-manager-service/internal/app/auth.go), [Compose](../../docker-compose.yml) | Key configuration and mounted files are not proof of live key reload. Rotation must cover actual running consumers. | X11 |
| [Case release gates](../../CASE_MANAGEMENT_IMPLEMENTATION_PLAN.md) REL01/REL04 | Fixture tests and separately passing service tests do not complete live sign-in/investigation acceptance. Sequence must not defer security dependencies needed by earlier slices. | X12, G04 |

## Coverage checklist

| Requirement family | Existing tracker coverage | Cross-check additions |
| --- | --- | --- |
| Provider selection, credentials, MFA/recovery | D01-D07, L01-L09 | X07, X10 |
| Accounts, stable IDs, tenants, roles, bootstrap | U01-U08, M01-M08 | X01, X08 |
| Sessions, logout, expiry, revocation, concurrency | S01-S11 | X02, X04, X09 |
| Invitations and mail | I01-I06 | X05, X06 |
| Case directory, capacity, reassignment, attribution | C01-C10 | X01, X04, X08 |
| Frontend login/admin UX and alternate API exposure | W01-W09, D12 | X03, X09 |
| Shared DB ownership, migrations, indexes, pools | F01-F07, M01-M08 | X01, X02 |
| Logs, audit, events, operational recovery | O01-O07 | X05-X07 |
| Docker, secrets, keys, rollout, restore | R01-R08 | X03, X11 |
| Tests, full-stack acceptance and release evidence | T01-T07, G01-G06 | X12 and corrected phase dependencies |

All X tasks are open in [IMPLEMENTATION_TODO](IMPLEMENTATION_TODO.md). Existing
policy decisions remain open rather than being invented during this review.
Recheck this inventory after provider selection and before implementation; newly
selected provider capabilities may create additional configuration requirements.
