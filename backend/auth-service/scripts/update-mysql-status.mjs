import assert from 'node:assert/strict';
import {readFileSync,writeFileSync} from 'node:fs';
import {relative,resolve} from 'node:path';

const nativeDirectory=resolve(process.argv[2]??readFileSync('evidence/mysql-integration/latest.txt','utf8').trim());
const liveDirectory=resolve(process.argv[3]??readFileSync('evidence/mysql-endpoint-audit/latest.txt','utf8').trim());
const native=JSON.parse(readFileSync(resolve(nativeDirectory,'report.json'),'utf8'));
const live=JSON.parse(readFileSync(resolve(liveDirectory,'report.json'),'utf8'));
assert.ok(native.passed&&native.nativeOperationsVerified===14&&native.missingTests.length===0);
assert.ok(live.passed&&live.runtimeUnchanged&&live.successfulOperations===14&&live.temporaryProviderUsersRemoved===3);
const link=path=>relative(process.cwd(),path).replaceAll('\\','/');
const nativeLink=link(resolve(nativeDirectory,'report.json')),liveLink=link(resolve(liveDirectory,'report.json'));
const common=`The [full native report](${nativeLink}) passes **${native.topLevelPassed} top-level tests / ${native.testAndSubtestPasses} test and subtest results**, with all ${native.expectedTopLevelTests} independently inventoried tests accounted for. Vet passes, with zero failed/skipped tests and zero race warnings. Six packages compile without test files; the API executable is also exercised by the process suite. The shared MySQL module separately passes vet and ${native.sharedTopLevelPassed} top-level tests. Private target/reference MySQL and PostgreSQL characterization fixtures are distinct from application databases.`;
const coverage={
'/healthz':'Liveness remains available when the database pool is closed',
'/readyz':'Applied migration state/count and exact table grants; missing grants and dependency outage rejected',
'/internal/login/start':'Persisted digests, nonce and PKCE; proxy and origin rejection',
'/internal/login/complete':'Real Keycloak code exchange, single-use state/binding, explicit membership and tenant selection',
'/internal/session':'Signed platform JWT, current role/version, tenant/expiry/revocation, idle renewal and read-only sessions',
'/internal/logout':'Idempotent atomic revocation/audit, including logout while inactive and later tenant reactivation',
'/internal/sessions/list':'Current user/tenant only; safe projection and stable bounded cursor',
'/internal/sessions/revoke':'One/all, ownership, foreign tenant denial, immediate revocation and durable audit',
'/internal/members/list':'Admin authorization, foreign isolation, 101-row sentinel and tenant-key query plan',
'/internal/members/change':'Version conflict, last-admin race, post-lock expiry, session revocation and audit rollback',
'/internal/invitations/start':'Token/issuer/expiry binding and real provider acceptance',
'/internal/invitations/list':'Admin/tenant scope, safe projection, expired and delivery status, no secret material',
'/internal/invitations/change':'Create/replay/conflict, pending-email uniqueness, resend/cancel, stale generation and rate bounds',
'/internal/audit/list':'Admin/tenant scope, JSON details and deterministic 101-row sentinel'
};
writeFileSync('MYSQL_ENDPOINT_STATUS.md',`# Auth MySQL endpoint status

Updated ${live.at}. The local MySQL API is running at **http://localhost:8087/docs**.
All **14 existing operations** are implemented and have positive native and live
HTTP acceptance. Three additional runtime routes provide docs, OpenAPI and capabilities.

The [live report](${liveLink}) passes **7/7 suites across ${live.requestCount} HTTP requests**,
including actual existing Keycloak login, signed platform sessions, two-tenant
isolation, real SMTP capture, invitation acceptance, role changes and revocation.
All 14 documented operations have successful assertions. Expected 401/403/409
responses are separate negative coverage and never count as successful endpoints.
The API/worker image identities are retained; the final image is unchanged during
the audit. All three temporary provider users were removed, and issued test
sessions were logged out/revoked. Synthetic MySQL identities, audit and mail remain.

${common}

| Operation | MySQL | Asserted coverage |
| --- | --- | --- |
${live.matrix.map(op=>`| ${op.method} ${op.path} | [x] Native + live | ${coverage[op.path]} |`).join('\n')}

## Authentication and scope

Every internal operation requires the private proxy bearer credential. Session
bodies supply auth-issued opaque credentials; caller-provided actor/tenant/role
assertions do not grant access. Login never provisions users or auto-links email.
Operator provisioning is explicit, and a configured login tenant cannot fall back
to another tenant. With no default, the source first-active-membership selection
is preserved. Invitations select their own authorized tenant.

The frozen 14-step identity contract used by decisions is preserved; seven new
auth tables have their own ledger. Issued sessions work with the existing MySQL
decision session verifier. Runtime readiness checks counts/state and restricted
grants; full definition/checksum drift verification belongs to the installer.

## Evidence boundaries

The initial [full-suite failure](evidence/mysql-integration/2026-11-09T09-53-39Z/raw.jsonl)
is retained: an older PostgreSQL preflight test's deliberate DROP needed CASCADE
for newer fixture foreign keys. The correction is confined to its disposable
database, and the final complete suite passes. The initial live callback-capture
timeout is retained under evidence/mysql-endpoint-audit; the final browser runner
waits for the actual redirect before consuming its code.

Keycloak storage remains PostgreSQL by the user's choice. Application auth state
uses MySQL. This is local development acceptance, not production data transfer,
historical PostgreSQL user/session import, frontend cookie/proxy cutover, provider
disaster recovery, invitation capacity or sustained throughput certification.
PostgreSQL entrypoints and preflight tools remain separate. See
[plan](MYSQL_PLAN.md), [Docker setup](MYSQL_DOCKER.md) and [worker status](MYSQL_WORKER_STATUS.md).
`);
writeFileSync('MYSQL_WORKER_STATUS.md',`# Auth MySQL worker status

Updated ${live.at}. The dedicated **auth-worker** is healthy against the shared
MySQL fraud database. It runs the existing invitation-delivery behavior separately
from the API, plus bounded expired-login retention. It is not a River worker and
publishes no application port; its readiness listener is private.

${common}

The [live report](${liveLink}) verifies persisted sent delivery, captured SMTP mail
and successful invitation acceptance using real Keycloak identities on the same
final image as the API. Docker health alone is not treated as delivery proof.

| Capability | Passing evidence |
| --- | --- |
| Email delivery and secret cleanup | Encrypted payload, real SMTP process capture, sent receipt and payload clearing; native lifecycle/process tests and live invitation suite |
| Competing claims | Two concurrent claimers/recovery calls yield one send for the live lease; TestMySQLInvitationWorkerRetriesFencesAndRecovery |
| Retry/exhaustion | Five failed sends, retry eligibility and terminal failed state with erased payload |
| Lease recovery/fencing | Expired sending lease reclaimed; cancelled/stale completion returns ErrDeliveryFenced and cannot overwrite current work |
| Resend/cancellation | Current invitation version, resend cooldown, renewed digest/generation and cancelled-token rejection |
| SQL/network separation | Cancellation commits while a test sender is blocked; no SQL lock spans SMTP |
| Restricted role | Actual worker can send and purge logins, but cannot read session digests/memberships or mutate invitations/audit; TestMySQLRestrictedRolesAndLoginRetention |
| Login retention | Exactly 100 expired attempts per pass, remaining expired attempts handled next, current login preserved |
| Process lifecycle | Real API/worker start, SIGTERM cancellation/join, worker restart, durable session across API restart; TestMySQLAPIAndWorkerProcessesRestartAndDeliver |

## Execution policy and limits

One handler runs per worker process, polling every two seconds. It claims using
indexed state/due-time or state/lease-expiry ranges and SKIP LOCKED, then commits
before network I/O. Leases last one minute; each iteration has a 25-second deadline.
Results require the same generation, unexpired lease and sending state. Failed
sends use minute backoff of 1/2/4/8 before the fifth attempt exhausts the job.
API/worker pools are capped at five/three connections, respectively, within the
shared database budget. Worker readiness validates required table grants.

SMTP is at-least-once: a crash after accepted DATA can duplicate an email. Lease
fencing does not retract mail already delivered. Invitation acceptance and its
membership/session/audit effects remain single-use and atomic. The suite tests
expired leases and stale completion in native owners; it does not claim a forced
in-flight SMTP process crash or byte-level lost-COMMIT-ack fault injection.

The worker does not delete sessions, memberships, invitation history or audit.
Production SMTP, provider storage migration, mixed-service capacity and release
cutover remain separate gates. See [setup](MYSQL_DOCKER.md) and [endpoint status](MYSQL_ENDPOINT_STATUS.md).
`);
let plan=readFileSync('MYSQL_PLAN.md','utf8');
plan=plan.replace(/\nExecution(?: status|:)[\s\S]*$/,'');
plan+=`\nExecution: steps 1-7 are complete for the local development profile. The
[native report](${nativeLink}) and [live report](${liveLink}) pass. The user selected
reuse of existing Keycloak, including its current storage. Production transfer,
frontend/consumer cutover and release gates are explicitly outside these checks.
Exact coverage and remaining limits are in [endpoint status](MYSQL_ENDPOINT_STATUS.md)
and [worker status](MYSQL_WORKER_STATUS.md).\n`;
writeFileSync('MYSQL_PLAN.md',plan);
console.log('Updated auth plan, endpoint status and worker status from passing native/live evidence.');
