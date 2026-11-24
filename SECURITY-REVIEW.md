# Semgrep Findings Review

**Review date:** 2026-11-24
**Input:** `semgrep.sarif` (Semgrep OSS 1.185.0)
**Scope:** All 73 results in the SARIF report, checked against the repository source at review time. No source changes were made as part of this review.

## Summary

The report contains 73 findings across 20 rules. By Semgrep rule defaults, 42 are `error`, 26 are `warning`, and 5 are `note`. The severity labels below are a contextual assessment of security impact, not a direct mapping of Semgrep levels. No critical or high exploitable vulnerability was confirmed.

The main actionable hardening items are:

1. Six backend service images default to root at runtime. Run them as non-root after addressing private-key and runtime-file permissions.
2. The root-tenant setup script communicates with Data Model over Docker-network HTTP and sends a scoped service bearer on later requests. Use TLS/mTLS across untrusted network boundaries, or tightly constrain the Docker network and host-port exposure.
3. Consider replacing SHA-1 for idempotency request fingerprints and index-job deduplication with SHA-256. These uses are not authentication or signature checks, and compatibility must be considered.

Most other results are likely false positives based on their current inputs and use sites. Preserve safe identifier quoting, static upstream allowlists, and output encoding; do not make public gateway routes internal just to satisfy the rule.

### Scan completeness

The SARIF invocation reported syntax warnings for:

- `frontend/Dockerfile:8`
- `frontend/src/components/cases/analytics.tsx:705`
- `frontend/src/components/screening/manual-screening.tsx:509`

The scan may be incomplete for these files. Rerun Semgrep with a parser/configuration that handles these files and confirm the warnings are gone before treating this as a complete repository scan.

Confidence is the reviewer’s confidence that a result represents a security issue, rather than confidence that the scanner matched the reported code.

## Group 1 — Dynamic SQL and SQL injection (31 findings)

**Assessment and recommendation:** These findings are in stress-test, demo, and test utilities. The PostgreSQL DDL uses `psycopg.sql.Identifier` for identifiers and a fixed type mapping; the local reconciliation queries use constant category names and UUID-validated tenant values. The test database names are generated UUIDs and escaped with `pgx.Identifier.Sanitize()`. The matches are likely false positives. Keep identifier quoting (SQL identifiers cannot be value-bound), parameterize data values, make unsupported types fail closed rather than defaulting to `TEXT`, and add focused tests or narrow Semgrep suppressions documenting these trust boundaries.

| # | Severity | File | Lines | Vulnerability | Confidence |
|---|----------|------|-------|---------------|------------|
| 12 | ⚪ LOW | `backend/platform-auth/authtest/shared_database.go` | 32 | Test database name is UUID-generated and identifier-quoted; SQLi alert is likely false positive | 1/10 |
| 13 | ⚪ LOW | `backend/platform-auth/authtest/shared_database.go` | 43 | Test database cleanup uses the same sanitized identifier | 1/10 |
| 22 | ⚪ LOW | `backend/stress-tests/decision_rule_complexity_scaling.py` | 626 | Schema creation uses a quoted identifier | 1/10 |
| 23 | ⚪ LOW | `backend/stress-tests/decision_rule_complexity_scaling.py` | 629 | Table creation uses a quoted identifier | 1/10 |
| 24 | ⚪ LOW | `backend/stress-tests/decision_rule_complexity_scaling.py` | 639 | ALTER TABLE identifiers are quoted; type is fixed-mapped | 1/10 |
| 25 | ⚪ LOW | `backend/stress-tests/decision_rule_complexity_scaling.py` | 646 | Unique index creation uses quoted identifiers | 1/10 |
| 26 | ⚪ LOW | `backend/stress-tests/decision_rule_complexity_scaling.py` | 655 | Field index creation uses quoted identifiers | 1/10 |
| 27 | ⚪ LOW | `backend/stress-tests/decision_rule_complexity_scaling.py` | 663 | Updated-at index creation uses quoted identifiers | 1/10 |
| 28 | ⚪ LOW | `backend/stress-tests/decision_rule_complexity_scaling.py` | 670 | Composite index creation uses quoted identifiers | 1/10 |
| 29 | ⚪ LOW | `backend/stress-tests/demo_additional_fraud_scenarios.py` | 286 | Schema creation uses a quoted identifier | 1/10 |
| 30 | ⚪ LOW | `backend/stress-tests/demo_additional_fraud_scenarios.py` | 289 | Table creation uses a quoted identifier | 1/10 |
| 31 | ⚪ LOW | `backend/stress-tests/demo_additional_fraud_scenarios.py` | 299 | ALTER TABLE identifiers are quoted; type is fixed-mapped | 1/10 |
| 32 | ⚪ LOW | `backend/stress-tests/demo_additional_fraud_scenarios.py` | 302 | Unique index creation uses quoted identifiers | 1/10 |
| 33 | ⚪ LOW | `backend/stress-tests/demo_additional_fraud_scenarios.py` | 307 | Field index creation uses quoted identifiers | 1/10 |
| 34 | ⚪ LOW | `backend/stress-tests/demo_additional_fraud_scenarios.py` | 316 | Additional index creation uses quoted identifiers | 1/10 |
| 35 | ⚪ LOW | `backend/stress-tests/demo_full_fraud_scenario_catalog.py` | 336 | Schema creation uses a quoted identifier | 1/10 |
| 36 | ⚪ LOW | `backend/stress-tests/demo_full_fraud_scenario_catalog.py` | 339 | Table creation uses a quoted identifier | 1/10 |
| 37 | ⚪ LOW | `backend/stress-tests/demo_full_fraud_scenario_catalog.py` | 349 | ALTER TABLE identifiers are quoted; type is fixed-mapped | 1/10 |
| 38 | ⚪ LOW | `backend/stress-tests/demo_full_fraud_scenario_catalog.py` | 352 | Unique index creation uses quoted identifiers | 1/10 |
| 39 | ⚪ LOW | `backend/stress-tests/demo_full_fraud_scenario_catalog.py` | 357 | Field index creation uses quoted identifiers | 1/10 |
| 40 | ⚪ LOW | `backend/stress-tests/demo_full_fraud_scenario_catalog.py` | 366 | Additional index creation uses quoted identifiers | 1/10 |
| 41 | ⚪ LOW | `backend/stress-tests/demo_wallet_transfer_fraud_screening.py` | 358 | Schema creation uses a quoted identifier | 1/10 |
| 42 | ⚪ LOW | `backend/stress-tests/demo_wallet_transfer_fraud_screening.py` | 361 | Table creation uses a quoted identifier | 1/10 |
| 43 | ⚪ LOW | `backend/stress-tests/demo_wallet_transfer_fraud_screening.py` | 375 | ALTER TABLE identifiers are quoted; type is fixed-mapped | 1/10 |
| 44 | ⚪ LOW | `backend/stress-tests/demo_wallet_transfer_fraud_screening.py` | 382 | Unique index creation uses quoted identifiers | 1/10 |
| 45 | ⚪ LOW | `backend/stress-tests/demo_wallet_transfer_fraud_screening.py` | 391 | Field index creation uses quoted identifiers | 1/10 |
| 46 | ⚪ LOW | `backend/stress-tests/demo_wallet_transfer_fraud_screening.py` | 403 | Account/merchant/IP index creation uses quoted identifiers | 1/10 |
| 47 | ⚪ LOW | `backend/stress-tests/production_replay/database_reconciliation.py` | 111 | Local-ledger aggregation uses fixed `CATEGORIES` names | 1/10 |
| 48 | ⚪ LOW | `backend/stress-tests/production_replay/database_reconciliation.py` | 144 | Local-ledger aggregation uses fixed `CATEGORIES` names | 1/10 |
| 49 | ⚪ LOW | `backend/stress-tests/production_replay/database_reconciliation.py` | 148 | Histogram SQL uses fixed aggregate names | 1/10 |
| 50 | ⚪ LOW | `backend/stress-tests/production_replay/database_reconciliation.py` | 148 | Duplicate raw-SQL alert for the same fixed-name query as #49 | 1/10 |

## Group 2 — Containers defaulting to root (8 findings)

**Assessment and recommendation:** Six backend images have no final `USER`, so their processes default to root. Add non-root runtime users and verify startup, migration, and file access. Private key mounts must remain private and be readable by the service UID through an appropriate secret/ownership mechanism. The frontend entrypoint stages its BFF key as root, then starts the server as `node`; do not add `USER node` without redesigning that root-only setup step.

| # | Severity | File | Lines | Vulnerability | Confidence |
|---|----------|------|-------|---------------|------------|
| 1 | 🟡 MEDIUM | `backend/auth-service/Dockerfile` | 12 | Backend image defaults to root at runtime | 8/10 |
| 2 | 🟡 MEDIUM | `backend/case-manager-service/Dockerfile` | 24 | Backend image defaults to root at runtime | 8/10 |
| 3 | 🟡 MEDIUM | `backend/data-model-service/Dockerfile` | 24 | Backend image defaults to root at runtime | 8/10 |
| 8 | 🟡 MEDIUM | `backend/decision-engine-service/Dockerfile` | 25 | Backend image defaults to root at runtime | 8/10 |
| 9 | 🟡 MEDIUM | `backend/ingestion-service/Dockerfile` | 20 | Backend image defaults to root at runtime | 8/10 |
| 14 | 🟡 MEDIUM | `backend/screening-service/Dockerfile` | 34 | Backend image defaults to root at runtime | 8/10 |
| 69 | ⚪ LOW | `frontend/Dockerfile` | 43 | Root entrypoint stages key, then drops server to `node` | 2/10 |
| 70 | ⚪ LOW | `frontend/Dockerfile` | 44 | Same frontend privilege-drop flow; duplicate user alert | 2/10 |

## Group 3 — SHA-1 fingerprints (5 findings)

**Assessment and recommendation:** SHA-1 is used for identifiers/fingerprints, not signatures or authentication. Consider SHA-256 for idempotency request hashes and index-job dedupe keys. Before changing shortened database names or public revision IDs, account for existing persisted names, client caches, and rollout compatibility.

| # | Severity | File | Lines | Vulnerability | Confidence |
|---|----------|------|-------|---------------|------------|
| 4 | ⚪ LOW | `backend/data-model-service/internal/domain/datamodel/model.go` | 321 | SHA-1 index-job deduplication key; collision could dedupe unrelated work | 3/10 |
| 5 | ⚪ LOW | `backend/data-model-service/internal/service/data_model_read_service.go` | 74 | SHA-1 public model revision fingerprint | 2/10 |
| 6 | ⚪ LOW | `backend/data-model-service/internal/tenantdb/postgres/indexing.go` | 138 | SHA-1 truncated to 32 bits for managed index names | 3/10 |
| 7 | ⚪ LOW | `backend/data-model-service/internal/tenantdb/postgres/indexing.go` | 143 | SHA-1 truncated to 24 bits for archived field names | 2/10 |
| 10 | 🟡 MEDIUM | `backend/ingestion-service/internal/service/ingest_service.go` | 423 | SHA-1 request fingerprint used for idempotency comparison | 4/10 |

## Group 4 — Nginx proxy rules (10 findings)

**Assessment and recommendation:** Upstream values are fixed or selected through finite static maps. These locations intentionally serve public gateway routes. Keep the upstream allowlists and add regression checks that client input cannot change the destination. Do **not** add `internal` to these routes; authorization belongs at the route/service boundary.

| # | Severity | File | Lines | Vulnerability | Confidence |
|---|----------|------|-------|---------------|------------|
| 56 | ⚪ LOW | `docker/gateway/nginx.conf` | 107 | Dynamic proxy host is fixed OAuth upstream | 1/10 |
| 57 | ⚪ LOW | `docker/gateway/nginx.conf` | 107 | `internal` absent on intentionally public OAuth endpoint | 1/10 |
| 58 | ⚪ LOW | `docker/gateway/nginx.conf` | 122 | Dynamic proxy host is fixed identity upstream | 1/10 |
| 59 | ⚪ LOW | `docker/gateway/nginx.conf` | 122 | `internal` absent on intentionally public identity route | 1/10 |
| 60 | ⚪ LOW | `docker/gateway/nginx.conf` | 154 | Documentation upstream selected from finite service map | 1/10 |
| 61 | ⚪ LOW | `docker/gateway/nginx.conf` | 154 | `internal` absent on intentionally public docs route | 1/10 |
| 62 | ⚪ LOW | `docker/gateway/nginx.conf` | 167 | API upstream selected from finite service map | 1/10 |
| 63 | ⚪ LOW | `docker/gateway/nginx.conf` | 167 | `internal` absent on intentionally public API route | 1/10 |
| 64 | ⚪ LOW | `docker/gateway/nginx.conf` | 174 | Dynamic proxy host is fixed frontend upstream | 1/10 |
| 65 | ⚪ LOW | `docker/gateway/nginx.conf` | 174 | `internal` absent on intentionally public frontend route | 1/10 |

## Group 5 — HTML escaping and response writes (8 findings)

**Assessment and recommendation:** The UI escape helper covers HTML-special characters before interpolation; the Go handlers return embedded docs or JSON-marshaled output. Keep escaping limited to HTML text/quoted-attribute contexts; use a maintained sanitizer if rendering user-authored rich HTML. The Caddy replacement is a local static test fixture, not untrusted-input sanitization.

| # | Severity | File | Lines | Vulnerability | Confidence |
|---|----------|------|-------|---------------|------------|
| 16 | ⚪ LOW | `backend/screening-service/internal/providerproduct/docs.go` | 16 | Direct write of embedded OpenAPI YAML, not user HTML | 1/10 |
| 17 | ⚪ LOW | `backend/screening-service/internal/providerproduct/docs.go` | 21 | Direct write of constant provider documentation HTML | 1/10 |
| 18 | ⚪ LOW | `backend/screening-service/internal/providerproduct/http.go` | 96 | Direct write of JSON-marshaled response with JSON content type/nosniff | 1/10 |
| 51 | ⚪ LOW | `backend/ui_test/static/app.js` | 668 | First `replaceAll` in HTML escape chain (`&`) | 1/10 |
| 52 | ⚪ LOW | `backend/ui_test/static/app.js` | 668 | Second `replaceAll` in HTML escape chain (`<`) | 1/10 |
| 53 | ⚪ LOW | `backend/ui_test/static/app.js` | 668 | Third `replaceAll` in HTML escape chain (`>`) | 1/10 |
| 54 | ⚪ LOW | `backend/ui_test/static/app.js` | 668 | Quote escaping in the same HTML helper | 1/10 |
| 55 | ⚪ LOW | `deploy/ec2/prepare-test.mjs` | 6 | One-shot replacement in static Caddy test fixture | 1/10 |

## Group 6 — Dynamic URL and transport (4 findings)

**Assessment and recommendation:** The metrics URL is operator-configurable, not exposed through a request handler; restrict it to HTTP(S) and expected service hosts if untrusted automation can set it. The MySQL integration runner uses fixed localhost origins. The tenant bootstrap script uses internal Docker HTTP and sends a scoped bearer on provisioning requests. Use TLS/mTLS across untrusted network boundaries or tightly limit Docker-network membership and host-port exposure. `docker-compose.ec2.yml:205` publishes the Data Model port, so ensure firewall/security-group access is restricted.

| # | Severity | File | Lines | Vulnerability | Confidence |
|---|----------|------|-------|---------------|------------|
| 21 | ⚪ LOW | `backend/stress-tests/capture_runtime_read_metrics.py` | 28 | CLI-configured URL passed to `urllib`; `file://` possible if input is untrusted | 2/10 |
| 66 | ⚪ LOW | `docker/mysql-dev/integration/pipeline.py` | 36 | SSRF rule sees request construction; origin is selected from fixed localhost map | 1/10 |
| 67 | ⚪ LOW | `docker/mysql-dev/integration/pipeline.py` | 40 | `urlopen` is called on the same fixed-origin integration request | 1/10 |
| 68 | 🟡 MEDIUM | `docker/setup-root-tenant-ec2.mjs` | 56 | HTTP service URL; subsequent provisioning requests carry a scoped bearer | 6/10 |

## Group 7 — Process execution and logging (3 findings)

**Assessment and recommendation:** Current call sites are test-only or use hard-coded commands. For future-proofing, use argument arrays with `spawnSync` and `shell: false`; keep binary paths trusted. Construct the error log as one string so future dynamic descriptions cannot be interpreted as a format string.

| # | Severity | File | Lines | Vulnerability | Confidence |
|---|----------|------|-------|---------------|------------|
| 11 | ⚪ LOW | `backend/platform-auth/authtest/process.go` | 69 | Test helper starts a supplied binary directly; no shell invocation | 1/10 |
| 72 | ⚪ LOW | `scripts/seed-all-screening-data.mjs` | 25 | `execSync` receives only static script commands today | 2/10 |
| 73 | ⚪ LOW | `scripts/seed-all-screening-data.mjs` | 28 | Format-string alert depends on dynamic `desc`; current callers pass constants | 2/10 |

## Group 8 — Go error handling and integer cast (3 findings)

**Assessment and recommendation:** No code change appears necessary. `Parsed` is returned by value, so reading its zero value on error cannot nil-dereference. `DATABASE_MAX_CONNS` is bounded to 1–100 before conversion. Keep tests for these contracts.

| # | Severity | File | Lines | Vulnerability | Confidence |
|---|----------|------|-------|---------------|------------|
| 15 | ⚪ LOW | `backend/screening-service/cmd/source-verify/main.go` | 129 | `parsed.Total` read on error; `Parsed` is a non-pointer value | 1/10 |
| 19 | ⚪ LOW | `backend/screening-service/internal/sourcecollector/worker.go` | 210 | Same value-return contract; no nil dereference | 1/10 |
| 20 | ⚪ LOW | `backend/screening-service/internal/store/postgres/db.go` | 26 | `int` to `int32` cast follows a 1–100 range check | 1/10 |

## Group 9 — Path traversal (1 finding)

**Assessment and recommendation:** No fix appears necessary: `frontendRoot` is derived from the script's fixed `__dirname`, and the joined path uses constant segments. Revalidate if the root ever becomes externally configurable.

| # | Severity | File | Lines | Vulnerability | Confidence |
|---|----------|------|-------|---------------|------------|
| 71 | ⚪ LOW | `frontend/scripts/check-rule-roundtrip.js` | 7 | Path join uses fixed script root, not user-controlled path | 1/10 |
