"""Exercise the separate asyncio timeout type used before Python 3.11."""
from __future__ import annotations

import asyncio
import contextlib
import io
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import AsyncMock, MagicMock, patch

import httpx

from production_replay import database_scale_suite
from production_replay.benchmark_observation import RuntimeObserver
from production_replay.database_reconciliation import reconcile_database
from production_replay.decision_completion import DecisionCompletionError, verify_decision_completion
from production_replay.identity_ledger import IdentityLedger
from production_replay.postgres_observation import PostgresObserver
from production_replay.tests.test_benchmark_foundation import FakeClients, execution
from production_replay.tests.test_database_scale_suite import event


class LegacyAsyncioTimeoutError(Exception):
    """Deliberately not a subclass of the built-in TimeoutError."""


@contextlib.contextmanager
def legacy_timeouts(*, shorten_progress_interval: bool = False):
    original_wait = asyncio.wait_for
    original_errors = (asyncio.TimeoutError, TimeoutError)

    async def wait(awaitable, timeout):
        if shorten_progress_interval and timeout == 5.0:
            timeout = .005
        try:
            return await original_wait(awaitable, timeout)
        except original_errors as exc:
            raise LegacyAsyncioTimeoutError() from exc

    with patch.object(asyncio, "TimeoutError", LegacyAsyncioTimeoutError), \
         patch.object(asyncio, "wait_for", wait):
        yield


class TimeoutCompatibilityTests(unittest.IsolatedAsyncioTestCase):
    async def test_observer_keeps_sampling_after_legacy_interval_timeout(self):
        sampled_during_work = asyncio.Event()
        with TemporaryDirectory() as directory:
            async with RuntimeObserver(Path(directory)/"metrics", {}, interval=.005) as observer:
                async def sample():
                    if observer.sample.await_count >= 2:
                        sampled_during_work.set()

                observer.sample = AsyncMock(side_effect=sample)
                with legacy_timeouts():
                    await observer.run_during(sampled_during_work.wait)
                self.assertGreaterEqual(observer.sample.await_count, 3)

    async def test_progress_keeps_reporting_after_legacy_interval_timeout(self):
        reported_during_work = asyncio.Event()

        class Slow(FakeClients):
            async def ingest_one(self, *_args, **_kwargs):
                await reported_during_work.wait()
                return {}, 1

        snapshots = []

        def progress(snapshot):
            snapshots.append(snapshot)
            if len(snapshots) >= 2:
                reported_during_work.set()

        with legacy_timeouts(shorten_progress_interval=True):
            report = await database_scale_suite._run_fixed_pipeline(
                Slow(), "tenant-1", [event(0)], target=1, ingestion_concurrency=1,
                evaluation_concurrency=1, segment_size=1, pipeline_timeout=10,
                expected_scenarios=1, on_progress=progress)
        self.assertEqual(report["decision"]["successes"], 1)
        self.assertGreaterEqual(len(snapshots), 3)

    async def test_remote_timeout_is_failed_metrics_not_uncaught_exception(self):
        async def handler(_request):
            await asyncio.Event().wait()

        with TemporaryDirectory() as directory:
            async with RuntimeObserver(Path(directory)/"metrics", {"decision": "http://decision"},
                                       timeout=.005, transport=httpx.MockTransport(handler)) as observer:
                with legacy_timeouts():
                    report = await observer._remote("decision", "http://decision")
                self.assertEqual(report["status"], "error")
                self.assertEqual(report["error_type"], "LegacyAsyncioTimeoutError")

    async def test_database_observation_timeout_reaps_subprocess(self):
        process = MagicMock(returncode=None)
        calls = 0

        async def communicate():
            nonlocal calls
            calls += 1
            if calls == 1:
                await asyncio.Event().wait()
            return b"", b""

        process.communicate = communicate
        with patch("asyncio.create_subprocess_exec", AsyncMock(return_value=process)), legacy_timeouts():
            report = await PostgresObserver(["psql"], {}, .005)()
        self.assertEqual(report["status"], "error")
        process.kill.assert_called_once()
        self.assertEqual(calls, 2)

    async def test_reconciliation_timeout_is_invalid_and_reaps_subprocess(self):
        async def lines():
            await asyncio.Event().wait()
            yield b"unreachable"

        process = MagicMock(returncode=None, stdout=lines())
        process.wait = AsyncMock(return_value=0)
        with TemporaryDirectory() as directory, IdentityLedger(Path(directory)/"ledger") as ledger:
            with patch("asyncio.create_subprocess_exec", AsyncMock(return_value=process)), legacy_timeouts():
                report = await reconcile_database(["psql"], {}, ledger,
                    "11111111-1111-1111-1111-111111111111", 0, 1, .005)
        self.assertFalse(report["valid"])
        self.assertEqual(report["error_type"], "TimeoutError")
        process.kill.assert_called_once()
        process.wait.assert_awaited_once()

    async def test_decision_deadline_remains_a_completion_failure(self):
        clients = FakeClients()
        clients.get_async_decision_execution = AsyncMock(return_value={"async_decision_execution": execution()})
        with legacy_timeouts(), self.assertRaisesRegex(DecisionCompletionError, "decision_completion_timeout"):
            await verify_decision_completion(clients, "tenant-1", "object-0",
                {"async_decision_execution": execution()}, 202,
                allow_deferred=True, timeout_seconds=.02, poll_interval_seconds=.005)

    async def test_pipeline_cancellation_retrieves_all_group_exceptions(self):
        entered = asyncio.Event()

        class Stuck(FakeClients):
            async def ingest_one(self, *_args, **_kwargs):
                entered.set()
                await asyncio.Event().wait()

        loop = asyncio.get_running_loop()
        previous = loop.get_exception_handler()
        unhandled = []
        loop.set_exception_handler(lambda _loop, context: unhandled.append(context))
        try:
            task = asyncio.create_task(database_scale_suite._run_fixed_pipeline(
                Stuck(), "tenant-1", [event(0)], target=1, ingestion_concurrency=1,
                evaluation_concurrency=1, segment_size=1))
            await asyncio.wait_for(entered.wait(), 2)
            task.cancel()
            with self.assertRaises(asyncio.CancelledError):
                await task
            del task
            await asyncio.sleep(0)
            self.assertEqual(unhandled, [])
        finally:
            loop.set_exception_handler(previous)


class EntrypointTimeoutTests(unittest.TestCase):
    def test_legacy_timeout_is_reported_without_traceback(self):
        stderr = io.StringIO()
        with patch.object(asyncio, "TimeoutError", LegacyAsyncioTimeoutError), \
             patch.object(database_scale_suite, "async_main", AsyncMock(side_effect=LegacyAsyncioTimeoutError)), \
             contextlib.redirect_stderr(stderr), self.assertRaises(SystemExit) as raised:
            database_scale_suite.main([])
        self.assertEqual(raised.exception.code, 1)
        self.assertIn("error:", stderr.getvalue())
