from __future__ import annotations

import asyncio
import hashlib
import json
import threading
import unittest
from collections import Counter
from dataclasses import replace
from datetime import datetime, timezone
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import AsyncMock, MagicMock, patch

from production_replay.benchmark_observation import check_runtime_pools
from production_replay.benchmark_reporting import RunReport
from production_replay.database_scale_suite import (
    PhasePlan, _acceptance, _build_phase_plans, _prepare_evaluation_sources, _prepare_sources, _run_phase,
)
from production_replay.sorting import AsyncEventSource
from production_replay.domain import TransactionEvent
from production_replay.tests.test_benchmark_foundation import FakeClients, phase
from production_replay.tests.test_database_scale_suite import event


class PreparedSourceTests(unittest.TestCase):
    def test_real_selection_excludes_prior_month_and_reserved_prefix(self):
        from argparse import Namespace
        records = [replace(event(i), occurred_at=datetime(2026, 6 if i < 2 else 7, 1, tzinfo=timezone.utc))
                   for i in range(9)]
        args = Namespace(evaluation_count=2, evaluation_offset=3, evaluation_cohort="fixed",
                         same_month="2026-07", seed_month="2026-06", phase=["empty", "1month"])
        with TemporaryDirectory() as directory:
            chunk = Path(directory)/"chunk.ndjson"
            chunk.write_text("".join(json.dumps(e.to_sort_record(i))+"\n" for i, e in enumerate(records)))
            plans = _build_phase_plans((chunk,), 9, Counter({"2026-06": 2, "2026-07": 7}), args)
            prepared = _prepare_evaluation_sources(plans, Path(directory), 2)
            for plan in prepared:
                self.assertEqual([e.object_id for e in plan.evaluation_factory()], ["object-5", "object-6", "object-7", "object-8"])
            self.assertEqual([e.object_id for e in prepared[1].seed_factory()], ["object-0", "object-1"])

    def test_reuses_fixed_selection_preserves_order_and_replacement_tail(self):
        records = [event(i) for i in range(8)]
        factory = MagicMock(side_effect=lambda: iter(records))
        plans = [PhasePlan(name, 0, lambda: iter(()), factory, "test", "2026-07", 5)
                 for name in ("empty_database", "seed_1m_same_month", "seed_5m_same_month", "seed_full_month_next_month")]
        with TemporaryDirectory() as directory:
            prepared = _prepare_evaluation_sources(plans, Path(directory), 3)
            factory.assert_called_once()
            self.assertEqual(len(list(Path(directory).glob("evaluation-*.ndjson"))), 1)
            for plan in prepared:
                self.assertEqual(list(plan.evaluation_factory()), [TransactionEvent.from_sort_record(
                    e.to_sort_record(i)) for i, e in enumerate(records)])
                self.assertEqual(plan.prepared_source["records_available"], 8)
                digest = hashlib.sha256()
                for e in records[:3]:
                    digest.update(json.dumps(e.fields, sort_keys=True, separators=(",", ":"), default=str).encode() + b"\n")
                self.assertEqual(plan.prepared_source["target_sha256"], digest.hexdigest())

    def test_distinct_offsets_do_not_reuse_wrong_selection(self):
        plans = [PhasePlan("first", 0, lambda: iter(()), lambda: iter([event(0)]), "test"),
                 PhasePlan("second", 0, lambda: iter(()), lambda: iter([event(1)]), "test", evaluation_offset=1)]
        with TemporaryDirectory() as directory:
            prepared = _prepare_evaluation_sources(plans, Path(directory), 1)
            self.assertNotEqual(prepared[0].prepared_source["target_sha256"], prepared[1].prepared_source["target_sha256"])
            self.assertEqual(next(prepared[1].evaluation_factory()).object_id, "object-1")

    def test_insufficient_or_broken_source_is_not_silently_accepted(self):
        def broken():
            yield event(0)
            raise OSError("disk failure")
        with TemporaryDirectory() as directory:
            for factory, error in ((lambda: iter(()), ValueError), (broken, OSError)):
                with self.assertRaises(error):
                    _prepare_evaluation_sources([PhasePlan("test", 0, lambda: iter(()), factory, "test")],
                                                Path(directory), 2)
                for path in Path(directory).iterdir():
                    path.unlink()


class SourceConcurrencyTests(unittest.IsolatedAsyncioTestCase):
    async def test_blocking_source_read_does_not_block_event_loop_and_closes_on_owner_thread(self):
        gate = threading.Event()
        owners = []
        closed = threading.Event()
        def records():
            owners.append(threading.get_ident())
            try:
                if not gate.wait(2):
                    raise AssertionError("event loop did not release reader")
                yield event(0)
            finally:
                owners.append(threading.get_ident())
                closed.set()
        source = AsyncEventSource(records())
        task = asyncio.create_task(source.next())
        await asyncio.sleep(.01)
        self.assertFalse(task.done())
        gate.set()
        self.assertEqual((await task).object_id, "object-0")
        await source.close()
        self.assertTrue(closed.is_set())
        self.assertEqual(len(set(owners)), 1)
        self.assertNotEqual(owners[0], threading.get_ident())

    async def test_cancelled_read_is_joined_and_closed(self):
        entered = threading.Event()
        gate = threading.Event()
        closed = threading.Event()
        def records():
            try:
                entered.set()
                gate.wait(2)
                yield event(0)
            finally:
                closed.set()
        source = AsyncEventSource(records())
        task = asyncio.create_task(source.next())
        while not entered.is_set():
            await asyncio.sleep(.001)
        task.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await task
        gate.set()
        await source.close()
        self.assertTrue(closed.is_set())
        self.assertTrue(source.pending.done())

    async def test_source_preparation_runs_off_event_loop_and_cancellation_closes_it(self):
        entered = threading.Event()
        gate = threading.Event()
        closed = threading.Event()
        def records():
            try:
                entered.set()
                gate.wait(2)
                yield event(0)
            finally:
                closed.set()
        with TemporaryDirectory() as directory:
            task = asyncio.create_task(_prepare_sources(
                [PhasePlan("test", 0, lambda: iter(()), records, "test")], Path(directory), 1))
            while not entered.is_set():
                await asyncio.sleep(.001)
            task.cancel()
            gate.set()
            with self.assertRaises(asyncio.CancelledError):
                await task
            self.assertTrue(closed.is_set())


class PoolReadinessTests(unittest.IsolatedAsyncioTestCase):
    def deployment(self, maximum="8"):
        return {"status": "ok", "containers": [{"service": name,
                "runtime_settings": {"DATABASE_MAX_CONNS": maximum}}
                for name in ("decision-engine-service", "ingestion-service")]}

    async def test_maximum_is_verified_not_assumed_from_environment(self):
        for maximum, status in (("8", "ok"), ("24", "error"), ("0", "ok"), ("bad", "error")):
            with self.subTest(maximum=maximum):
                report = await check_runtime_pools(FakeClients(), self.deployment(maximum), .1)
                self.assertEqual(report["status"], status)
        phase_ = phase()
        phase_["runtime_pools"] = {"status": "error"}
        self.assertFalse(_acceptance([phase_])["passed"])

    async def test_invalid_metrics_and_timeout_fail_without_leaking_payload(self):
        clients = FakeClients()
        for response in ({"runtime_metrics": {"db_pool": {"MaxConns": True}}},
                         {"runtime_metrics": {"db_pool": None}}, {"secret": "DO_NOT_SAVE"}):
            clients.request = AsyncMock(return_value=response)
            report = await check_runtime_pools(clients, self.deployment(), .1)
            self.assertEqual(report["status"], "error")
            self.assertNotIn("DO_NOT_SAVE", json.dumps(report))
        async def stuck(*_args):
            await asyncio.Event().wait()
        clients.request = stuck
        self.assertEqual((await check_runtime_pools(clients, self.deployment(), .01))["status"], "error")

    async def test_pool_mismatch_stops_before_setup_or_seeding(self):
        from argparse import Namespace
        args = Namespace(data_model_url="http://data", ingestion_url="http://ingestion", decision_engine_url="http://decision",
                         auth_token=None, request_timeout=1, ingestion_concurrency=1, evaluation_concurrency=1)
        database = MagicMock()
        database.deployment.return_value = self.deployment("24")
        plan = PhasePlan("test", 0, lambda: iter(()), lambda: iter(()), "test")
        with TemporaryDirectory() as directory, \
             patch("production_replay.database_scale_suite.ServiceClients", return_value=FakeClients()), \
             patch("production_replay.database_scale_suite.EnvironmentSetup") as setup:
            with self.assertRaisesRegex(ValueError, "runtime_pool_preflight_failed"), RunReport(Path(directory)) as report:
                await _run_phase(plan, args, MagicMock(), database, report)
            setup.assert_not_called()
            database.stats.assert_not_called()
            evidence = json.loads((Path(directory)/"test.json").read_text())
            self.assertEqual(evidence["runtime_pools"]["status"], "error")
            self.assertEqual(evidence["stage"], "checking_runtime_pools")
