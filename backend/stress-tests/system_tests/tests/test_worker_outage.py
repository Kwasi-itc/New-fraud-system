from __future__ import annotations

import asyncio
import json
import time
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import MagicMock, patch

from production_replay.benchmark_reporting import write_json_atomic
from system_tests.worker_outage import DockerWorker, run_outage, watchdog


class WorkerTargetTests(unittest.TestCase):
    def test_non_disposable_or_wrong_project_targets_refused_before_stop(self):
        worker = DockerWorker("a" * 64, "system-tests-unit")
        with patch.object(worker, "command", return_value=json.dumps([{
            "Id": "a" * 64, "Config": {"Labels": {"system-tests.disposable": "false"}}, "State": {"Running": True},
        }])) as command:
            with self.assertRaises(ValueError):
                worker.stop()
            command.assert_called_once_with("inspect", "a" * 64)
        with self.assertRaises(ValueError):
            DockerWorker("a" * 64, "production")

    def test_expired_watchdog_lease_restores_without_parent(self):
        with TemporaryDirectory() as directory:
            journal = Path(directory) / "fault.json"
            write_json_atomic(journal, {"schema_version": 1, "container_id": "a" * 64,
                "project": "system-tests-unit", "status": "active", "expires_at": time.time() - 1})
            worker = MagicMock()
            with patch("system_tests.worker_outage.DockerWorker", return_value=worker):
                watchdog(journal)
            worker.restore.assert_called_once()
            self.assertEqual(json.loads(journal.with_suffix(".watchdog.json").read_text())["status"], "restored")


class WorkerLifecycleTests(unittest.IsolatedAsyncioTestCase):
    async def test_cancellation_restores_worker_and_does_not_pass_outage(self):
        loop = asyncio.get_running_loop()
        stopped = asyncio.Event()
        worker = MagicMock(identity="a" * 64, project="system-tests-unit")
        worker.inspect.return_value = {"Running": True}
        worker.stop.side_effect = lambda: loop.call_soon_threadsafe(stopped.set)
        with TemporaryDirectory() as directory:
            journal = Path(directory) / "fault.json"
            child = MagicMock()
            child.poll.return_value = None
            def launch(*_args, **_kwargs):
                write_json_atomic(journal.with_suffix(".watchdog.json"), {"status": "ready"})
                return child
            def wait(_timeout):
                write_json_atomic(journal.with_suffix(".watchdog.json"), {"status": "restored"})
                return 0
            child.wait.side_effect = wait
            trigger = asyncio.Event()
            trigger.set()
            with patch("system_tests.worker_outage.subprocess.Popen", side_effect=launch):
                task = asyncio.create_task(run_outage(worker, journal, trigger, duration=10))
                await asyncio.wait_for(stopped.wait(), 2)
                task.cancel()
                with self.assertRaises(asyncio.CancelledError):
                    await task
            worker.restore.assert_called_once()
            state = json.loads(journal.read_text())
            self.assertTrue(state["restored"])
            self.assertFalse(state["passed"])

    async def test_missing_stage_times_out_without_mutation(self):
        worker = MagicMock()
        with TemporaryDirectory() as directory:
            with self.assertRaises(TimeoutError):
                await run_outage(worker, Path(directory) / "fault.json", asyncio.Event(), duration=1, trigger_timeout=0.01)
        worker.stop.assert_not_called()


if __name__ == "__main__":
    unittest.main()
