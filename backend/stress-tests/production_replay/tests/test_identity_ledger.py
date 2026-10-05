from __future__ import annotations

import asyncio
import sqlite3
import unittest
from contextlib import closing
from pathlib import Path
from tempfile import TemporaryDirectory

from production_replay.identity_ledger import IdentityLedger, LedgerInvariantError
from production_replay.database_scale_suite import _acceptance
from production_replay.tests.test_benchmark_foundation import FakeClients, pipeline


class IdentityTests(unittest.IsolatedAsyncioTestCase):
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
