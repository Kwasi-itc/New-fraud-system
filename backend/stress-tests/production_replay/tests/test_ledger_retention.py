from __future__ import annotations

import argparse
import asyncio
import errno
import json
import sqlite3
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import AsyncMock, MagicMock, patch

from production_replay.benchmark_reporting import RunReport
from production_replay.database_scale_suite import PhasePlan, _acceptance, _run_phase
from production_replay.identity_ledger import IdentityLedger, LedgerCleanupError, LedgerStorageError
from production_replay.local_storage import DiskSpaceGuard
from production_replay.tests.test_benchmark_foundation import FakeClients
from production_replay.tests.test_database_scale_suite import event


class LedgerRetentionTests(unittest.TestCase):
    def test_default_retains_closed_ledger_and_preserves_existing_files(self):
        with TemporaryDirectory() as directory:
            path = Path(directory) / "ledger.sqlite3"
            with IdentityLedger(path) as ledger:
                ledger.register("tenant", "one")
            self.assertTrue(path.is_file())
            self.assertEqual(ledger.artifact_summary()["status"], "retained")
            previous = path.read_bytes()
            with self.assertRaises(FileExistsError):
                IdentityLedger(path, retain_artifact=False)
            self.assertEqual(path.read_bytes(), previous)

    def test_temporary_ledger_and_wal_sidecars_are_removed_after_close(self):
        with TemporaryDirectory() as directory:
            path = Path(directory) / "ledger.sqlite3"
            sentinel = Path(directory) / "older.identities.sqlite3"
            sentinel.write_bytes(b"preserve old run")
            with IdentityLedger(path, retain_artifact=False) as ledger:
                ledger.connection.execute("PRAGMA journal_mode=WAL")
                ledger.register("tenant", "one")
                ledger.flush()
                self.assertTrue(Path(str(path) + "-wal").is_file())
                self.assertTrue(Path(str(path) + "-shm").is_file())
                self.assertTrue(path.exists())
            for owned_path in ledger.artifact_paths:
                self.assertFalse(owned_path.exists())
            self.assertEqual(ledger.artifact_summary()["status"], "deleted")
            self.assertEqual(sentinel.read_bytes(), b"preserve old run")
            with self.assertRaises(sqlite3.ProgrammingError):
                ledger.connection.execute("SELECT 1")

    def test_failure_and_failed_initialization_remove_only_new_owned_files(self):
        with TemporaryDirectory() as directory:
            path = Path(directory) / "ledger.sqlite3"
            with self.assertRaisesRegex(ValueError, "workload failed"):
                with IdentityLedger(path, retain_artifact=False) as ledger:
                    ledger.register("tenant", "one")
                    raise ValueError("workload failed")
            self.assertFalse(path.exists())
            with patch("production_replay.identity_ledger.sqlite3.connect", side_effect=sqlite3.OperationalError("cannot open")):
                with self.assertRaises(sqlite3.OperationalError):
                    IdentityLedger(path, retain_artifact=False)
            self.assertFalse(path.exists())
            with patch.object(DiskSpaceGuard, "check", side_effect=ValueError("initialization failed")):
                with self.assertRaisesRegex(ValueError, "initialization failed"):
                    IdentityLedger(path, DiskSpaceGuard(Path(directory), 1), retain_artifact=False)
            self.assertFalse(path.exists())

    def test_existing_sidecar_is_not_adopted_or_removed(self):
        with TemporaryDirectory() as directory:
            path = Path(directory) / "ledger.sqlite3"
            for suffix in ("-journal", "-wal", "-shm"):
                with self.subTest(suffix=suffix):
                    sidecar = Path(str(path) + suffix)
                    sidecar.write_bytes(b"existing evidence")
                    with self.assertRaises(FileExistsError):
                        IdentityLedger(path, retain_artifact=False)
                    self.assertEqual(sidecar.read_bytes(), b"existing evidence")
                    self.assertFalse(path.exists())
                    sidecar.unlink()

    def test_full_sqlite_database_is_cleaned_without_masking_storage_failure(self):
        with TemporaryDirectory() as directory:
            path = Path(directory) / "ledger.sqlite3"
            with self.assertRaises(LedgerStorageError):
                with IdentityLedger(path, retain_artifact=False) as ledger:
                    pages = ledger.connection.execute("PRAGMA page_count").fetchone()[0]
                    ledger.connection.execute(f"PRAGMA max_page_count={pages + 1}")
                    for i in range(1000):
                        ledger.register("tenant", str(i))
            self.assertEqual(ledger.artifact_summary()["status"], "deleted")
            self.assertFalse(path.exists())

    def test_cleanup_failure_aborts_success_but_does_not_replace_workload_error(self):
        for failing_workload in (False, True):
            with self.subTest(failing_workload=failing_workload), TemporaryDirectory() as directory:
                path = Path(directory) / "ledger.sqlite3"
                unlink = Path.unlink
                def deny_ledger(candidate, *args, **kwargs):
                    if candidate == path:
                        raise PermissionError(errno.EACCES, "denied")
                    return unlink(candidate, *args, **kwargs)
                expected = ValueError if failing_workload else LedgerCleanupError
                with patch.object(Path, "unlink", deny_ledger):
                    with self.assertRaises(expected):
                        with IdentityLedger(path, retain_artifact=False) as ledger:
                            ledger.register("tenant", "one")
                            if failing_workload:
                                raise ValueError("original workload")
                self.assertTrue(path.exists())
                self.assertEqual(ledger.artifact_summary()["status"], "cleanup_failed")
                self.assertEqual(ledger.cleanup_error["errno"], errno.EACCES)


class PhaseRetentionTests(unittest.IsolatedAsyncioTestCase):
    async def test_real_phase_skip_still_checks_response_identities_and_cannot_pass_acceptance(self):
        args = argparse.Namespace(
            skip_verification=True, min_free_disk_mib=1, data_model_url="http://data",
            ingestion_url="http://ingestion", decision_engine_url="http://decision", auth_token=None,
            request_timeout=1, publication_timeout=1, evaluation_count=2, ingestion_concurrency=1,
            evaluation_concurrency=1, segment_size=1, pipeline_timeout=30, deferred_policy="reject",
            decision_completion_timeout=1, decision_poll_interval=.01, seed_batch_size=2,
            seed_concurrency=1, capture_metrics=False,
        )
        database = MagicMock()
        database.stats.return_value = {"database_bytes": 1000, "estimated_user_rows": 2}
        database.deployment.return_value = {"status": "ok", "containers": [
            {"service": name, "runtime_settings": {}, "database_targets": {}}
            for name in ("decision-engine-service", "ingestion-service")
        ]}
        database.reconcile = AsyncMock()
        setup = MagicMock()
        setup.run = AsyncMock(return_value={"tenant_id": "tenant-1", "scenarios": {"one": {}}})
        plan = PhasePlan("empty_database", 0, lambda: iter(()), lambda: iter([event(0), event(1)]), "smoke")
        with TemporaryDirectory() as directory, \
             patch("production_replay.database_scale_suite.ServiceClients", return_value=FakeClients()), \
             patch("production_replay.database_scale_suite.EnvironmentSetup", return_value=setup):
            output = Path(directory)
            with RunReport(output) as report:
                result = await _run_phase(plan, args, MagicMock(), database, report)
                report.finish_phase(result)
            self.assertTrue(result["evaluation"]["identity_reconciliation"]["valid"])
            self.assertEqual(result["database"]["durable_reconciliation"]["status"], "skipped")
            self.assertEqual(result["evaluation"]["decision"]["successes"], 2)
            self.assertEqual(result["identity_ledger_artifact"]["status"], "deleted")
            self.assertFalse((output / "empty_database.identities.sqlite3").exists())
            acceptance = _acceptance([result])
            self.assertFalse(acceptance["passed"])
            self.assertIn("durable_verification_skipped", acceptance["comparisons"][0]["reliability_failures"])
            database.reconcile.assert_not_awaited()

    async def test_phase_flag_controls_cleanup_and_keeps_results_and_failure_counters(self):
        for skip in (False, True):
            for outcome in ("completed", "failed", "cancelled"):
                with self.subTest(skip=skip, outcome=outcome), TemporaryDirectory() as directory:
                    output = Path(directory)
                    metrics = output / "test.metrics.ndjson"
                    metrics.write_text("metrics retained\n")
                    older = output / "previous.identities.sqlite3"
                    older.write_bytes(b"previous ledger")
                    args = argparse.Namespace(skip_verification=skip, min_free_disk_mib=1)
                    plan = PhasePlan("test", 0, lambda: iter(()), lambda: iter(()), "test")
                    async def work(phase, args, manifest, database, report, error_log, ledger):
                        report.start_phase(phase.name, "test")
                        ledger.register("tenant", "one")
                        ledger.transition("tenant", "one", "ingesting", "ingested")
                        ledger.transition("tenant", "one", "ingested", "evaluating")
                        if outcome == "completed":
                            ledger.transition("tenant", "one", "evaluating", "completed")
                        evaluation = {"identity_reconciliation": ledger.summary(1)}
                        report.update_phase(evaluation=evaluation)
                        if outcome == "failed":
                            raise ValueError("workload failed")
                        if outcome == "cancelled":
                            raise asyncio.CancelledError()
                        return {"phase": phase.name, "evaluation": evaluation}
                    with patch("production_replay.database_scale_suite._run_phase_with_ledger", side_effect=work):
                        if outcome == "completed":
                            with RunReport(output) as report:
                                result = await _run_phase(plan, args, MagicMock(), MagicMock(), report)
                                report.finish_phase(result)
                            self.assertEqual(result["identity_ledger_artifact"]["status"], "deleted" if skip else "retained")
                        else:
                            expected = ValueError if outcome == "failed" else asyncio.CancelledError
                            with self.assertRaises(expected), RunReport(output) as report:
                                await _run_phase(plan, args, MagicMock(), MagicMock(), report)
                    saved = json.loads((output / "test.json").read_text())
                    self.assertEqual(saved["identity_ledger_artifact"]["status"], "deleted" if skip else "retained")
                    self.assertEqual(saved["evaluation"]["identity_reconciliation"]["registered"], 1)
                    self.assertEqual((output / "test.identities.sqlite3").exists(), not skip)
                    self.assertEqual(metrics.read_text(), "metrics retained\n")
                    self.assertEqual(older.read_bytes(), b"previous ledger")
                    if outcome != "completed":
                        summary = json.loads((output / "summary.json").read_text())
                        self.assertEqual(summary["status"], "failed" if outcome == "failed" else "interrupted")
                        self.assertFalse(summary["acceptance"]["passed"])


if __name__ == "__main__":
    unittest.main()
