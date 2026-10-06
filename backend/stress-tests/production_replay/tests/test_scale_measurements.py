from __future__ import annotations

import copy
import json
import subprocess
from types import SimpleNamespace
import unittest
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import patch

from production_replay.benchmark_observation import capture_deployment, project_container
from production_replay.database_reconciliation import durable_summary, record_observation, reconciliation_sql
from production_replay.database_scale_suite import DatabaseController, PipelineMetrics, _acceptance, _build_phase_plans, build_parser
from production_replay.identity_ledger import IdentityLedger, LedgerInvariantError
from production_replay.postgres_observation import COUNTERS, counter_deltas
from production_replay.tests.test_benchmark_foundation import phase
from production_replay.tests.test_database_scale_suite import event


class MeasurementTests(unittest.TestCase):
    def test_skipped_verification_cannot_pass_acceptance(self):
        value = phase()
        value["database"]["durable_reconciliation"] = {"status": "skipped", "valid": None}
        result = _acceptance([value])
        self.assertFalse(result["passed"])
        self.assertIn("durable_verification_skipped", result["comparisons"][0]["reliability_failures"])

    def test_throughput_uses_stage_windows_not_harness_lifetime(self):
        metrics = PipelineMetrics(100, started_at=1, ingestion_first_at=10, ingestion_last_at=20,
                                  decision_first_at=12, decision_last_at=25,
                                  ingestion_successes=100, decision_successes=90, decision_attempts=100)
        with patch("production_replay.database_scale_suite.time.perf_counter", return_value=100):
            result = metrics.summary()
        self.assertEqual(result["ingestion"]["requests_per_second"], 10)
        self.assertEqual(result["decision"]["successful_evaluations_per_second"], round(90/13, 2))
        self.assertEqual(result["pipeline_successes_per_second"], 6)
        self.assertEqual(result["throughput_windows_seconds"]["end_to_end"], 15)

    def test_reset_aware_database_deltas(self):
        def sample(second, counter):
            return {"status": "ok", "metrics": {"captured_at": f"2026-10-01T00:00:{second:02d}+00:00",
                "configuration": {"track_io_timing": "on"},
                "database": {**dict.fromkeys(COUNTERS, counter), "stats_reset": "unchanged"}}}
        before, after = sample(0, 10), sample(10, 20)
        value = counter_deltas(before, after)
        self.assertTrue(value["valid"])
        self.assertEqual(value["per_second"]["xact_commit"], 1)
        self.assertEqual(value["buffer_hit_ratio"], .5)
        after["metrics"]["database"]["stats_reset"] = "reset"
        self.assertFalse(counter_deltas(before, after)["valid"])
        after["metrics"]["database"]["stats_reset"] = "unchanged"
        after["metrics"]["database"]["xact_commit"] = 0
        self.assertFalse(counter_deltas(before, after)["valid"])
        self.assertFalse(counter_deltas(before, {"status": "error"})["valid"])

    def test_fixed_corpus_same_across_empty_and_previous_month_seed(self):
        args = build_parser().parse_args([
            "--manifest", "m", "--database-name", "test", "--allow-drop-database", "test",
            "--pg-host", "db", "--pg-user", "u", "--pg-sslrootcert", "ca", "--pii-key-file", "key",
            "--ingestion-concurrency", "2", "--evaluation-concurrency", "2", "--evaluation-count", "2",
            "--evaluation-offset", "2", "--phase", "empty", "1month"])
        from dataclasses import replace
        seed = replace(event(0), occurred_at=datetime(2026, 6, 1, tzinfo=timezone.utc))
        records = [replace(event(i+10), occurred_at=datetime(2026, 7, 1, tzinfo=timezone.utc)) for i in range(10)]
        def select(_chunks, *, month=None, skip=0, limit=None):
            source = [seed] if month == "2026-06" else records
            return iter(source[skip:] if limit is None else source[skip:skip+limit])
        with patch("production_replay.database_scale_suite._select_events", side_effect=select):
            plans = _build_phase_plans((), 11, Counter({"2026-06": 1, "2026-07": 10}), args)
            self.assertEqual(list(plans[0].evaluation_factory()), list(plans[1].evaluation_factory()))
            self.assertEqual(list(plans[1].seed_factory()), [seed])

    def test_no_seeded_phase_is_mistaken_for_empty_baseline(self):
        value = phase()
        value["phase"] = "seed_5m_same_month"
        result = _acceptance([value])
        self.assertFalse(result["passed"])
        self.assertFalse(result["evaluated"])
        changed = copy.deepcopy(phase())
        changed["evaluation"]["evaluation_source"]["sha256"] = "other"
        self.assertFalse(_acceptance([phase(), changed])["passed"])

    def test_deployment_does_not_persist_credentials_or_arbitrary_env(self):
        value = project_container({"Config": {"Env": ["SECRET_TOKEN=secret", "DATABASE_URL=postgres://u:secret@db/test?token=secret",
            "LIVE_DECISION_MODE=sync"]}, "State": {"Status": "running"}})
        self.assertNotIn("secret", json.dumps(value))
        self.assertEqual(value["runtime_settings"], {"LIVE_DECISION_MODE": "sync"})
        self.assertEqual(value["database_targets"]["DATABASE_URL"], {"host": "db", "port": 5432, "database": "test"})

    def test_deployment_requires_running_services_and_database_targets(self):
        item = {"Config": {"Labels": {"com.docker.compose.service": "service"},
                           "Env": ["DATABASE_URL=postgres://db/test"]}, "State": {"Status": "running"}}
        def inspect(services=("service",)):
            with patch("production_replay.benchmark_observation.subprocess.run", side_effect=[
                subprocess.CompletedProcess([], 0, stdout="id"),
                subprocess.CompletedProcess([], 0, stdout=json.dumps([item]))]):
                return capture_deployment([], {}, services, "test", "db", 5432)
        self.assertEqual(inspect()["status"], "ok")
        self.assertEqual(inspect(("service", "missing"))["status"], "error")
        item["State"]["Status"] = "exited"
        self.assertEqual(inspect()["status"], "error")
        item["State"]["Status"] = "running"
        item["Config"]["Env"] = []
        self.assertEqual(inspect()["status"], "error")

    def test_failed_required_database_snapshots_or_durable_evidence_fail_acceptance(self):
        value = phase()
        value["telemetry"] = {"enabled": True, "valid": False, "boundary_snapshots_valid": False}
        self.assertFalse(_acceptance([value])["passed"])
        value["telemetry"]["valid"] = True
        value["database"]["durable_reconciliation"]["valid"] = False
        self.assertFalse(_acceptance([value])["passed"])

    def test_invalid_compose_stops_before_database_reset(self):
        database = object.__new__(DatabaseController)
        database.args = SimpleNamespace(no_manage_services=False)
        with patch.object(database, "compose_command", return_value=["docker", "compose", "config", "--quiet"]), \
             patch.object(database, "compose_env", return_value={}), \
             patch("production_replay.database_scale_suite._run", side_effect=subprocess.CalledProcessError(1, "compose")) as run:
            with self.assertRaises(subprocess.CalledProcessError):
                database.recreate()
        run.assert_called_once()
        self.assertEqual(run.call_args.args[0][-2:], ["config", "--quiet"])


class DurableTests(unittest.TestCase):
    def test_exact_seed_and_evaluation_counts_and_separate_cardinality(self):
        with TemporaryDirectory() as directory, IdentityLedger(Path(directory)/"ledger") as ledger:
            ledger.register_seed("tenant", "seed")
            ledger.register("tenant", "eval")
            ledger.transition("tenant", "eval", "ingesting", "ingested")
            ledger.transition("tenant", "eval", "ingested", "evaluating")
            ledger.transition("tenant", "eval", "evaluating", "completed", decisions=("decision",))
            record_observation(ledger, "tenant", {"object_id": "seed", "counts": {"transactions": 1, "ingestion_audit": 1, "outbox_events": 1}})
            record_observation(ledger, "tenant", {"object_id": "eval", "counts": {"transactions": 1, "ingestion_audit": 1,
                "outbox_events": 1, "decisions": 1, "rule_executions": 2}})
            record_observation(ledger, "tenant", {"object_id": "eval", "decision_id": "decision"})
            value = durable_summary(ledger, 1, 1)
            self.assertTrue(value["valid"])
            self.assertEqual(value["record_cardinality"]["seed"]["average_records"], 3)
            self.assertEqual(value["record_cardinality"]["evaluation"]["average_records"], 6)
            record_observation(ledger, "tenant", {"shared_seed": {"idempotency_keys": 1, "batch_outbox": 1}})
            self.assertTrue(durable_summary(ledger, 1, 1, 1)["valid"])
            self.assertEqual(durable_summary(ledger, 1, 1, 1)["record_cardinality"]["seed"]["average_records_including_shared"], 5)
            self.assertFalse(durable_summary(ledger, 1, 1, 2)["valid"])
            record_observation(ledger, "other-tenant", {"object_id": "eval", "counts": {"transactions": 1}})
            self.assertFalse(durable_summary(ledger, 1, 1)["valid"])
            with self.assertRaisesRegex(LedgerInvariantError, "overlap"):
                ledger.register("tenant", "seed")

    def test_missing_decision_and_duplicate_writes_are_failures(self):
        with TemporaryDirectory() as directory, IdentityLedger(Path(directory)/"ledger") as ledger:
            ledger.register("tenant", "eval")
            ledger.transition("tenant", "eval", "ingesting", "ingested")
            ledger.transition("tenant", "eval", "ingested", "evaluating")
            ledger.transition("tenant", "eval", "evaluating", "completed", decisions=("decision",))
            record_observation(ledger, "tenant", {"object_id": "eval", "counts": {"transactions": 1, "ingestion_audit": 2, "outbox_events": 1}})
            value = durable_summary(ledger, 0, 1)
            self.assertFalse(value["valid"])
            self.assertEqual(value["discrepancies"]["audit_or_outbox_mismatch"], 1)
            self.assertEqual(value["discrepancies"]["missing_decisions"], 1)

    def test_sql_validates_tenant_and_projects_only_counts_and_ids(self):
        sql = reconciliation_sql("11111111-1111-1111-1111-111111111111")
        self.assertIn('"tenant_11111111111111111111111111111111".transactions', sql)
        self.assertNotIn("payload", sql)
        with self.assertRaises(ValueError):
            reconciliation_sql("bad'; DROP DATABASE test;")
