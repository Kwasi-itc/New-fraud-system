from __future__ import annotations

import asyncio
import json
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from production_replay.api_client import APIError
from production_replay.identity_ledger import IdentityLedger, LedgerInvariantError
from system_tests.queue_campaign import Stage, async_main, parse_stage, read_inputs, run_campaign


class Clients:
    def __init__(self, *, delay=0, fail_ingest=False, wrong_tenant=False, sync=False):
        self.delay = delay
        self.fail_ingest = fail_ingest
        self.wrong_tenant = wrong_tenant
        self.sync = sync
        self.calls = 0
        self.active = 0

    async def ingest_one(self, tenant, table, fields, key, **options):
        self.calls += 1
        assert options["max_attempts"] == 1
        if self.fail_ingest:
            raise APIError("sensitive response", status_code=503)
        return {}, 1

    async def record_ingested(self, tenant, identity, fields, **options):
        assert options["mode"] == "async"
        if self.sync:
            return {"result": {}}, 200, {}
        return {"async_decision_execution": {
            "id": identity, "tenant_id": tenant, "object_type": "transactions", "status": "queued",
        }}, 202, {}

    async def get_async_decision_execution(self, tenant, identity):
        self.active += 1
        try:
            await asyncio.sleep(self.delay)
            return {"async_decision_execution": {
                "id": identity, "tenant_id": "wrong" if self.wrong_tenant else tenant,
                "object_type": "transactions", "status": "completed",
                "result_body": {"object_id": identity, "results": [{"triggered": False}]},
            }}
        finally:
            self.active -= 1


class CampaignTests(unittest.IsolatedAsyncioTestCase):
    async def run_case(self, clients=None, **overrides):
        options = dict(stages=[Stage("steady", 40, 0.1)], max_inflight=10,
                       completion_timeout=1, poll_interval=0.001, expected_scenarios=1,
                       deadline=1, max_lag=0.5)
        options.update(overrides)
        with TemporaryDirectory() as directory:
            root = Path(directory)
            with IdentityLedger(root / "identities.sqlite3") as ledger:
                result = await run_campaign(clients or Clients(), "tenant",
                    iter({"object_id": str(i)} for i in range(100)), output=root, ledger=ledger, **options)
            saved = json.loads((root / "summary.json").read_text())
            self.assertEqual(saved, result)
            self.assertNotIn("sensitive response", (root / "summary.json").read_text())
            return result

    async def test_async_completion_and_cohort_reconciliation(self):
        result = await self.run_case(stages=[Stage("baseline", 20, 0.1), Stage("burst", 40, 0.1)])
        self.assertTrue(result["acceptance"]["passed"])
        self.assertEqual(result["counts"]["completed"], 6)
        self.assertEqual(result["counts"]["async_accepted"], 6)
        self.assertEqual(result["counts"]["outstanding"], 0)
        self.assertEqual(result["cohorts"]["burst"]["counts"]["completed"], 4)

    async def test_saturated_generator_drops_without_extending_arrival_schedule(self):
        result = await self.run_case(Clients(delay=0.15), max_inflight=1)
        self.assertEqual(result["counts"]["offered"], 4)
        self.assertEqual(result["counts"]["submitted"], 1)
        self.assertEqual(result["counts"]["generator_dropped"], 3)
        self.assertEqual(result["peak_inflight"], 1)
        self.assertFalse(result["acceptance"]["passed"])

    async def test_late_success_is_not_deadline_success(self):
        result = await self.run_case(Clients(delay=0.03), deadline=0.001)
        self.assertEqual(result["counts"]["completed"], 4)
        self.assertEqual(result["counts"]["deadline_met"], 0)
        self.assertFalse(result["acceptance"]["passed"])

    async def test_timeout_cancels_polling_and_keeps_unresolved_accounting(self):
        clients = Clients(delay=1)
        result = await self.run_case(clients, completion_timeout=0.02)
        self.assertEqual(result["counts"]["unresolved"], 4)
        self.assertEqual(result["counts"]["outstanding"], 0)
        self.assertEqual(clients.active, 0)
        self.assertFalse(result["identity_reconciliation"]["valid"])

    async def test_ingestion_error_is_not_retried_or_reported_as_definite_rejection(self):
        clients = Clients(fail_ingest=True)
        result = await self.run_case(clients)
        self.assertEqual(clients.calls, 4)
        self.assertEqual(result["counts"]["unresolved"], 4)
        self.assertEqual(result["counts"]["async_accepted"], 0)

    async def test_wrong_tenant_and_unexercised_queue_cannot_pass(self):
        for clients in (Clients(wrong_tenant=True), Clients(sync=True)):
            with self.subTest(clients=clients):
                result = await self.run_case(clients)
                self.assertEqual(result["counts"]["completed"], 0)
                self.assertFalse(result["acceptance"]["passed"])

    async def test_duplicate_identity_fails_and_preserves_partial_report(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            with IdentityLedger(root / "ids.sqlite3") as ledger:
                with self.assertRaises(LedgerInvariantError):
                    await run_campaign(Clients(), "tenant", iter([{"object_id": "same"}] * 2),
                        [Stage("steady", 20, 0.1)], root, max_inflight=10,
                        completion_timeout=1, poll_interval=0.001, expected_scenarios=1,
                        deadline=1, max_lag=0.5, ledger=ledger)
            report = json.loads((root / "summary.json").read_text())
            self.assertEqual(report["status"], "interrupted_or_failed")
            self.assertFalse(report["acceptance"]["passed"])

    async def test_dry_run_validates_source_without_artifacts(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "input.ndjson"
            source.write_text('{"object_id":"a"}\n')
            output = root / "run"
            code = await async_main(["--input", str(source), "--output", str(output),
                "--tenant-id", "tenant", "--stage", "smoke:1:1", "--expected-scenarios", "1",
                "--deadline", "1", "--dry-run"])
            self.assertEqual(code, 0)
            self.assertFalse(output.exists())

    def test_input_rejects_missing_identity_and_oversized_rows(self):
        with TemporaryDirectory() as directory:
            path = Path(directory) / "input.ndjson"
            for content in ('{}\n', ' ' * 1_048_577):
                path.write_text(content)
                with self.assertRaises(ValueError):
                    list(read_inputs(path))

    def test_stage_rejects_nonfinite_or_nonpositive_values(self):
        import argparse
        for value in ("x:nan:1", "x:1:inf", "x:0:1", "../x:1:1"):
            with self.assertRaises(argparse.ArgumentTypeError):
                parse_stage(value)


if __name__ == "__main__":
    unittest.main()
