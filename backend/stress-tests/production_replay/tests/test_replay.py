from __future__ import annotations

import asyncio
import json
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any

from production_replay.api_client import APIError
from production_replay.domain import TransactionEvent
from production_replay.replay import ReplayCursor, ReplayMetrics, TransactionChain, build_error_breakdown, schedule_events


def event(name: str, at: datetime, sequence: int | None = None) -> TransactionEvent:
    return TransactionEvent(at, "stream", name, {"object_id": name, "transaction_id": name}, Path("source.csv"), 2, sequence)


class ReplayTests(unittest.IsolatedAsyncioTestCase):
    async def test_equal_timestamp_events_are_launched_together(self) -> None:
        at = datetime(2026, 1, 1, tzinfo=timezone.utc)
        gate = asyncio.Event()
        started: list[str] = []

        async def processor(item: TransactionEvent, _lag: float) -> None:
            started.append(item.object_id)
            if len(started) == 2:
                gate.set()
            await asyncio.wait_for(gate.wait(), timeout=1)

        await schedule_events(
            [event("first", at), event("second", at)],
            multiplier=1,
            max_in_flight=2,
            processor=processor,
        )
        self.assertCountEqual(started, ["first", "second"])

    async def test_chain_ingests_before_deciding_and_records_retry_count(self) -> None:
        class FakeClients:
            def __init__(self) -> None:
                self.calls: list[str] = []

            async def ingest_one(self, *_args: Any, **_kwargs: Any) -> tuple[dict[str, Any], int]:
                self.calls.append("ingest")
                return {}, 2

            async def record_ingested(self, *_args: Any, **kwargs: Any) -> tuple[dict[str, Any], int, dict[str, Any]]:
                self.calls.append("decision")
                return {}, 200, {"mode": kwargs["mode"]}

        clients = FakeClients()
        metrics = ReplayMetrics()
        chain = TransactionChain(clients, "tenant", metrics, 1)  # type: ignore[arg-type]
        await chain(event("tx", datetime.now(timezone.utc)), 5.0)
        self.assertEqual(clients.calls, ["ingest", "decision"])
        self.assertEqual(metrics.ingestion_retries, 1)
        self.assertEqual(metrics.decision_successes, 1)
        self.assertEqual(metrics.completed, 1)

    async def test_decision_failure_is_not_retried(self) -> None:
        class FakeClients:
            def __init__(self) -> None:
                self.decision_calls = 0

            async def ingest_one(self, *_args: Any, **_kwargs: Any) -> tuple[dict[str, Any], int]:
                return {}, 1

            async def record_ingested(self, *_args: Any, **_kwargs: Any) -> tuple[dict[str, Any], int, dict[str, Any]]:
                self.decision_calls += 1
                raise APIError("decision unavailable", status_code=503)

        clients = FakeClients()
        metrics = ReplayMetrics()
        chain = TransactionChain(clients, "tenant", metrics, 1)  # type: ignore[arg-type]
        await chain(event("tx", datetime.now(timezone.utc) + timedelta(seconds=1)), 0.0)
        self.assertEqual(clients.decision_calls, 1)
        self.assertEqual(metrics.decision_failures, 1)
        self.assertEqual(metrics.summary(multiplier=1, source_start=None, source_end=None)["status"], "completed_with_errors")

    async def test_async_decision_mode_uses_record_ingested_request_mode(self) -> None:
        class FakeClients:
            def __init__(self) -> None:
                self.calls: list[str] = []
                self.async_wait_timeout_ms = -1
                self.async_callback_url = ""
                self.mode = ""

            async def ingest_one(self, *_args: Any, **_kwargs: Any) -> tuple[dict[str, Any], int]:
                self.calls.append("ingest")
                return {}, 1

            async def record_ingested(self, *_args: Any, **kwargs: Any) -> tuple[dict[str, Any], int, dict[str, Any]]:
                self.calls.append("async_decision")
                self.async_wait_timeout_ms = int(kwargs["wait_timeout_ms"])
                self.async_callback_url = str(kwargs["callback_url"])
                self.mode = str(kwargs["mode"])
                body = {
                    "object_id": _args[1],
                    "object_type": "transactions",
                    "mode": kwargs["mode"],
                    "fields": _args[2],
                    "wait_timeout_ms": kwargs["wait_timeout_ms"],
                    "callback_url": kwargs["callback_url"],
                    "source": kwargs["source"],
                }
                return {"deferred": True, "async_decision_execution": {"status": "queued"}}, 202, body

        clients = FakeClients()
        metrics = ReplayMetrics()
        chain = TransactionChain(
            clients,
            "tenant",
            metrics,
            1,
            decision_mode="async",
            async_wait_timeout_ms=25,
            async_callback_url="https://callbacks.example/async",
        )  # type: ignore[arg-type]
        await chain(event("tx", datetime.now(timezone.utc)), 0.0)
        self.assertEqual(clients.calls, ["ingest", "async_decision"])
        self.assertEqual(clients.async_wait_timeout_ms, 25)
        self.assertEqual(clients.async_callback_url, "https://callbacks.example/async")
        self.assertEqual(clients.mode, "async")
        self.assertEqual(metrics.decision_successes, 1)
        self.assertEqual(metrics.completed, 1)

    async def test_chain_writes_successful_sync_requests_and_responses(self) -> None:
        ingestion_response = {"result": {"action": "created", "object_id": "tx"}}
        decision_response = {"decisions": [{"outcome": "allow"}]}

        class FakeClients:
            async def ingest_one(self, *_args: Any, **_kwargs: Any) -> tuple[dict[str, Any], int]:
                return ingestion_response, 2

            async def record_ingested(self, *_args: Any, **kwargs: Any) -> tuple[dict[str, Any], int, dict[str, Any]]:
                return decision_response, 200, {
                    "object_id": _args[1],
                    "object_type": "transactions",
                    "mode": kwargs["mode"],
                    "fields": _args[2],
                    "wait_timeout_ms": kwargs["wait_timeout_ms"],
                    "callback_url": kwargs["callback_url"],
                    "source": kwargs["source"],
                }

        with tempfile.TemporaryDirectory() as temp_dir:
            success_log = Path(temp_dir) / "successes.ndjson"
            chain = TransactionChain(  # type: ignore[arg-type]
                FakeClients(),
                "tenant",
                ReplayMetrics(),
                1,
                success_log_path=success_log,
            )
            await chain(event("tx", datetime.now(timezone.utc)), 0.0)
            await chain.flush_success_log()

            records = [json.loads(line) for line in success_log.read_text(encoding="utf-8").splitlines()]

        self.assertEqual([record["stage"] for record in records], ["ingestion", "decision"])
        ingestion, decision = records
        self.assertEqual(ingestion["attempts"], 2)
        self.assertEqual(ingestion["request"]["method"], "POST")
        self.assertEqual(ingestion["request"]["path"], "/v1/tenants/tenant/ingest/transactions")
        self.assertEqual(ingestion["request"]["body"]["transaction_id"], "tx")
        self.assertEqual(set(ingestion["request"]["headers"]), {"Idempotency-Key"})
        self.assertNotIn("Authorization", ingestion["request"]["headers"])
        self.assertEqual(ingestion["response"], {"status_code": 200, "body": ingestion_response})
        self.assertEqual(
            decision["request"]["path"],
            "/v1/tenants/tenant/ingestion-events/record-ingested",
        )
        self.assertEqual(decision["request"]["body"]["mode"], "sync")
        self.assertEqual(decision["request"]["body"]["wait_timeout_ms"], 0)
        self.assertEqual(decision["request"]["body"]["callback_url"], "")
        self.assertEqual(decision["request"]["body"]["source"], "production_replay")
        self.assertEqual(decision["response"], {"status_code": 200, "body": decision_response})
        self.assertIn("request_started_at", decision)
        self.assertIn("response_received_at", decision)
        self.assertGreaterEqual(decision["latency_ms"], 0)

    async def test_chain_writes_async_submission_request_and_response(self) -> None:
        async_response = {
            "async_decision_execution": {"id": "execution-1", "status": "queued"},
            "deferred": True,
        }

        class FakeClients:
            async def ingest_one(self, *_args: Any, **_kwargs: Any) -> tuple[dict[str, Any], int]:
                return {}, 1

            async def record_ingested(self, *_args: Any, **kwargs: Any) -> tuple[dict[str, Any], int, dict[str, Any]]:
                return async_response, 202, {
                    "object_id": _args[1],
                    "object_type": "transactions",
                    "mode": kwargs["mode"],
                    "fields": _args[2],
                    "wait_timeout_ms": kwargs["wait_timeout_ms"],
                    "callback_url": kwargs["callback_url"],
                    "source": kwargs["source"],
                }

        with tempfile.TemporaryDirectory() as temp_dir:
            success_log = Path(temp_dir) / "successes.ndjson"
            chain = TransactionChain(  # type: ignore[arg-type]
                FakeClients(),
                "tenant",
                ReplayMetrics(),
                1,
                decision_mode="async",
                async_wait_timeout_ms=25,
                async_callback_url="https://callbacks.example/async",
                success_log_path=success_log,
            )
            await chain(event("tx", datetime.now(timezone.utc)), 0.0)
            await chain.flush_success_log()
            records = [json.loads(line) for line in success_log.read_text(encoding="utf-8").splitlines()]

        decision = records[1]
        self.assertEqual(decision["stage"], "decision")
        self.assertEqual(decision["request"]["path"], "/v1/tenants/tenant/ingestion-events/record-ingested")
        self.assertEqual(decision["request"]["body"]["mode"], "async")
        self.assertEqual(decision["request"]["body"]["wait_timeout_ms"], 25)
        self.assertEqual(decision["request"]["body"]["callback_url"], "https://callbacks.example/async")
        self.assertEqual(decision["request"]["body"]["source"], "production_replay")
        self.assertEqual(decision["response"], {"status_code": 202, "body": async_response})

    async def test_resume_cursor_starts_after_a_drained_checkpoint(self) -> None:
        at = datetime(2026, 1, 1, tzinfo=timezone.utc)
        events = [event("first", at, 0), event("second", at + timedelta(seconds=1), 1), event("third", at + timedelta(seconds=2), 2)]
        checkpoints: list[ReplayCursor] = []

        async def checkpoint(cursor: ReplayCursor, _start: datetime, _end: datetime) -> None:
            checkpoints.append(cursor)

        async def ignore(_item: TransactionEvent, _lag: float) -> None:
            return None

        await schedule_events(
            events[:2], multiplier=1_000, max_in_flight=2, processor=ignore,
            checkpoint_every=2, checkpoint=checkpoint,
        )
        replayed: list[str] = []

        async def capture(item: TransactionEvent, _lag: float) -> None:
            replayed.append(item.object_id)

        await schedule_events(
            events, multiplier=1_000, max_in_flight=2, processor=capture,
            resume_after=checkpoints[-1],
        )
        self.assertEqual(replayed, ["third"])

    def test_metrics_checkpoint_state_is_bounded_and_round_trips(self) -> None:
        metrics = ReplayMetrics(completed=2)
        metrics.ingestion_latencies_ms.add(10.2)
        metrics.ingestion_latencies_ms.add(20.4)
        restored = ReplayMetrics.from_state(metrics.to_state())
        self.assertEqual(restored.completed, 2)
        self.assertEqual(restored.ingestion_latencies_ms.summary()["count"], 2)
        self.assertEqual(restored.ingestion_latencies_ms.summary()["avg_ms"], 15.3)

    def test_error_breakdown_separates_ingestion_and_callback_failures(self) -> None:
        breakdown = build_error_breakdown(
            [
                {
                    "stage": "decision",
                    "error": (
                        'POST http://127.0.0.1:8082/v1/tenants/t1/ingestion-events/record-ingested returned 400, '
                        'expected [200]: {"error":"record_ingested_processing_failed","details":"context deadline exceeded"}'
                    ),
                },
                {
                    "stage": "ingestion",
                    "error": (
                        'POST http://127.0.0.1:8081/v1/tenants/t1/records/transactions returned 400, '
                        'expected [200]: {"error":"validation_failed","details":"required field missing"}'
                    ),
                },
            ]
        )
        self.assertEqual(breakdown["by_stage"], {"decision": 1, "ingestion": 1})
        self.assertEqual(breakdown["by_route_hint"]["decision_callback"], 1)
        self.assertEqual(breakdown["by_route_hint"]["record_write"], 1)
        self.assertEqual(breakdown["by_classification"]["decision.record_ingested_processing_failed.timeout"], 1)
        self.assertEqual(breakdown["by_classification"]["ingestion.validation"], 1)
        self.assertEqual(breakdown["by_status_code"], {"400": 2})

    async def test_chain_writes_full_error_log(self) -> None:
        response_body = {"error": "overloaded", "details": "x" * 5_000}

        class FakeClients:
            async def ingest_one(self, *_args: Any, **_kwargs: Any) -> tuple[dict[str, Any], int]:
                raise APIError(
                    "POST http://127.0.0.1:8081/v1/tenants/t1/records/transactions returned 429, expected [200]",
                    status_code=429,
                    response_body=response_body,
                )

        metrics = ReplayMetrics()
        error_log = Path(self.id().replace("/", "_") + ".errors.ndjson")
        try:
            chain = TransactionChain(  # type: ignore[arg-type]
                FakeClients(),
                "tenant",
                metrics,
                1,
                error_log_path=error_log,
            )
            await chain(event("tx", datetime.now(timezone.utc)), 0.0)
            lines = error_log.read_text(encoding="utf-8").splitlines()
            self.assertEqual(len(lines), 1)
            record = json.loads(lines[0])
            self.assertEqual(record["response"], {"status_code": 429, "body": response_body})
            self.assertEqual(metrics.ingestion_failures, 1)
            self.assertEqual(len(metrics.errors), 1)
        finally:
            if error_log.exists():
                error_log.unlink()


if __name__ == "__main__":
    unittest.main()
