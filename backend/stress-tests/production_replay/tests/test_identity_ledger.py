from __future__ import annotations

import asyncio
import sqlite3
import unittest
from contextlib import closing
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import Mock

from production_replay.identity_ledger import IdentityLedger, LedgerInvariantError, LedgerStorageError
from production_replay.database_scale_suite import _acceptance
from production_replay.tests.test_benchmark_foundation import FakeClients, pipeline


class IdentityTests(unittest.IsolatedAsyncioTestCase):
    async def test_real_sqlite_full_discards_uncommitted_batch_and_preserves_first_error(self) -> None:
        with TemporaryDirectory() as directory, IdentityLedger(Path(directory) / "ledger") as ledger:
            ledger.register("tenant", "durable")
            ledger.flush()
            pages = ledger.connection.execute("PRAGMA page_count").fetchone()[0]
            ledger.connection.execute(f"PRAGMA max_page_count={pages + 2}")
            failure = None
            for i in range(1000):
                try:
                    ledger.register("tenant", str(i))
                except LedgerStorageError as exc:
                    failure = exc
                    break
            self.assertIsNotNone(failure, "page limit must produce a real SQLITE_FULL")
            self.assertGreater(i, 1)
            self.assertIsInstance(failure.__cause__, sqlite3.OperationalError)
            self.assertEqual(failure.details["category"], "identity_ledger_disk_full")
            self.assertFalse(ledger.connection.in_transaction)
            with self.assertRaises(LedgerStorageError) as again:
                ledger.transition("tenant", str(i - 1), "ingesting", "ingested")
            self.assertIs(again.exception, failure)
            summary = ledger.summary(1000)
            self.assertFalse(summary["valid"])
            self.assertEqual(summary["counts_basis"], "last_successful_checkpoint")
            self.assertEqual(summary["registered"], 1)
            self.assertEqual(ledger.connection.execute("SELECT count(*) FROM identities").fetchone()[0], 1)

    async def test_sqlite_full_inside_savepoint_does_not_mask_error_or_claim_lost_effects(self) -> None:
        with TemporaryDirectory() as directory, IdentityLedger(Path(directory) / "ledger") as ledger:
            for identity in ("one", "two"):
                ledger.register("tenant", identity)
                ledger.transition("tenant", identity, "ingesting", "ingested")
                ledger.transition("tenant", identity, "ingested", "evaluating")
            ledger.transition("tenant", "one", "evaluating", "completed", decisions=("durable-decision",))
            ledger.flush()
            ledger.register("tenant", "uncommitted")
            pages = ledger.connection.execute("PRAGMA page_count").fetchone()[0]
            ledger.connection.execute(f"PRAGMA max_page_count={pages + 1}")
            with self.assertRaises(LedgerStorageError) as raised:
                ledger.transition("tenant", "two", "evaluating", "completed",
                                  decisions=tuple(f"decision-{i}" for i in range(1000)))
            self.assertEqual(str(raised.exception.__cause__), "database or disk is full")
            self.assertEqual(raised.exception.details["operation"], "transition")
            summary = ledger.summary(2)
            self.assertEqual(summary["registered"], 2)
            self.assertEqual(summary["states"]["completed"], 1)
            self.assertEqual(summary["decision_ids"], 1)
            self.assertEqual(ledger.connection.execute("SELECT count(*) FROM decision_effects").fetchone()[0], 1)
            self.assertFalse(summary["valid"])

    async def test_commit_and_rollback_failures_keep_original_cause_and_safe_counts(self) -> None:
        with TemporaryDirectory() as directory, IdentityLedger(Path(directory) / "ledger") as ledger:
            ledger.register("tenant", "durable")
            ledger.flush()
            ledger.register("tenant", "pending")
            connection = Mock(wraps=ledger.connection)
            connection.commit.side_effect = sqlite3.OperationalError("database or disk is full")
            connection.rollback.side_effect = sqlite3.OperationalError("disk I/O error")
            ledger.connection = connection
            with self.assertRaises(LedgerStorageError) as raised:
                ledger.flush()
            self.assertEqual(str(raised.exception.__cause__), "database or disk is full")
            self.assertTrue(raised.exception.details["durability_uncertain"])
            self.assertFalse(raised.exception.details["rollback_succeeded"])
            self.assertEqual(ledger.summary(2)["registered"], 1)
            self.assertFalse(ledger.summary(2)["valid"])
            self.assertEqual(connection.commit.call_count, 1)

    async def test_final_commit_error_does_not_replace_workload_error(self) -> None:
        with TemporaryDirectory() as directory:
            with self.assertRaisesRegex(ValueError, "original workload"):
                with IdentityLedger(Path(directory) / "ledger") as ledger:
                    connection = Mock(wraps=ledger.connection)
                    connection.commit.side_effect = sqlite3.OperationalError("database or disk is full")
                    ledger.connection = connection
                    raise ValueError("original workload")
            self.assertEqual(ledger.storage_error.details["category"], "identity_ledger_disk_full")

    async def test_pipeline_sqlite_full_cancels_workers_and_reports_committed_evidence(self) -> None:
        with TemporaryDirectory() as directory, IdentityLedger(Path(directory) / "ledger") as ledger:
            pages = ledger.connection.execute("PRAGMA page_count").fetchone()[0]
            ledger.connection.execute(f"PRAGMA max_page_count={pages + 1}")
            snapshots = []
            before = set(asyncio.all_tasks())
            with self.assertRaises(LedgerStorageError):
                await pipeline(FakeClients(), target=100, ingestion_concurrency=10, evaluation_concurrency=10,
                               ledger=ledger, on_progress=snapshots.append)
            self.assertEqual(set(asyncio.all_tasks()) - before, set())
            summary = snapshots[-1]["identity_reconciliation"]
            self.assertEqual(summary["storage_error"]["category"], "identity_ledger_disk_full")
            self.assertFalse(summary["valid"])
            actual = ledger.connection.execute("SELECT count(*) FROM identities WHERE state='completed'").fetchone()[0]
            self.assertEqual(summary["states"].get("completed", 0), actual)

    async def test_final_snapshot_failure_does_not_replace_original_ledger_error(self) -> None:
        class Broken(FakeClients):
            async def record_ingested(self, *args, **kwargs):
                raise LedgerInvariantError("original ledger error")

        snapshots = []
        def save(snapshot):
            snapshots.append(snapshot)
            if len(snapshots) > 1:
                raise OSError("disk full in final report")
        with self.assertRaisesRegex(LedgerInvariantError, "original ledger error"):
            await pipeline(Broken(), on_progress=save)

    async def test_reused_response_decision_aborts_pipeline_and_fails_acceptance(self) -> None:
        class ReusedDecision(FakeClients):
            async def record_ingested(self, tenant, object_id, *args, **kwargs):
                return {"result": {"object_id": object_id, "results": [{"triggered": True,
                    "decision": {"id": "reused", "tenant_id": tenant, "object_id": object_id}}]}}, 200, {}

        with TemporaryDirectory() as directory, IdentityLedger(Path(directory) / "ledger") as ledger:
            snapshots = []
            with self.assertRaisesRegex(LedgerInvariantError, "duplicate_decision"):
                await pipeline(ReusedDecision(), ledger=ledger, on_progress=snapshots.append)
            self.assertGreater(snapshots[-1]["identity_reconciliation"]["duplicate_observations"], 0)
            acceptance = _acceptance([{"phase": "empty_database", "evaluation": snapshots[-1]}])
            self.assertFalse(acceptance["passed"])
            self.assertIn("identity_reconciliation_failed", acceptance["comparisons"][0]["reliability_failures"])

    async def test_pipeline_records_every_completion_without_raw_identities(self) -> None:
        with TemporaryDirectory() as directory:
            path = Path(directory) / "ledger.sqlite3"
            with IdentityLedger(path) as ledger:
                result = await pipeline(FakeClients(), ledger=ledger)
                self.assertTrue(result["identity_reconciliation"]["valid"])
            with closing(sqlite3.connect(path)) as connection:
                self.assertEqual(connection.execute("SELECT count(*) FROM identities WHERE state='completed'").fetchone()[0], 10)
            self.assertNotIn(b"tenant-1", path.read_bytes())
            self.assertNotIn(b"object-0", path.read_bytes())
            with self.assertRaises(FileExistsError):
                IdentityLedger(path)

    async def test_duplicates_and_wrong_state_fail_without_partial_effects(self) -> None:
        with TemporaryDirectory() as directory, IdentityLedger(Path(directory) / "ledger") as ledger:
            for identity in ("one", "two"):
                ledger.register("tenant", identity)
                ledger.transition("tenant", identity, "ingesting", "ingested")
                ledger.transition("tenant", identity, "ingested", "evaluating")
            ledger.transition("tenant", "one", "evaluating", "completed", decisions=("decision",))
            with self.assertRaisesRegex(LedgerInvariantError, "duplicate_decision"):
                ledger.transition("tenant", "two", "evaluating", "completed", decisions=("new", "decision"))
            self.assertEqual(ledger.connection.execute("SELECT count(*) FROM decision_effects").fetchone()[0], 1)
            self.assertEqual(ledger.summary(2)["states"]["evaluating"], 1)
            with self.assertRaisesRegex(LedgerInvariantError, "duplicate_input"):
                ledger.register("tenant", "one")
            with self.assertRaisesRegex(LedgerInvariantError, "unexpected_identity_state"):
                ledger.transition("tenant", "one", "ingesting", "ingested")
            self.assertFalse(ledger.summary(2)["valid"])

    async def test_tenant_scoping_and_cancelled_pipeline_evidence(self) -> None:
        entered = asyncio.Event()
        class Hanging(FakeClients):
            async def record_ingested(self, *args, **kwargs):
                entered.set()
                await asyncio.Event().wait()

        with TemporaryDirectory() as directory:
            path = Path(directory) / "ledger"
            snapshots = []
            with IdentityLedger(path) as ledger:
                self.assertNotEqual(ledger.token("a", "same"), ledger.token("b", "same"))
                task = asyncio.create_task(pipeline(Hanging(), ledger=ledger, pipeline_timeout=30, on_progress=snapshots.append))
                await asyncio.wait_for(entered.wait(), 10)
                task.cancel()
                with self.assertRaises(asyncio.CancelledError):
                    await task
                final = snapshots[-1]["identity_reconciliation"]
                self.assertGreater(final["unfinished"], 0)
                self.assertFalse(final["valid"])
            with closing(sqlite3.connect(path)) as connection:
                self.assertGreater(connection.execute("SELECT count(*) FROM identities WHERE state='evaluating'").fetchone()[0], 0)


if __name__ == "__main__":
    unittest.main()
