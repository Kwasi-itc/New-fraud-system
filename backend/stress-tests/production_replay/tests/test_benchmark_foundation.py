from __future__ import annotations

import argparse
import asyncio
import copy
import json
import unittest
from collections import Counter
from pathlib import Path
from tempfile import TemporaryDirectory
from typing import Any
from unittest.mock import AsyncMock, MagicMock, patch

from production_replay.api_client import APIError
from production_replay.benchmark_reporting import RunReport, write_json_atomic
from production_replay.database_scale_suite import (
    PhasePlan, QUIESCED_SERVICES, _acceptance, _build_phase_plans, _run_fixed_pipeline, _run_phase,
    _validate_args, async_main, build_parser,
)
from production_replay.decision_completion import DecisionCompletionError, verify_decision_completion
from production_replay.tests.test_database_scale_suite import event


def result(object_id: str = "object-0") -> dict[str, Any]:
    return {"object_id": object_id, "results": [{"triggered": False}]}


def execution(status: str = "queued", **overrides: Any) -> dict[str, Any]:
    return {"id": "exec-1", "tenant_id": "tenant-1", "object_type": "transactions",
            "status": status, **overrides}


class FakeClients:
    decision_engine = "decision"
    ingestion = "ingestion"

    async def request(self, _client, _method, path, _expected):
        envelope = "runtime_metrics" if path.endswith("runtime-metrics") else "read_metrics"
        return {envelope: {"db_pool": {"MaxConns": 8}}}
    async def __aenter__(self) -> FakeClients:
        return self

    async def __aexit__(self, *_args: Any) -> None:
        pass

    async def wait_until_ready(self, **_kwargs: Any) -> None:
        pass

    async def ingest_one(self, *_args: Any, **_kwargs: Any) -> tuple[dict[str, Any], int]:
        await asyncio.sleep(0)
        return {}, 1

    async def record_ingested(self, _tenant: str, object_id: str, *_args: Any, **_kwargs: Any) -> Any:
        return {"result": result(object_id)}, 200, {}

    async def get_async_decision_execution(self, tenant: str, identity: str) -> dict[str, Any]:
        assert (tenant, identity) == ("tenant-1", "exec-1")
        return {"async_decision_execution": execution("completed", result_body=result())}


async def pipeline(clients: Any, **kwargs: Any) -> dict[str, Any]:
    options = dict(target=10, ingestion_concurrency=2, evaluation_concurrency=2, segment_size=3,
                   pipeline_timeout=2, expected_scenarios=1)
    options.update(kwargs)
    return await _run_fixed_pipeline(clients, "tenant-1", [event(i) for i in range(100)], **options)


class CompletionTests(unittest.IsolatedAsyncioTestCase):
    async def verify(self, response: Any, status: int = 202, **kwargs: Any) -> None:
        options = dict(allow_deferred=True, timeout_seconds=0.2, poll_interval_seconds=0.001,
                       expected_scenarios=1)
        options.update(kwargs)
        await verify_decision_completion(FakeClients(), "tenant-1", "object-0", response, status, **options)

    async def test_sync_result_requires_object_and_scenario_evidence(self) -> None:
        await self.verify({"result": result()}, 200)
        for body in ({}, {"result": result("wrong")}, {"result": {"object_id": "object-0", "results": []}}):
            with self.subTest(body=body), self.assertRaises(DecisionCompletionError):
                await self.verify(body, 200)

    async def test_valid_fraud_decline_is_a_success(self) -> None:
        body = result()
        body["results"] = [{"triggered": True, "decision": {
            "id": "decision-1", "tenant_id": "tenant-1", "object_id": "object-0", "outcome": "decline",
        }}]
        await self.verify({"result": body}, 200)
        body["results"][0]["decision"]["tenant_id"] = "other-tenant"
        with self.assertRaisesRegex(DecisionCompletionError, "invalid_decision_identity"):
            await self.verify({"result": body}, 200)

    async def test_deferred_request_is_followed_to_completed(self) -> None:
        await self.verify({"async_decision_execution": execution()})

    async def test_default_policy_rejects_deferred_even_when_status_is_200(self) -> None:
        with self.assertRaisesRegex(DecisionCompletionError, "deferred_rejected"):
            await self.verify({"deferred": True}, 200, allow_deferred=False)

    async def test_missing_execution_id_and_unknown_state_are_not_successes(self) -> None:
        for body in ({}, {"async_decision_execution": execution("mystery")}):
            with self.subTest(body=body), self.assertRaises(DecisionCompletionError):
                await self.verify(body)

    async def test_failed_execution_is_terminal_failure(self) -> None:
        with self.assertRaisesRegex(DecisionCompletionError, "async_execution_failed") as raised:
            await self.verify({"async_decision_execution": execution("failed")})
        self.assertFalse(raised.exception.unresolved)

    async def test_wrong_tenant_or_execution_id_is_rejected(self) -> None:
        with self.assertRaisesRegex(DecisionCompletionError, "invalid_execution_identity"):
            await self.verify({"async_decision_execution": execution(tenant_id="wrong")})
        with patch.object(FakeClients, "get_async_decision_execution",
                          return_value={"async_decision_execution": execution("completed", id="wrong")}):
            with self.assertRaisesRegex(DecisionCompletionError, "invalid_execution_identity"):
                await self.verify({"async_decision_execution": execution()})

    async def test_async_domain_decision_identity_is_validated(self) -> None:
        body = result()
        body["results"] = [{"triggered": True, "decision": {
            "ID": "decision-1", "TenantID": "tenant-1", "ObjectID": "object-0",
        }}]
        await self.verify({"async_decision_execution": execution("completed", result_body=body)})

    async def test_deadline_includes_slow_status_request(self) -> None:
        cancelled = asyncio.Event()

        async def stuck(*_args: Any) -> Any:
            try:
                await asyncio.Event().wait()
            finally:
                cancelled.set()

        with patch.object(FakeClients, "get_async_decision_execution", stuck):
            with self.assertRaisesRegex(DecisionCompletionError, "decision_completion_timeout"):
                await self.verify({"async_decision_execution": execution()}, timeout_seconds=2)
        self.assertTrue(cancelled.is_set())


class PipelineTests(unittest.IsolatedAsyncioTestCase):
    async def test_successes_and_final_partial_segment(self) -> None:
        report = await pipeline(FakeClients())
        self.assertEqual(report["decision"]["successes"], 10)
        self.assertEqual(report["decision"]["unresolved"], 0)
        self.assertEqual(report["decision"]["pending_submission"], 0)
        self.assertEqual(report["ingestion"]["unfinished"], 0)
        self.assertGreater(report["decision"]["successful_evaluations_per_second"], 0)
        self.assertEqual([segment["requests"] for segment in report["segments"]], [3, 3, 3, 1])

    async def test_fast_errors_have_zero_successful_throughput(self) -> None:
        class Failing(FakeClients):
            async def record_ingested(self, *_args: Any, **_kwargs: Any) -> Any:
                raise APIError("busy", status_code=503)

        report = await pipeline(Failing())
        self.assertEqual(report["decision"]["failures"], 10)
        self.assertEqual(report["decision"]["successful_evaluations_per_second"], 0)
        self.assertEqual(report["decision"]["unresolved"], 10)

    async def test_failed_ingest_retry_attempts_are_counted(self) -> None:
        class RetryFailure(FakeClients):
            failed = False

            async def ingest_one(self, *_args: Any, **_kwargs: Any) -> Any:
                if not self.failed:
                    self.failed = True
                    raise APIError("unavailable", status_code=503, attempts=3)
                return {}, 1

        report = await pipeline(RetryFailure())
        self.assertEqual(report["ingestion"]["started"], 11)
        self.assertEqual(report["ingestion"]["retries"], 2)
        self.assertEqual(report["ingestion"]["failures"], 1)

    async def test_deferred_is_fatal_by_default_and_has_partial_metrics(self) -> None:
        class Deferred(FakeClients):
            async def record_ingested(self, *_args: Any, **_kwargs: Any) -> Any:
                return {"async_decision_execution": execution()}, 202, {}

        snapshots = []
        with self.assertRaisesRegex(DecisionCompletionError, "deferred_rejected"):
            await pipeline(Deferred(), on_progress=snapshots.append)
        self.assertGreater(snapshots[-1]["decision"]["deferred"], 0)
        self.assertEqual(snapshots[-1]["decision"]["successes"], 0)

    async def test_wait_policy_counts_only_verified_completion(self) -> None:
        class Deferred(FakeClients):
            async def record_ingested(self, *_args: Any, **_kwargs: Any) -> Any:
                return {"async_decision_execution": execution()}, 202, {}

        report = await pipeline(Deferred(), target=1, allow_deferred=True, decision_poll_interval=0.001)
        self.assertEqual(report["decision"]["deferred"], 1)
        self.assertEqual(report["decision"]["successes"], 1)
        self.assertEqual(report["decision"]["request_latency"]["count"], 1)

    async def test_consumer_exception_while_producers_fill_queue_does_not_hang(self) -> None:
        class Broken(FakeClients):
            async def record_ingested(self, *_args: Any, **_kwargs: Any) -> Any:
                raise RuntimeError("consumer broke")

        before = set(asyncio.all_tasks())
        with self.assertRaisesRegex(RuntimeError, "consumer broke"):
            await asyncio.wait_for(pipeline(Broken(), target=100, evaluation_concurrency=1), 0.5)
        self.assertEqual(set(asyncio.all_tasks()) - before, set())

    async def test_pipeline_timeout_cancels_inflight_work_and_reports_unfinished(self) -> None:
        class Stuck(FakeClients):
            async def ingest_one(self, *_args: Any, **_kwargs: Any) -> Any:
                await asyncio.Event().wait()

        snapshots = []
        with self.assertRaises((asyncio.TimeoutError, TimeoutError)):
            await pipeline(Stuck(), pipeline_timeout=0.02, on_progress=snapshots.append)
        self.assertEqual(snapshots[-1]["ingestion"]["unfinished"], 2)

    async def test_cancellation_is_preserved_and_saves_snapshot(self) -> None:
        started = asyncio.Event()

        class Stuck(FakeClients):
            async def record_ingested(self, *_args: Any, **_kwargs: Any) -> Any:
                started.set()
                await asyncio.Event().wait()

        snapshots = []
        task = asyncio.create_task(pipeline(Stuck(), on_progress=snapshots.append))
        await started.wait()
        task.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await task
        self.assertGreater(snapshots[-1]["decision"]["unresolved"], 0)

    async def test_source_exhaustion_preserves_metrics(self) -> None:
        snapshots = []
        with self.assertRaisesRegex(ValueError, "source exhausted"):
            await pipeline(FakeClients(), target=101, on_progress=snapshots.append)
        self.assertEqual(snapshots[-1]["ingestion"]["successes"], 100)

    async def test_progress_writer_failure_is_not_ignored(self) -> None:
        def fail(_snapshot: Any) -> None:
            raise OSError("disk full")

        with self.assertRaisesRegex(OSError, "disk full"):
            await pipeline(FakeClients(), on_progress=fail)


def phase() -> dict[str, Any]:
    return {"phase": "empty_database", "runtime_pools": {"status": "ok"},
            "database": {"durable_reconciliation": {"valid": True}}, "evaluation": {
        "evaluation_source": {"sha256": "same-corpus"},
        "identity_reconciliation": {"valid": True},
        "target_evaluations": 100,
        "ingestion": {"successes": 100, "failures": 0, "requests_per_second": 100,
                      "latency": {"p95_ms": 100}},
        "decision": {"successes": 100, "failures": 0, "unresolved": 0,
                     "successful_evaluations_per_second": 100, "latency": {"p95_ms": 100}},
    }}


class AcceptanceAndReportTests(unittest.TestCase):
    def test_failures_cannot_pass_despite_unchanged_performance(self) -> None:
        for stage in ("ingestion", "decision"):
            with self.subTest(stage=stage):
                bad = copy.deepcopy(phase())
                bad["evaluation"][stage]["failures"] = 1
                result_ = _acceptance([phase(), bad])
                self.assertFalse(result_["passed"])
                self.assertTrue(result_["comparisons"][1]["performance_passed"])

    def test_missing_completions_and_unresolved_work_fail(self) -> None:
        for key, value in (("successes", 99), ("unresolved", 1)):
            bad = phase()
            bad["evaluation"]["decision"][key] = value
            self.assertFalse(_acceptance([bad])["passed"])
        self.assertFalse(_acceptance([])["passed"])
        self.assertTrue(_acceptance([phase(), phase()])["passed"])

    def test_requested_but_incomplete_telemetry_fails_acceptance(self) -> None:
        incomplete = phase()
        incomplete["telemetry"] = {"enabled": True, "valid": False}
        self.assertFalse(_acceptance([incomplete])["passed"])

    def test_explicit_empty_seed_month_is_rejected(self) -> None:
        args = argparse.Namespace(evaluation_count=1, same_month="2026-07", seed_month="2026-06", phase=None)
        with self.assertRaisesRegex(ValueError, "populated month"):
            _build_phase_plans((), 6_000_000, Counter({"2026-07": 6_000_000}), args)

    def test_quiescing_includes_case_manager_worker(self) -> None:
        self.assertIn("case-manager-worker", QUIESCED_SERVICES)

    def test_partial_report_preserves_completed_phases_and_omits_raw_error(self) -> None:
        with TemporaryDirectory() as directory:
            output = Path(directory)
            with self.assertRaises(RuntimeError):
                with RunReport(output) as report:
                    report.start_phase("first", "first phase")
                    report.finish_phase({"phase": "first", "evaluation": {"completed": 1}})
                    report.start_phase("second", "second phase")
                    report.stage("evaluating")
                    report.update_phase(evaluation={"completed": 3})
                    raise RuntimeError("secret response body")
            raw = (output / "summary.json").read_text()
            summary = json.loads(raw)
            self.assertEqual(summary["status"], "failed")
            self.assertFalse(summary["acceptance"]["passed"])
            self.assertEqual(len(summary["phases"]), 1)
            self.assertEqual(summary["current_phase"]["evaluation"]["completed"], 3)
            self.assertNotIn("secret response body", raw)
            self.assertEqual(json.loads((output / "second.json").read_text())["status"], "failed")

    def test_cancelled_report_is_interrupted(self) -> None:
        with TemporaryDirectory() as directory:
            output = Path(directory)
            with self.assertRaises(asyncio.CancelledError):
                with RunReport(output):
                    raise asyncio.CancelledError()
            self.assertEqual(json.loads((output / "summary.json").read_text())["status"], "interrupted")

    def test_failure_locations_are_saved_without_exception_message_or_full_paths(self):
        from production_replay.database_scale_suite import _validate_month
        with TemporaryDirectory() as directory:
            output = Path(directory)
            with self.assertRaises(ValueError), RunReport(output):
                _validate_month("INVALID_PRIVATE_VALUE", "SECRET_FLAG")
            raw = (output / "summary.json").read_text()
            self.assertNotIn("SECRET_FLAG", raw)
            self.assertNotIn("INVALID_PRIVATE_VALUE", raw)
            self.assertNotIn(str(Path(__file__).resolve().parent), raw)
            error = json.loads(raw)["error"]
            self.assertEqual(error["type"], "ValueError")
            self.assertEqual(error["frames"][-1]["function"], "_validate_month")
            self.assertEqual(error["frames"][-1]["module"], "database_scale_suite.py")

    def test_atomic_write_retains_previous_report_on_serialization_failure(self) -> None:
        with TemporaryDirectory() as directory:
            output = Path(directory) / "summary.json"
            write_json_atomic(output, {"previous": True})
            with self.assertRaises(ValueError):
                write_json_atomic(output, {"bad": float("nan")})
            self.assertEqual(json.loads(output.read_text()), {"previous": True})
            self.assertEqual(len(list(output.parent.iterdir())), 1)

    def test_nonfinite_deadlines_are_rejected_before_filesystem_checks(self) -> None:
        args = build_parser().parse_args([
            "--manifest", "missing", "--database-name", "test_db", "--allow-drop-database", "test_db",
            "--pg-host", "test", "--pg-user", "test", "--pg-sslrootcert", "missing",
            "--pii-key-file", "missing", "--ingestion-concurrency", "1", "--evaluation-concurrency", "1",
            "--pipeline-timeout", "nan",
        ])
        with self.assertRaisesRegex(ValueError, "finite and positive"):
            _validate_args(args)


class EntryPointReportTests(unittest.IsolatedAsyncioTestCase):
    async def test_phase_integrates_completion_and_preserves_audit_failure_evidence(self) -> None:
        args = argparse.Namespace(
            data_model_url="http://data", ingestion_url="http://ingestion", decision_engine_url="http://decision",
            auth_token=None, request_timeout=1, publication_timeout=1, evaluation_count=2,
            ingestion_concurrency=1, evaluation_concurrency=1, segment_size=1, pipeline_timeout=30,
            deferred_policy="reject", decision_completion_timeout=1, decision_poll_interval=0.01,
            seed_batch_size=2, seed_concurrency=1, capture_metrics=False,
        )
        plan = PhasePlan("test", 0, lambda: iter([]), lambda: iter([event(0), event(1)]), "smoke")
        for count in (2, 0):
            with self.subTest(audit_count=count), TemporaryDirectory() as directory:
                output = Path(directory)
                database = MagicMock()
                database.stats.return_value = {"database_bytes": 1000, "estimated_user_rows": 2}
                database.deployment.return_value = {"status": "ok"}
                database.reconcile = AsyncMock(return_value={"valid": count == 2,
                    "persisted_totals": {"ingestion_audit": count, "outbox_events": count},
                    "record_cardinality": {}})
                setup = MagicMock()
                setup.run = AsyncMock(return_value={"tenant_id": "tenant-1", "scenarios": {"one": {}}})
                with patch("production_replay.database_scale_suite.ServiceClients", return_value=FakeClients()), \
                     patch("production_replay.database_scale_suite.EnvironmentSetup", return_value=setup):
                    if count:
                        with RunReport(output) as report:
                            value = await _run_phase(plan, args, MagicMock(), database, report)
                            report.finish_phase(value)
                        self.assertEqual(value["evaluation"]["decision"]["successes"], 2)
                    else:
                        with self.assertRaisesRegex(ValueError, "reconciliation failed"):
                            with RunReport(output) as report:
                                await _run_phase(plan, args, MagicMock(), database, report)
                        value = json.loads((output / "test.json").read_text())
                        self.assertFalse(value["database"]["per_record_entries"]["verified"])
                        self.assertEqual(value["evaluation"]["decision"]["successes"], 2)
                database.recreate.assert_called_once()
                database.migrate_and_start.assert_called_once()

    async def test_preprocessing_failure_is_saved_before_any_database_reset(self) -> None:
        with TemporaryDirectory() as directory:
            args = argparse.Namespace(output_root=directory)
            with patch("production_replay.database_scale_suite.build_parser") as parser, \
                 patch("production_replay.database_scale_suite._validate_args"), \
                 patch("production_replay.database_scale_suite._execute_suite", side_effect=ValueError("bad source")):
                parser.return_value.parse_args.return_value = args
                with self.assertRaises(ValueError):
                    await async_main([])
            reports = list(Path(directory).glob("*/summary.json"))
            self.assertEqual(len(reports), 1)
            self.assertEqual(json.loads(reports[0].read_text())["status"], "failed")
