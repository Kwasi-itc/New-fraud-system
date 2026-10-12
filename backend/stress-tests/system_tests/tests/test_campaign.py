from __future__ import annotations

import json
import asyncio
import unittest
from pathlib import Path
from types import SimpleNamespace
from tempfile import TemporaryDirectory
from unittest.mock import AsyncMock, patch

from production_replay.identity_ledger import LedgerInvariantError
from system_tests.campaign import execute, load_campaign, preflight
from system_tests.input_source import InputSource, inspect_source
from system_tests.tests import test_queue_campaign as fixtures


class PathTests(unittest.IsolatedAsyncioTestCase):
    run_case = fixtures.CampaignTests.run_case
    # These tests use the same real runner as the async-path cases.
    async def test_ingestion_path_does_not_request_decisions(self):
        clients = fixtures.Clients()
        clients.record_ingested = AsyncMock(side_effect=AssertionError("must not run"))
        result = await self.run_case(clients, path="ingestion")
        self.assertTrue(result["acceptance"]["passed"])
        self.assertEqual(result["counts"]["completed"], 4)
        self.assertEqual(result["counts"]["async_accepted"], 0)

    async def test_sync_path_rejects_async_fallback(self):
        class SyncClients(fixtures.Clients):
            async def record_ingested(self, tenant, identity, fields, **options):
                assert options["mode"] == "sync"
                return {"result": {"object_id": identity, "results": [{"triggered": False}]}}, 200, {}
        result = await self.run_case(SyncClients(), path="ingest-sync")
        self.assertTrue(result["acceptance"]["passed"])
        client = SyncClients()
        client.record_ingested = AsyncMock(return_value=({"deferred": True}, 202, {}))
        result = await self.run_case(client, path="ingest-sync")
        self.assertFalse(result["acceptance"]["passed"])

    async def test_disk_reserve_refuses_traffic(self):
        client = fixtures.Clients()
        with patch("system_tests.queue_campaign.shutil.disk_usage", return_value=SimpleNamespace(free=0)):
            with self.assertRaisesRegex(RuntimeError, "disk_reserve"):
                await self.run_case(client, minimum_free_disk_bytes=1)
        self.assertEqual(client.calls, 0)

    async def test_worker_failure_interrupts_long_arrival_wait(self):
        client = fixtures.Clients()
        client.ingest_one = AsyncMock(side_effect=RuntimeError("fixture bug"))
        from system_tests.queue_campaign import Stage
        with self.assertRaises(RuntimeError):
            await asyncio.wait_for(self.run_case(client, stages=[Stage("slow", 0.01, 200)]), timeout=2)


class InputTests(unittest.TestCase):
    def test_preflight_duplicate_and_malformed_records(self):
        with TemporaryDirectory() as directory:
            source = Path(directory) / "input"
            for contents, error in ((('{"object_id":"a"}\n' * 2), LedgerInvariantError),
                    ('{"object_id":"a","object_id":"b"}\n{}\n', ValueError),
                    ('{"object_id":"a","amount":NaN}\n{}\n', ValueError)):
                source.write_text(contents)
                with self.assertRaises(error):
                    inspect_source(source, 2)

    def test_fingerprint_detects_changes_and_only_reads_scheduled_prefix(self):
        with TemporaryDirectory() as directory:
            source = Path(directory) / "input"
            source.write_text('{"object_id":"a","amount":1}\nnot-json\n')
            original = inspect_source(source, 1)
            source.write_text('{"object_id":"a","amount":2}\nnot-json\n')
            reader = InputSource(source, 1)
            try:
                self.assertEqual(len(list(reader)), 1)
                self.assertNotEqual(reader.evidence(), original)
            finally:
                reader.close()


class CampaignConfigTests(unittest.IsolatedAsyncioTestCase):
    def test_ambiguous_configuration_is_rejected(self):
        with TemporaryDirectory() as directory:
            path = Path(directory) / "campaign.json"
            for contents in ('{"mode":"qualification","mode":"discovery"}', '{"deadline":NaN}'):
                path.write_text(contents)
                with self.assertRaises(ValueError):
                    load_campaign(path)

    def config(self, root):
        (root / "a.ndjson").write_text('{"object_id":"a"}\n')
        (root / "b.ndjson").write_text('{"object_id":"b"}\n')
        value = {"schema_version": 1, "mode": "discovery", "defaults": {
            "tenant_id": "tenant", "stage": ["smoke:1:0.1"], "deadline": 1, "expected_scenarios": 1},
            "runs": [{"name": "one", "options": {"input": "a.ndjson"}},
                     {"name": "two", "options": {"input": "b.ndjson"}}]}
        path = root / "campaign.json"
        path.write_text(json.dumps(value))
        return path, value

    def test_unknown_keys_qualification_and_overallocated_budget_rejected(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            path, base = self.config(root)
            changes = [{"unexpected": True}, {"mode": "qualification"}, {"defaults": {"shell_command": "x"}},
                {"connection_budget": {"max_connections": 20, "reserve": 5, "pools": [
                    {"role": "workers", "replicas": 2, "max_connections": 10}]}}]
            for change in changes:
                with self.subTest(change=change):
                    path.write_text(json.dumps({**base, **change}))
                    with self.assertRaises(ValueError):
                        load_campaign(path)

    def test_duplicate_id_across_runs_rejected_but_tenants_isolated(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            path, value = self.config(root)
            (root / "b.ndjson").write_text('{"object_id":"a"}\n')
            with self.assertRaises(LedgerInvariantError):
                preflight(load_campaign(path))
            value["runs"][1]["options"]["tenant_id"] = "other"
            path.write_text(json.dumps(value))
            self.assertEqual(len(preflight(load_campaign(path))), 2)

    async def test_failure_stops_remaining_runs_and_generates_report(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            path, _ = self.config(root)
            output = root / "run"
            with patch("system_tests.campaign.run_experiment", AsyncMock(side_effect=RuntimeError("secret"))) as runner:
                result = await execute(load_campaign(path), output)
            self.assertEqual(result, 2)
            runner.assert_awaited_once()
            state = json.loads((output / "summary.json").read_text())
            self.assertEqual(state["status"], "incomplete")
            self.assertFalse(state["acceptance"]["passed"])
            self.assertNotIn("secret", (output / "summary.json").read_text())
            self.assertTrue((output / "report.html").exists())

    async def test_successful_campaign_finishes_all_runs_without_qualification_claim(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            path, _ = self.config(root)
            async def run(argv):
                destination = Path(argv[argv.index("--output") + 1])
                destination.mkdir()
                (destination / "summary.json").write_text(json.dumps({
                    "acceptance": {"evaluated": True, "passed": True}, "cohorts": {}, "path": "ingest-async"}))
                return 0
            with patch("system_tests.campaign.run_experiment", side_effect=run):
                code = await execute(load_campaign(path), root / "run")
            state = json.loads((root / "run/summary.json").read_text())
            self.assertEqual(code, 0)
            self.assertEqual(len(state["runs"]), 2)
            self.assertFalse(state["production_qualified"])


if __name__ == "__main__":
    unittest.main()
