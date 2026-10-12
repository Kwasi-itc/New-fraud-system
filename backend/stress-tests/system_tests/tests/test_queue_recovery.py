from __future__ import annotations

import asyncio
import json
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import AsyncMock, patch

from system_tests.queue_campaign import async_main
from system_tests.queue_recovery import (
    STATES, QueueCollector, QueueRecoveryMonitor, RecoveryGate, RecoveryPolicy, queue_sql,
)


def snapshot(count=0, age=0, exact=True, paused=False):
    return {"status": "ok", "queues": {"decisions": {
        "active_jobs": count, "oldest_due_seconds": age, "counts_exact": exact, "paused": paused,
    }}}


def raw():
    return {"status": "ok", "metrics": {
        "database": "test_shared", "registry": [{"queue": "decisions", "registered": True, "paused": False}],
        "states": [{"queue": "decisions", "state": state, "observed_jobs": 0,
                    "truncated": False, "oldest_due_seconds": 0} for state in STATES],
    }}


class RecoveryTests(unittest.TestCase):
    def gate(self):
        return RecoveryGate(["decisions"], RecoveryPolicy(
            start=10, deadline=10, hold=4, max_gap=5, max_active=2, max_due_age=3))

    def test_burst_allowed_then_sustained_recovery(self):
        gate = self.gate()
        for elapsed in (0, 5, 9):
            gate.observe(elapsed, snapshot(count=100))
        gate.observe(10, snapshot())
        gate.observe(12, snapshot())
        self.assertFalse(gate.summary(finished=True)["passed"])
        gate.observe(14, snapshot())
        self.assertTrue(gate.summary(finished=True)["passed"])
        self.assertEqual(gate.summary(finished=True)["confirmed_seconds_after_recovery_start"], 4)
        self.assertFalse(gate.summary(finished=False)["passed"])

    def test_bad_reading_resets_hold_and_late_recovery_fails(self):
        for unhealthy in (snapshot(count=3), snapshot(age=4), snapshot(exact=False), snapshot(paused=True)):
            with self.subTest(unhealthy=unhealthy):
                gate = self.gate()
                for elapsed in (0, 5, 10, 14):
                    gate.observe(elapsed, snapshot())
                self.assertTrue(gate.summary(finished=True)["passed"])
                gate.observe(17, unhealthy)
                gate.observe(18, snapshot())
                gate.observe(22, snapshot())
                self.assertFalse(gate.summary(finished=True)["passed"])

    def test_missing_data_or_sample_gap_cannot_pass(self):
        for missing in ({"status": "error"}, {"status": "ok", "queues": {}}):
            gate = self.gate()
            gate.observe(0, missing)
            for elapsed in (5, 10, 14):
                gate.observe(elapsed, snapshot())
            self.assertFalse(gate.summary(finished=True)["passed"])
        gate = self.gate()
        for elapsed in (0, 10, 14):
            gate.observe(elapsed, snapshot())
        self.assertFalse(gate.summary(finished=True)["passed"])

    def test_equal_clock_ticks_do_not_count_as_time_reversal(self):
        gate = self.gate()
        for elapsed in (0, 0, 5, 10, 14, 14):
            gate.observe(elapsed, snapshot())
        self.assertTrue(gate.summary(finished=True)["passed"])
        gate.observe(13, snapshot())
        self.assertFalse(gate.summary(finished=True)["passed"])

    def test_sql_is_bounded_per_named_queue_and_state_and_excludes_payloads(self):
        sql = queue_sql("public", ["decisions", "outbox"], 100)
        self.assertEqual(sql.count("LIMIT 101"), 10)
        self.assertIn('"public".river_job', sql)
        self.assertIn('"public".river_queue', sql)
        self.assertIn("WHERE queue = 'decisions'", sql)
        self.assertNotIn("args", sql)
        self.assertNotIn("completed", sql)
        for schema, queues in (("public;drop", ["x"]), ("public", ["x' OR true"]), ("public", ["x", "x"])):
            with self.assertRaises(ValueError):
                queue_sql(schema, queues, 100)


class CollectorTests(unittest.IsolatedAsyncioTestCase):
    async def test_zero_counts_are_explicit_and_caps_remain_lower_bounds(self):
        database = AsyncMock()
        database.query.return_value = raw()
        collector = QueueCollector(database, "test_shared", "public", ["decisions"], 10)
        result = await collector()
        self.assertEqual(result["queues"]["decisions"]["active_jobs"], 0)
        self.assertTrue(result["queues"]["decisions"]["counts_exact"])
        data = raw()
        data["metrics"]["states"][0].update(observed_jobs=11, truncated=True, oldest_due_seconds=5)
        database.query.return_value = data
        result = await collector()
        self.assertFalse(result["queues"]["decisions"]["counts_exact"])
        self.assertEqual(result["queues"]["decisions"]["oldest_due_seconds"], 5)

    async def test_wrong_database_unregistered_queue_and_incomplete_states_fail(self):
        cases = []
        value = raw()
        value["metrics"]["database"] = "application_not_test"
        cases.append(value)
        value = raw()
        value["metrics"]["registry"][0]["registered"] = False
        cases.append(value)
        value = raw()
        value["metrics"]["states"].pop()
        cases.append(value)
        value = raw()
        value["metrics"]["states"][0]["oldest_due_seconds"] = float("nan")
        cases.append(value)
        for value in cases:
            database = AsyncMock()
            database.query.return_value = value
            result = await QueueCollector(database, "test_shared", "public", ["decisions"])()
            self.assertEqual(result["status"], "error")
            self.assertNotIn("application_not_test", json.dumps(result))

    async def test_preflight_failure_prevents_any_workload(self):
        gate = RecoveryGate(["decisions"], RecoveryPolicy(0, 1, 0.01, 1, 0, 1))
        with TemporaryDirectory() as directory:
            monitor = QueueRecoveryMonitor(AsyncMock(return_value={"status": "error"}),
                gate, Path(directory) / "queues.ndjson", 0.01)
            work = AsyncMock()
            with self.assertRaisesRegex(ValueError, "preflight"):
                await monitor.run_during(work)
            work.assert_not_called()
            self.assertFalse(monitor.summary()["passed"])

    async def test_observer_exception_cancels_workload(self):
        stopped = asyncio.Event()
        async def workload():
            try:
                await asyncio.Event().wait()
            finally:
                stopped.set()
        with TemporaryDirectory() as directory:
            gate = RecoveryGate(["decisions"], RecoveryPolicy(0, 1, 0.01, 1, 0, 1))
            monitor = QueueRecoveryMonitor(AsyncMock(side_effect=[snapshot(), RuntimeError("collector")]),
                gate, Path(directory) / "queues.ndjson", 0.01)
            with self.assertRaises(RuntimeError):
                await monitor.run_during(workload)
            self.assertTrue(stopped.is_set())
            self.assertFalse(monitor.summary()["passed"])

    async def test_cli_combines_queue_gate_with_successful_transaction_gate(self):
        from system_tests.tests.test_queue_campaign import Clients
        class ManagedClients(Clients):
            async def __aenter__(self):
                return self

            async def __aexit__(self, *_args):
                pass

        clients = ManagedClients()
        for active, expected_code in ((0, 0), (5, 2)):
            with self.subTest(active=active), TemporaryDirectory() as directory:
                root = Path(directory)
                source = root / "input.ndjson"
                source.write_text('{"object_id":"one"}\n')
                with patch("system_tests.queue_campaign.ServiceClients", return_value=clients), \
                     patch("system_tests.queue_campaign.capture_environment", return_value={}), \
                     patch("system_tests.queue_campaign.QueueCollector", return_value=AsyncMock(return_value=snapshot(count=active))):
                    code = await async_main([
                        "--input", str(source), "--output", str(root / "run"), "--tenant-id", "t",
                        "--stage", "recovery:1:0.15", "--expected-scenarios", "1", "--deadline", "1",
                        "--poll-interval", "0.001", "--queue", "decisions", "--queue-database", "test_shared",
                        "--queue-recovery-stage", "recovery", "--queue-recovery-deadline", "0.15",
                        "--queue-healthy-seconds", "0.02", "--queue-max-active", "0",
                        "--queue-max-due-age", "1", "--queue-interval", "0.01"])
                report = json.loads((root / "run/summary.json").read_text())
                self.assertEqual(code, expected_code, report)
                self.assertEqual(report["counts"]["completed"], 1)
                self.assertEqual(report["queue_recovery"]["passed"], active == 0)


if __name__ == "__main__":
    unittest.main()
