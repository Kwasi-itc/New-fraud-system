# Auth MySQL worker status

Updated 2026-11-09T10:30:07.545Z. The dedicated **auth-worker** is healthy against the shared
MySQL fraud database. It runs the existing invitation-delivery behavior separately
from the API, plus bounded expired-login retention. It is not a River worker and
publishes no application port; its readiness listener is private.

The [full native report](evidence/mysql-integration/2026-11-09T10-40-03Z/report.json) passes **42 top-level tests / 69 test and subtest results**, with all 42 independently inventoried tests accounted for. Vet passes, with zero failed/skipped tests and zero race warnings. Six packages compile without test files; the API executable is also exercised by the process suite. The shared MySQL module separately passes vet and 47 top-level tests. Private target/reference MySQL and PostgreSQL characterization fixtures are distinct from application databases.

The [live report](evidence/mysql-endpoint-audit/2026-11-09T10-29-40.911Z/report.json) verifies persisted sent delivery, captured SMTP mail
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
