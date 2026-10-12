from __future__ import annotations

import argparse
import errno
import json
import sqlite3
import unittest
from collections import namedtuple
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import MagicMock, patch

from production_replay.benchmark_reporting import RunReport
from production_replay.database_reconciliation import record_observation
from production_replay.database_scale_suite import PhasePlan, _prepare_evaluation_sources, _run_phase, _validate_args, async_main, build_parser
from production_replay.identity_ledger import IdentityLedger, LedgerStorageError
from production_replay.local_storage import DiskSpaceGuard, LocalStorageError
from production_replay.sorting import build_sorted_chunks
from production_replay.tests.test_database_scale_suite import event


Usage = namedtuple("Usage", "total used free")


class StorageTests(unittest.TestCase):
    def test_guard_throttles_checks_and_failure_is_permanent_for_run(self):
        guard = DiskSpaceGuard(Path("."), 100)
        with patch("production_replay.local_storage.time.monotonic", side_effect=[10, 10.5, 11, 12]), \
             patch("production_replay.local_storage.shutil.disk_usage", side_effect=[Usage(1000, 800, 200), Usage(1000, 950, 50)]) as usage:
            guard.check()
            guard.check()
            self.assertEqual(usage.call_count, 1)
            with self.assertRaises(LocalStorageError) as raised:
                guard.check()
            with self.assertRaises(LocalStorageError) as again:
                guard.check(force=True)
            self.assertIs(raised.exception, again.exception)
            self.assertEqual(usage.call_count, 2)
            self.assertEqual(raised.exception.details["free_bytes"], 50)

    def test_low_disk_blocks_ledger_writes_but_allows_final_evidence_commit(self):
        with TemporaryDirectory() as directory:
            guard = DiskSpaceGuard(Path(directory), 100)
            with patch("production_replay.local_storage.shutil.disk_usage", return_value=Usage(1000, 800, 200)):
                with IdentityLedger(Path(directory) / "ledger", guard) as ledger:
                    ledger.register("tenant", "first")
                    with patch("production_replay.local_storage.shutil.disk_usage", return_value=Usage(1000, 950, 50)):
                        with self.assertRaises(LocalStorageError):
                            guard.check(force=True)
                        with self.assertRaises(LocalStorageError):
                            ledger.register("tenant", "second")
                        self.assertEqual(ledger.summary(2)["registered"], 1)
                        self.assertFalse(ledger.summary(2)["valid"])

    def test_reconciliation_writes_obey_same_storage_guard(self):
        with TemporaryDirectory() as directory:
            guard = DiskSpaceGuard(Path(directory), 100)
            with IdentityLedger(Path(directory) / "ledger", guard) as ledger:
                with patch("production_replay.local_storage.shutil.disk_usage", return_value=Usage(1000, 950, 50)), \
                     patch("production_replay.local_storage.time.monotonic", return_value=guard.next_check):
                    with self.assertRaises(LocalStorageError):
                        record_observation(ledger, "tenant", {"object_id": "one", "counts": {"transactions": 1}})
                self.assertEqual(ledger.connection.execute("SELECT count(*) FROM persisted").fetchone()[0], 0)

    def test_duplicate_verification_identity_is_not_misclassified_as_storage_failure(self):
        with TemporaryDirectory() as directory, IdentityLedger(Path(directory) / "ledger") as ledger:
            row = {"object_id": "one", "counts": {"transactions": 1}}
            record_observation(ledger, "tenant", row)
            with self.assertRaises(sqlite3.IntegrityError):
                record_observation(ledger, "tenant", row)
            self.assertIsNone(ledger.storage_error)
            self.assertEqual(ledger.connection.execute("SELECT count(*) FROM persisted").fetchone()[0], 1)

    def test_disk_reserve_default_and_invalid_values(self):
        argv = ["--manifest", "missing", "--database-name", "test", "--allow-drop-database", "test",
                "--pg-host", "test", "--pg-user", "test", "--pg-sslrootcert", "missing",
                "--pii-key-file", "missing", "--ingestion-concurrency", "10", "--evaluation-concurrency", "10"]
        self.assertEqual(build_parser().parse_args(argv).min_free_disk_mib, 1024)
        for minimum in ("0", "-1"):
            with self.subTest(minimum=minimum), self.assertRaisesRegex(ValueError, "--min-free-disk-mib must be positive"):
                _validate_args(build_parser().parse_args([*argv, "--min-free-disk-mib", minimum]))

    def test_preparing_sources_checks_during_writes_and_closes_source(self):
        closed = []
        def records():
            try:
                for i in range(1000):
                    yield event(i)
            finally:
                closed.append(True)
        plan = PhasePlan("test", 0, lambda: iter(()), records, "test")
        check = MagicMock(side_effect=[None, LocalStorageError("low disk", {"category": "insufficient_local_disk_space"})])
        with TemporaryDirectory() as directory:
            with self.assertRaises(LocalStorageError):
                _prepare_evaluation_sources([plan], Path(directory), 10, storage_check=check)
            self.assertEqual(check.call_count, 2)
            self.assertEqual(closed, [True])

    def test_sort_chunk_checks_space_while_writing(self):
        manifest = MagicMock()
        stream = MagicMock(adapter="fake")
        manifest.transaction_streams = [stream]
        check = MagicMock(side_effect=[None, None, LocalStorageError("low disk", {})])
        with TemporaryDirectory() as directory, patch("production_replay.sorting.get_adapter") as adapter:
            adapter.return_value.iter_events.return_value = (event(i) for i in range(1000))
            with self.assertRaises(LocalStorageError):
                build_sorted_chunks(manifest, Path(directory), 1000, storage_check=check)
            self.assertEqual(check.call_count, 3)

    def test_failure_report_records_safe_storage_details_and_preserves_last_atomic_report(self):
        with TemporaryDirectory() as directory:
            output = Path(directory)
            failure = LedgerStorageError("controlled message", {"category": "identity_ledger_disk_full"})
            with self.assertRaises(LedgerStorageError), RunReport(output) as report:
                report.start_phase("test", "test")
                raise failure
            summary = json.loads((output / "summary.json").read_text())
            self.assertEqual(summary["status"], "failed")
            self.assertEqual(summary["error"]["details"], failure.details)
            self.assertEqual(json.loads((output / "test.json").read_text())["error"]["details"], failure.details)
            with self.assertRaises(LedgerStorageError) as raised:
                with RunReport(output) as report:
                    previous = (output / "summary.json").read_bytes()
                    report.save = MagicMock(side_effect=OSError(errno.ENOSPC, "disk full"))
                    raise failure
            self.assertIs(raised.exception, failure)
            # __enter__ wrote a new running report before the injected failure;
            # failed writes leave that valid atomic artifact intact.
            summary = json.loads((output / "summary.json").read_text())
            self.assertEqual((output / "summary.json").read_bytes(), previous)
            self.assertEqual(summary["status"], "running")
            self.assertFalse(summary["acceptance"]["passed"])


class StoragePreflightTests(unittest.IsolatedAsyncioTestCase):
    async def test_low_disk_prevents_database_reset_and_service_mutations(self):
        with TemporaryDirectory() as directory:
            database = MagicMock()
            plan = PhasePlan("test", 0, lambda: iter(()), lambda: iter(()), "test")
            with patch("production_replay.local_storage.shutil.disk_usage", return_value=Usage(1000, 950, 50)):
                with self.assertRaises(LocalStorageError), RunReport(Path(directory)) as report:
                    await _run_phase(plan, argparse.Namespace(min_free_disk_mib=1), MagicMock(), database, report)
            database.recreate.assert_not_called()
            database.migrate_and_start.assert_not_called()
            database.deployment.assert_not_called()
            self.assertEqual(list(Path(directory).glob("*.sqlite3")), [])

    async def test_entrypoint_checks_space_before_preparing_or_resetting(self):
        with TemporaryDirectory() as directory:
            args = argparse.Namespace(output_root=directory, min_free_disk_mib=1)
            with patch("production_replay.database_scale_suite.build_parser") as parser, \
                 patch("production_replay.database_scale_suite._validate_args"), \
                 patch("production_replay.database_scale_suite._execute_suite") as execute, \
                 patch("production_replay.local_storage.shutil.disk_usage", return_value=Usage(1000, 950, 50)):
                parser.return_value.parse_args.return_value = args
                with self.assertRaises(LocalStorageError):
                    await async_main([])
            execute.assert_not_called()
            summary = json.loads(next(Path(directory).glob("*/summary.json")).read_text())
            self.assertEqual(summary["error"]["details"]["category"], "insufficient_local_disk_space")
            self.assertEqual(summary["status"], "failed")


if __name__ == "__main__":
    unittest.main()
