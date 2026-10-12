"""Opt-in verification against a disposable local PostgreSQL database.

Set FRAUD_SCALE_TEST_PG_PORT for a local cluster. The database must be named
fraud_scale_observation_test; this test never accepts an RDS host.
"""
from __future__ import annotations

import asyncio
import json
import os
import subprocess
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from production_replay.database_reconciliation import reconcile_database
from production_replay.identity_ledger import IdentityLedger
from production_replay.postgres_observation import PostgresObserver, observation_sql


TENANT = "11111111-1111-1111-1111-111111111111"
SCHEMA = "tenant_11111111111111111111111111111111"
EVAL_ID = r'eval\path"quoted'
FIXTURE = f"""
DROP SCHEMA IF EXISTS core CASCADE;
DROP SCHEMA IF EXISTS core_ingestion CASCADE;
DROP SCHEMA IF EXISTS {SCHEMA} CASCADE;
DROP TABLE IF EXISTS public.river_job;
CREATE SCHEMA core;
CREATE SCHEMA core_ingestion;
CREATE SCHEMA {SCHEMA};
CREATE TABLE {SCHEMA}.transactions (object_id text PRIMARY KEY);
CREATE TABLE core_ingestion.ingestion_audit (tenant_id uuid, object_id text, object_type text,
    status text, idempotency_key text);
CREATE TABLE core_ingestion.outbox_events (tenant_id uuid, aggregate_key text, aggregate_type text, event_type text);
CREATE TABLE core_ingestion.idempotency_keys (tenant_id uuid, key text);
CREATE TABLE core.decisions (id uuid PRIMARY KEY, tenant_id uuid, object_id text, object_type text);
CREATE TABLE core.rule_executions (id uuid PRIMARY KEY, decision_id uuid REFERENCES core.decisions);
CREATE TABLE core.outbox_events (tenant_id uuid, aggregate_id text, aggregate_type text);
CREATE TABLE public.river_job (queue text, state text, scheduled_at timestamptz);
CREATE INDEX river_active_sample ON public.river_job(state, scheduled_at);
INSERT INTO {SCHEMA}.transactions VALUES ('seed'), ('{EVAL_ID}');
INSERT INTO core_ingestion.ingestion_audit VALUES
    ('{TENANT}', 'seed', 'transactions', 'succeeded', 'database-scale-seed:1'),
    ('{TENANT}', '{EVAL_ID}', 'transactions', 'succeeded', 'database-scale-evaluation:1');
INSERT INTO core_ingestion.outbox_events VALUES ('{TENANT}', 'seed', 'transactions', 'record.ingested'),
    ('{TENANT}', '{EVAL_ID}', 'transactions', 'record.ingested'),
    ('{TENANT}', 'transactions', 'transactions', 'batch.ingestion.completed');
INSERT INTO core_ingestion.idempotency_keys VALUES ('{TENANT}', 'database-scale-seed:1'), ('{TENANT}', 'database-scale-evaluation:1');
INSERT INTO core.decisions VALUES ('22222222-2222-2222-2222-222222222222', '{TENANT}', '{EVAL_ID}', 'transactions');
INSERT INTO core.rule_executions VALUES ('33333333-3333-3333-3333-333333333333', '22222222-2222-2222-2222-222222222222');
INSERT INTO core.outbox_events VALUES ('{TENANT}', '22222222-2222-2222-2222-222222222222', 'decision');
-- Other-tenant effects must never contaminate the test tenant's cardinality.
INSERT INTO core_ingestion.outbox_events VALUES ('44444444-4444-4444-4444-444444444444', '{EVAL_ID}', 'transactions', 'record.ingested');
INSERT INTO public.river_job SELECT 'q', 'completed', now() FROM generate_series(1,20000);
INSERT INTO public.river_job VALUES ('q', 'available', now()-interval '10 seconds');
ANALYZE;
"""


@unittest.skipUnless(os.getenv("FRAUD_SCALE_TEST_PG_PORT"), "requires disposable local PostgreSQL")
class ScaleSQLIntegrationTests(unittest.IsolatedAsyncioTestCase):
    @classmethod
    def setUpClass(cls):
        cls.command = ["psql", "-h", "127.0.0.1", "-p", os.environ["FRAUD_SCALE_TEST_PG_PORT"],
                       "-d", "fraud_scale_observation_test"]
        subprocess.run([*cls.command, "-X", "-w", "-v", "ON_ERROR_STOP=1", "-c", FIXTURE],
                       check=True, capture_output=True, timeout=30)

    async def test_sql_snapshots_streaming_reconciliation_and_queue_plan(self):
        observer = PostgresObserver(self.command, dict(os.environ), 5)
        snapshot = await observer(storage=True)
        self.assertEqual(snapshot["status"], "ok", snapshot)
        self.assertEqual(snapshot["metrics"]["river"][0]["observed_jobs"], 1)
        self.assertGreater(snapshot["metrics"]["database_bytes"], 0)
        self.assertTrue(snapshot["metrics"]["storage"])
        with TemporaryDirectory() as directory, IdentityLedger(Path(directory)/"ledger") as ledger:
            ledger.register_seed(TENANT, "seed")
            ledger.register(TENANT, EVAL_ID)
            ledger.transition(TENANT, EVAL_ID, "ingesting", "ingested")
            ledger.transition(TENANT, EVAL_ID, "ingested", "evaluating")
            ledger.transition(TENANT, EVAL_ID, "evaluating", "completed", decisions=("22222222-2222-2222-2222-222222222222",))
            report = await reconcile_database(self.command, dict(os.environ), ledger, TENANT, 1, 1, 60, 1)
            self.assertTrue(report["valid"], report)
            self.assertEqual(report["persisted_totals"]["transactions"], 2)
            self.assertEqual(report["record_cardinality"]["seed"]["average_records"], 3)
            self.assertEqual(report["record_cardinality"]["seed"]["average_records_including_shared"], 5)
            self.assertEqual(report["record_cardinality"]["evaluation"]["average_records"], 7)
        plan = subprocess.run([*self.command, "-X", "-w", "-qAt", "-c",
            "EXPLAIN (ANALYZE, FORMAT JSON) SELECT queue,state,scheduled_at FROM public.river_job "
            "WHERE state='available' ORDER BY scheduled_at LIMIT 1001"], check=True, capture_output=True, text=True, timeout=10)
        self.assertIn("river_active_sample", plan.stdout)
