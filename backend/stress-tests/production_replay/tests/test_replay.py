from __future__ import annotations

import asyncio
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any

from production_replay.api_client import APIError
from production_replay.domain import TransactionEvent
from production_replay.replay import ReplayCursor, ReplayMetrics, TransactionChain, schedule_events


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

            async def decide_once(self, *_args: Any, **_kwargs: Any) -> dict[str, Any]:
                self.calls.append("decision")
                return {}

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

            async def decide_once(self, *_args: Any, **_kwargs: Any) -> dict[str, Any]:
                self.decision_calls += 1
                raise APIError("decision unavailable", status_code=503)

        clients = FakeClients()
        metrics = ReplayMetrics()
        chain = TransactionChain(clients, "tenant", metrics, 1)  # type: ignore[arg-type]
        await chain(event("tx", datetime.now(timezone.utc) + timedelta(seconds=1)), 0.0)
        self.assertEqual(clients.decision_calls, 1)
        self.assertEqual(metrics.decision_failures, 1)
        self.assertEqual(metrics.summary(multiplier=1, source_start=None, source_end=None)["status"], "completed_with_errors")

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


if __name__ == "__main__":
    unittest.main()
