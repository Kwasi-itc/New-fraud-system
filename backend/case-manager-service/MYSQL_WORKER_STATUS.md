# Case management MySQL worker status

Verified 2026-11-09 using retained MySQL target/reference tests and the local runtime. Latest read-only API/Swagger smoke: 2026-11-09T23:15:57.711Z.

`[x]` records implemented and evidenced behavior. Delivery implementation and live delivery activation have separate checkboxes; passing a fixture does not mean an external publisher is configured.

## Implemented and fixture-verified

- [x] Unfinished-upload expiry: bounded claims and deletion of expired, unfinished uploads.
- [x] Snooze wake-up: case update, audit event and outbox commit atomically; repeat runs produce no duplicate event.
- [x] Automatic assignment: active investigator/inbox checks, capacity enforcement and concurrent claims; canonical UUID and suspended-assignee regressions covered.
- [x] Notification transport: durable case/inbox outboxes, short claims, HTTP outside transactions, stable event idempotency headers and redirect rejection.
- [x] Notification recovery: 60-second leases, fenced completion, retry/backoff, terminal failure after 12 failed attempts, and explicit tenant/event replay.

## Local activation

- [x] Case API and maintenance worker are healthy in Docker with restricted database accounts.
- [x] Maintenance loop runs upload cleanup, snooze wake-up and automatic assignment.
- [ ] Configure a publisher and enable outbound delivery. `PublisherURL` is currently empty; notifications remain pending.
- [ ] Verify delivery, receiver deduplication and failure recovery against the configured external publisher.

## Remaining functionality and release checks

- [ ] Implement AI case-review functionality; its existing source endpoint still returns `501`.
- [ ] Implement the explicit internal auto-assignment run endpoint, which still returns `501`. Automatic assignment already runs in maintenance.
- [ ] Verify production worker capacity, process-failure recovery and any PostgreSQL queue-transfer/cutover procedure before release.

The periodic MySQL loop derives maintenance work from persisted domain rows. PostgreSQL River jobs are not copied into it. Restart safety comes from transactional state changes and durable delivery leases. Existing PostgreSQL entrypoints and tests remain available.

Case events and notifications commit or roll back together. Inbox notifications have a separate transport because the existing case outbox enforces case ownership through foreign keys. Receivers must deduplicate event UUIDs; the worker provides at-least-once delivery.

See the [complete test report](evidence/mysql-integration/2026-11-09T22-56-11Z/report.json), [local runtime evidence](evidence/mysql-runtime-smoke.json), [endpoint checklist](MYSQL_ENDPOINT_STATUS.md) and [operator instructions](MYSQL_DOCKER.md). No external publisher acceptance, production queue transfer or throughput target is claimed.
