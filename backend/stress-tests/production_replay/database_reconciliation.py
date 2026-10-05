"""Exact read-only durable evidence, collected after the measured pipeline.

Server rows stream into the local token ledger. SQL groups whole, tenant-scoped
relations once; it never runs a query per transaction. No identities are written
to artifacts without a per-run keyed token.
"""
from __future__ import annotations

import asyncio
import csv
import json
import math
import sqlite3
import time
from typing import Any
from uuid import UUID

from .identity_ledger import IdentityLedger


CATEGORIES = ("transactions", "ingestion_audit", "outbox_events", "decisions", "rule_executions",
              "idempotency_keys", "decision_outbox")


def reconciliation_sql(tenant_id: str) -> str:
    tenant = str(UUID(tenant_id))
    schema = "tenant_" + UUID(tenant_id).hex
    # Tenant schemas follow the data-model service's SchemaNameFor contract.
    filters = f"tenant_id = '{tenant}'::uuid"
    return f"""
        COPY (
            WITH effects AS (
                SELECT object_id, 'transactions' AS category, count(*) AS n
                FROM "{schema}".transactions GROUP BY object_id
                UNION ALL SELECT object_id, 'ingestion_audit', count(*)
                FROM core_ingestion.ingestion_audit WHERE {filters} AND object_type='transactions'
                    AND status='succeeded' GROUP BY object_id
                UNION ALL SELECT aggregate_key, 'outbox_events', count(*)
                FROM core_ingestion.outbox_events WHERE {filters} AND aggregate_type='transactions'
                    AND event_type IN ('record.ingested','record.updated')
                    GROUP BY aggregate_key
                UNION ALL SELECT object_id, 'decisions', count(*)
                FROM core.decisions WHERE {filters} AND object_type='transactions' GROUP BY object_id
                UNION ALL SELECT d.object_id, 'rule_executions', count(*)
                FROM core.rule_executions r JOIN core.decisions d ON r.decision_id=d.id
                WHERE d.{filters} AND d.object_type='transactions' GROUP BY d.object_id
                UNION ALL SELECT a.object_id, 'idempotency_keys', count(DISTINCT k.key)
                FROM core_ingestion.ingestion_audit a JOIN core_ingestion.idempotency_keys k
                    ON k.tenant_id=a.tenant_id AND k.key=a.idempotency_key
                WHERE a.{filters} AND a.object_type='transactions' AND a.status='succeeded'
                    AND k.key LIKE 'database-scale-evaluation:%'
                    GROUP BY a.object_id
                UNION ALL SELECT d.object_id, 'decision_outbox', count(*)
                FROM core.outbox_events o JOIN core.decisions d ON o.aggregate_id=d.id::text AND o.tenant_id=d.tenant_id
                WHERE d.{filters} AND d.object_type='transactions' AND o.aggregate_type='decision'
                    GROUP BY d.object_id
            ), grouped AS (
                SELECT object_id, json_object_agg(category, n) AS counts FROM effects GROUP BY object_id
            )
            SELECT json_build_object('object_id', object_id, 'counts', counts)::text FROM grouped
        ) TO STDOUT WITH (FORMAT csv);
        COPY (
            SELECT json_build_object('shared_seed', json_build_object(
                'idempotency_keys', (SELECT count(*) FROM core_ingestion.idempotency_keys
                    WHERE {filters} AND key LIKE 'database-scale-seed:%'),
                'batch_outbox', (SELECT count(*) FROM core_ingestion.outbox_events
                    WHERE {filters} AND aggregate_type='transactions' AND event_type='batch.ingestion.completed')
            ))::text
        ) TO STDOUT WITH (FORMAT csv);
        COPY (
            SELECT json_build_object('object_id', object_id, 'decision_id', id)::text
            FROM core.decisions WHERE {filters} AND object_type='transactions'
        ) TO STDOUT WITH (FORMAT csv);
    """


def record_observation(ledger: IdentityLedger, tenant: str, row: dict[str, Any]) -> None:
    if "shared_seed" in row:
        value = row["shared_seed"]
        ledger.connection.execute("INSERT INTO shared_seed_records VALUES (?,?)",
                                  (value["idempotency_keys"], value["batch_outbox"]))
        return
    identity = ledger.token(tenant, row["object_id"])
    if "decision_id" in row:
        ledger.connection.execute("INSERT INTO persisted_decisions VALUES (?, ?)",
                                  (ledger.token(tenant, row["decision_id"]), identity))
    else:
        counts = row["counts"]
        values = tuple(int(counts.get(name, 0)) for name in CATEGORIES)
        if any(value < 0 for value in values):
            raise ValueError("negative_persisted_count")
        ledger.connection.execute("INSERT INTO persisted VALUES (?,?,?,?,?,?,?,?)", (identity, *values))
    ledger._checkpoint()


def durable_summary(ledger: IdentityLedger, expected_seed: int, expected_evaluations: int,
                    expected_seed_batches: int | None = None) -> dict[str, Any]:
    db = ledger.connection
    db.commit()
    totals = dict(zip(CATEGORIES, db.execute(
        "SELECT " + ",".join(f"coalesce(sum({name}),0)" for name in CATEGORIES) + " FROM persisted"
    ).fetchone()))
    discrepancies = {
        "unexpected_objects": db.execute("""SELECT count(*) FROM persisted p
            LEFT JOIN seeds s USING(identity) LEFT JOIN identities i USING(identity)
            WHERE s.identity IS NULL AND (i.identity IS NULL OR i.state='ingestion_failed')""").fetchone()[0],
        "missing_seed_transactions": db.execute("""SELECT count(*) FROM seeds s
            LEFT JOIN persisted p USING(identity) WHERE coalesce(p.transactions,0)<>1""").fetchone()[0],
        "missing_evaluation_transactions": db.execute("""SELECT count(*) FROM identities i
            LEFT JOIN persisted p USING(identity) WHERE i.state='completed' AND coalesce(p.transactions,0)<>1""").fetchone()[0],
        "audit_or_outbox_mismatch": db.execute("""SELECT count(*) FROM (
            SELECT identity FROM seeds UNION ALL SELECT identity FROM identities WHERE state='completed'
            ) expected LEFT JOIN persisted p USING(identity)
            WHERE coalesce(p.ingestion_audit,0)<>1 OR coalesce(p.outbox_events,0)<>1""").fetchone()[0],
        "missing_decisions": db.execute("""SELECT count(*) FROM decision_effects e
            LEFT JOIN persisted_decisions p ON p.decision=e.decision AND p.identity=e.identity
            WHERE p.decision IS NULL""").fetchone()[0],
        "unexpected_decisions": db.execute("""SELECT count(*) FROM persisted_decisions p
            LEFT JOIN decision_effects e ON e.decision=p.decision AND e.identity=p.identity
            WHERE e.decision IS NULL""").fetchone()[0],
    }
    shared_row = db.execute("SELECT idempotency_keys,batch_outbox FROM shared_seed_records").fetchall()
    shared = dict(zip(("idempotency_keys", "batch_outbox"), shared_row[0])) if len(shared_row) == 1 else None
    if expected_seed_batches is not None:
        discrepancies["shared_seed_records_mismatch"] = int(shared is None or
            any(value != expected_seed_batches for value in shared.values()))
    seed_count = db.execute("SELECT count(*) FROM seeds").fetchone()[0]
    completed = db.execute("SELECT count(*) FROM identities WHERE state='completed'").fetchone()[0]
    cohorts = {}
    for cohort, relation in (("seed", "seeds"), ("evaluation", "identities WHERE state='completed'")):
        fields = ",".join(f"coalesce(p.{name},0) AS {name}" for name in CATEGORIES)
        base = f"SELECT {fields} FROM ({'SELECT identity FROM ' + relation}) e LEFT JOIN persisted p USING(identity)"
        cohort_totals = dict(zip(CATEGORIES, db.execute(
            "SELECT " + ",".join(f"coalesce(sum({name}),0)" for name in CATEGORIES) + f" FROM ({base})"
        ).fetchone()))
        total_expr = "+".join(CATEGORIES)
        histogram = db.execute(f"SELECT {total_expr} AS records, count(*) FROM ({base}) GROUP BY records ORDER BY records").fetchall()
        count = sum(n for _, n in histogram)
        def percentile(fraction: float) -> int | None:
            threshold, seen = math.ceil(count * fraction), 0
            for records, n in histogram:
                seen += n
                if seen >= threshold:
                    return records
            return None
        cohorts[cohort] = {"transactions": count, "by_category": cohort_totals,
                          "min_records": histogram[0][0] if histogram else None,
                          "max_records": histogram[-1][0] if histogram else None,
                          "average_records": sum(v * n for v, n in histogram) / count if count else None,
                          "p50_records": percentile(.5), "p95_records": percentile(.95),
                          "constant_records_per_transaction": len(histogram) == 1}
        if cohort == "seed" and shared is not None:
            cohorts[cohort]["shared_records"] = shared
            cohorts[cohort]["average_records_including_shared"] = (
                (sum(v * n for v, n in histogram) + sum(shared.values())) / count if count else None)
    return {"valid": not any(discrepancies.values()) and seed_count == expected_seed and completed == expected_evaluations,
            "expected_seed_transactions": expected_seed, "registered_seed_transactions": seed_count,
            "expected_evaluated_transactions": expected_evaluations, "completed_evaluation_identities": completed,
            "persisted_totals": totals, "discrepancies": discrepancies, "record_cardinality": cohorts,
            "shared_seed_records": shared,
            "scope": "tenant_transactions_and_ingestion_effects_and_response_decision_ids",
            "limitations": ["non_triggered_scenarios_need_not_persist_decisions",
                            "rule_execution_business_correctness_not_asserted",
                            "river_jobs_reference_data_and_downstream_deliveries_excluded"]}


async def reconcile_database(command: list[str], env: dict[str, str], ledger: IdentityLedger,
                             tenant_id: str, seed_count: int, evaluation_count: int,
                             timeout: float = 1800, expected_seed_batches: int | None = None) -> dict[str, Any]:
    started = time.monotonic()
    deadline = started + timeout
    process = None
    try:
        process = await asyncio.create_subprocess_exec(
            *command, "-X", "-w", "-qAt", "-v", "ON_ERROR_STOP=1", "-c", reconciliation_sql(tenant_id),
            env={**env, "PGOPTIONS": f"-c default_transaction_read_only=on -c statement_timeout={int(timeout*1000)} -c lock_timeout=1000",
                 "PGCONNECT_TIMEOUT": "10"},
            stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.DEVNULL)
        async def consume() -> None:
            assert process is not None and process.stdout is not None
            async for line in process.stdout:
                if time.monotonic() >= deadline:
                    raise TimeoutError("verification_deadline")
                # JSON encodes control characters; CSV preserves JSON backslashes/quotes.
                # Pre-sanitized source object identifiers need not be HMAC strings.
                record_observation(ledger, tenant_id, json.loads(next(csv.reader([line.decode("utf-8")]))[0]))
            await process.wait()
        await asyncio.wait_for(consume(), timeout)
        if process.returncode:
            return {"valid": False, "error_type": "PsqlFailure", "exit_code": process.returncode}
        ledger.connection.set_progress_handler(lambda: int(time.monotonic() >= deadline), 10000)
        return {**durable_summary(ledger, seed_count, evaluation_count, expected_seed_batches),
                "elapsed_seconds": time.monotonic() - started}
    except (OSError, ValueError, TimeoutError, sqlite3.Error) as exc:
        return {"valid": False, "error_type": "TimeoutError" if time.monotonic() >= deadline else type(exc).__name__}
    finally:
        ledger.connection.set_progress_handler(None, 0)
        if process is not None and process.returncode is None:
            process.kill()
            await process.wait()
