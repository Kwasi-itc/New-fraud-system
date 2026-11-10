# Screening service MySQL migration

The application uses the shared MySQL `fraud` database. Extend the existing
`screening` owner lineage and preserve decision execution bindings, intake
receipts, publication versions, source views and projection queues. PostgreSQL
entrypoints and characterization tests remain available. Keycloak is unchanged.

`[x]` means evidenced completion; `[ ]` means pending implementation or acceptance.

## Implementation and local acceptance

- [x] Inspect the source API, service ports, PostgreSQL repositories and existing
  MySQL publication/intake owner contracts.
- [x] Inventory every endpoint and worker into individual status checkboxes.
- [x] Append lifecycle, continuous-run provenance, evidence, exclusions and
  delivery storage to the existing owner lineage; verify on independent MySQL.
- [x] Implement native typed repositories with bounded reads, tenant-consistent
  relationships, short transactions and current identity checks.
- [x] Reuse source domain logic and HTTP handlers; preserve private operator,
  service-integration and investigator authorization boundaries.
- [x] Implement durable dispatch, monitored-object processing, dataset jobs,
  decision projection and case delivery with recovery and fenced claims.
- [x] Provide explicit local development matching/evidence adapters; never claim
  synthetic matches establish real sanctions-provider coverage.
- [x] Publish authoritative manual, continuous, decision-arrival and file
  contracts needed by case management; verify the five integration workflows.
- [x] Add restricted runtime accounts, Docker setup and docs at port 8085.
- [x] Verify positive behavior for every endpoint, denial/isolation, rollback,
  replay, concurrency and migration compatibility in disposable fixtures.
- [x] Run the complete screening suite with PostgreSQL characterization, race
  detection and vet, then live Docker/auth/Swagger acceptance.
- [x] Update endpoint and worker checklists only from passing evidence.

- [x] Verify checkpointed 50-object rescreen pages with 625 eligible objects,
  saved provider snapshots, replay exclusion and stale-claim fencing.
- [x] Exercise HTTP workflows and native workers with the exact restricted
  installer grants; reject DDL and inactive-tenant dispatch.
- [x] Verify Docker evidence upload, worker callback, case import/download and
  duplicate manual import using actual runtime accounts.

## Separate release acceptance

- [ ] Validate real provider credentials, datasets, coverage and operating policy.
- [ ] Verify representative performance and shared database resource budgets.
- [ ] Reconcile historical data and queues before production handover.
- [ ] Verify production integration, recovery and frontend cutover.
- [ ] Accept any handover of decision-owned `local` provider traffic; it still
  uses the existing private provider and is excluded by the standalone worker.

Credentials and local runtime state stay under ignored `.local/`. Production
readiness and real-provider acceptance are not established by synthetic tests.

Verified local acceptance: 107 top-level tests / 211 test and subtest passes;
zero failures, missing/skipped tests or race warnings; vet passed. All 50 native
mounted operations have positive HTTP evidence with restricted runtime accounts,
covering all 47 source operations plus JSON docs and local evidence capabilities.
All 41 protected operations reject unauthenticated requests. The real browser
renders 47 operations; Docker smoke also verifies auth, replay, worker-persisted
matches and actual screening-to-case evidence delivery/download.

Evidence: [complete report](evidence/mysql-integration/2026-11-09T23-15-03Z/report.json),
[HTTP operations](evidence/mysql-http-results.json),
[live smoke](evidence/mysql-runtime-smoke.json). Case regression passed
98 top-level tests / 190 test and subtest results, including all five
screening contracts, for 59 of 61 positive workflows. Two existing MySQL case
placeholders remain: AI review and the explicit assignment-run endpoint.
