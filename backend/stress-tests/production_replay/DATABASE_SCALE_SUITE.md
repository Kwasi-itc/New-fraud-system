# Database-volume performance suite

This command runs all four phases in order with the `internal` scenario set:

1. Empty database: ingest and evaluate 1,000,000 records.
2. Seed 1,000,000 records, then ingest and evaluate the next 1,000,000 records from the same month.
3. Seed 5,000,000 records, then ingest and evaluate the next 1,000,000 records from the same month.
4. Seed one complete month, then ingest and evaluate 1,000,000 records from the following month.

The runner keeps the configured number of ingestion and decision requests active. A decision is submitted only after its corresponding ingestion succeeds. Failed ingestions are replaced with later source records until the exact decision-request target is reached.

Per-record ingestion audit and outbox writes remain enabled for both seeding and measured ingestion. After each phase, the runner counts both tables and fails the phase if either contains fewer entries than the number of successfully ingested records.

## Privacy

The suite removes unused source fields before writing sort files or sending requests. `source_account_no`, `source_trans_id`, `thirdparty_id`, `terminal_id`, `account_name`, `payment_msisdn`, `narration`, `raw_account_ref`, and `raw_account_name` are dropped. `account_ref`, which the internal rules require for grouping, is deterministically pseudonymised with keyed HMAC-SHA256. PII-derived object and transaction identifiers are also replaced with namespaced HMAC tokens. Raw PII and the HMAC key are never written to reports.

Create a private key file once:

```bash
umask 077
openssl rand -hex 32 > pii-hmac.key
```

## Run

The host needs `python3`, Docker Compose, and PostgreSQL client commands (`psql`, `dropdb`, and `createdb`). Use the absolute path to the RDS CA certificate. Put the database password in an environment variable rather than the command line:

```bash
export FRAUD_DB_PASSWORD='replace-me'

./backend/stress-tests/run_database_scale_suite.sh \
  --manifest backend/stress-tests/production_replay/manifests/fraud-data.json \
  --data-root /home/ubuntu/fraud_data \
  --pg-host dev-fraud-database-1.cluster-cinofplxsbbb.eu-west-1.rds.amazonaws.com \
  --pg-port 5432 \
  --pg-user postgres \
  --pg-password-env FRAUD_DB_PASSWORD \
  --pg-admin-database postgres \
  --pg-sslrootcert "$PWD/global-bundle.pem" \
  --database-name fraud_scale_test \
  --allow-drop-database fraud_scale_test \
  --pii-key-file "$PWD/pii-hmac.key" \
  --ingestion-concurrency 50 \
  --evaluation-concurrency 50
```

The destructive guard requires `--allow-drop-database` to exactly match `--database-name`, refuses `postgres`, `template0`, and `template1`, terminates connections only for that exact database, and passes the exact name as a command argument to `dropdb`/`createdb`.

The suite automatically selects a month containing enough records for the 5M phase and a consecutive month pair for the final phase. Use `--same-month YYYY-MM` and `--seed-month YYYY-MM` to choose them explicitly. Results are written under `backend/stress-tests/database-scale-runs/`; `summary.json` compares each phase with the empty-database baseline. Success requires at least 80% throughput retention and no more than a 20% p95-latency increase for both ingestion and decision evaluation in every phase.

The internal set has four scenarios and six rules: high-value account activity; odd-hour amount and burst checks; rapid account and multi-merchant activity; and a 30-day account amount-spike check.
