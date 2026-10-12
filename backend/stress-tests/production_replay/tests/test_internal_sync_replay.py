from __future__ import annotations

import asyncio
from datetime import datetime, timezone
import json
from pathlib import Path
from tempfile import TemporaryDirectory
from types import SimpleNamespace
import unittest
from unittest.mock import AsyncMock, patch

from production_replay.api_client import APIError
from production_replay.decision_completion import DecisionCompletionError
from production_replay.domain import TransactionEvent
from production_replay.internal_sync_replay import (
    FixedSyncReplay, execute, parser, prepare_sources, validate_arguments, validate_decision,
    write_final_report,
)
from production_replay.local_storage import DiskSpaceGuard
from production_replay.manifest import load_manifest
from production_replay.reference_data import load_merchants, load_merchant_products
from production_replay.scenarios import SCENARIO_SET_INTERNAL
from production_replay.setup_environment import EnvironmentSetup, INTERNAL_TABLE_FIELDS
from production_replay.sorting import iter_merged_events
from production_replay.tests.helpers import manifest_data, stream, transaction_row, write_minimal_sources, write_transactions


def event(index):
    return TransactionEvent(datetime(2026, 7, 1, tzinfo=timezone.utc), "test", f"tx-{index}",
                            {"object_id": f"tx-{index}"}, Path("source.csv"), index + 2)


def arguments():
    return SimpleNamespace(transactions=6, ingestion_concurrency=2, evaluation_concurrency=3,
                           progress_every=2, min_free_disk_mib=1)


def completed(tenant, object_id, scenario="scenario-1"):
    return {"result": {"object_id": object_id, "results": [
        {"triggered": True, "decision": {"id": f"decision-{object_id}", "tenant_id": tenant,
                                          "object_id": object_id, "object_type": "transactions",
                                          "scenario_id": scenario}},
        {"triggered": False},
    ]}}


class FakeClients:
    def __init__(self):
        self.ingested = set()
        self.decisions = []
        self.keys = []
        self.active = 0

    async def ingest_one(self, tenant, object_type, fields, key):
        self.keys.append(key)
        self.active += 1
        try:
            await asyncio.sleep(0.002)
            self.ingested.add((tenant, fields["object_id"]))
            return {"result": {"object_id": fields["object_id"], "action": "created", "revision_id": "revision"}}, 1
        finally:
            self.active -= 1

    async def record_ingested(self, tenant, object_id, fields, *, mode, source):
        if (tenant, object_id) not in self.ingested:
            raise AssertionError("evaluation before ingestion, or wrong tenant")
        if mode != "sync":
            raise AssertionError("non-sync request")
        self.decisions.append(object_id)
        await asyncio.sleep(0.003)
        return completed(tenant, object_id), 200, {}


class PipelineTests(unittest.IsolatedAsyncioTestCase):
    async def test_fixed_count_independent_concurrency_and_tenant_scoping(self):
        with TemporaryDirectory() as directory:
            clients = FakeClients()
            replay = FixedSyncReplay(clients, "tenant-1", ("scenario-1", "scenario-2"), arguments(), Path(directory))
            result = await replay.run(event(index) for index in range(6))
        self.assertEqual((result["selected"], result["finished"]), (6, 6))
        self.assertEqual(result["ingestion"]["successes"], 6)
        self.assertEqual(result["decision"]["successes"], 6)
        self.assertEqual(result["decision_rows_reported"], 6)
        self.assertEqual(result["ingestion"]["max_observed_concurrency"], 2)
        self.assertLessEqual(result["decision"]["max_observed_concurrency"], 3)
        self.assertGreater(result["decision"]["successful_transactions_per_second"], 0)
        self.assertEqual(len(set(clients.keys)), 6)
        self.assertTrue(all(tenant == "tenant-1" for tenant, _ in clients.ingested))

    async def test_failed_ingestion_is_not_evaluated_or_replaced(self):
        clients = FakeClients()
        original = clients.ingest_one
        async def ingest(tenant, object_type, fields, key):
            if fields["object_id"] == "tx-2":
                raise APIError("sensitive body must not be logged", status_code=502, attempts=3)
            return await original(tenant, object_type, fields, key)
        clients.ingest_one = ingest
        with TemporaryDirectory() as directory:
            output = Path(directory)
            replay = FixedSyncReplay(clients, "tenant-1", ("scenario-1", "scenario-2"), arguments(), output)
            result = await replay.run(event(index) for index in range(6))
            errors = (output / "errors.ndjson").read_text()
        self.assertNotIn("tx-2", clients.decisions)
        self.assertEqual((result["selected"], result["finished"]), (6, 6))
        self.assertEqual(result["ingestion"]["failures"], 1)
        self.assertEqual(result["ingestion"]["retries"], 2)
        self.assertEqual(result["decision"]["successes"], 5)
        self.assertNotIn("sensitive body", errors)

    async def test_deferred_response_is_failure_and_is_not_polled_or_retried(self):
        clients = FakeClients()
        clients.record_ingested = AsyncMock(return_value=({"deferred": True}, 202, {}))
        with TemporaryDirectory() as directory:
            output = Path(directory)
            replay = FixedSyncReplay(clients, "tenant-1", ("scenario-1", "scenario-2"), arguments(), output)
            result = await replay.run(event(index) for index in range(6))
        self.assertEqual(result["decision"]["successes"], 0)
        self.assertEqual(result["decision"]["errors"], {"deferred_rejected": 6})
        self.assertEqual(clients.record_ingested.await_count, 6)

    async def test_malformed_ingestion_response_never_triggers_decision(self):
        clients = FakeClients()
        clients.ingest_one = AsyncMock(return_value=({"result": {"object_id": "other-record"}}, 1))
        with TemporaryDirectory() as directory:
            replay = FixedSyncReplay(clients, "tenant-1", ("scenario-1", "scenario-2"), arguments(), Path(directory))
            result = await replay.run(event(index) for index in range(6))
        self.assertEqual(result["ingestion"]["failures"], 6)
        self.assertEqual(clients.decisions, [])

    async def test_unhandled_failure_cancels_workers_and_closes_source(self):
        clients = FakeClients()
        async def fatal(*args, **kwargs):
            raise RuntimeError("infrastructure failure")
        clients.record_ingested = fatal
        closed = []
        def events():
            try:
                yield from (event(index) for index in range(6))
            finally:
                closed.append(True)
        with TemporaryDirectory() as directory:
            replay = FixedSyncReplay(clients, "tenant-1", ("scenario-1", "scenario-2"), arguments(), Path(directory))
            with self.assertRaisesRegex(RuntimeError, "infrastructure"):
                await asyncio.wait_for(replay.run(events()), timeout=2)
        self.assertEqual(closed, [True])
        self.assertEqual(clients.active, 0)

    async def test_completion_contract_rejects_wrong_tenant_scenario_and_missing_results(self):
        for mutate in (
            lambda body: body["result"]["results"][0]["decision"].update(tenant_id="other-tenant"),
            lambda body: body["result"]["results"][0]["decision"].update(scenario_id="other-scenario"),
            lambda body: body["result"].update(results=[]),
            lambda body: body["result"]["results"][0]["decision"].update(object_type="wallets"),
        ):
            body = completed("tenant-1", "tx-1")
            mutate(body)
            with self.assertRaises(DecisionCompletionError):
                await validate_decision(None, "tenant-1", event(1), body, 200, ("scenario-1", "scenario-2"))

    async def test_valid_non_triggering_evaluation_is_successful_without_decision_rows(self):
        body = {"result": {"object_id": "tx-1", "results": [{"triggered": False}, {"triggered": False}]}}
        self.assertEqual(await validate_decision(None, "tenant-1", event(1), body, 200,
                                                ("scenario-1", "scenario-2")), ())


class SourceTests(unittest.TestCase):
    def test_reporting_failure_is_not_silently_ignored_on_success(self):
        with patch("production_replay.internal_sync_replay.write_json", side_effect=OSError("disk full")):
            with self.assertRaises(OSError):
                write_final_report(Path("summary.json"), {})

    def test_reporting_failure_preserves_original_workload_error(self):
        with patch("production_replay.internal_sync_replay.write_json", side_effect=OSError("disk full")):
            with self.assertRaisesRegex(RuntimeError, "original failure"):
                try:
                    raise RuntimeError("original failure")
                finally:
                    write_final_report(Path("summary.json"), {})

    def make_sources(self, root):
        write_minimal_sources(root)
        (root / "key").write_bytes(b"a" * 32)
        write_transactions(root / "a.csv", [transaction_row(source_trans_id="later", source_date_created="2026-02-01 12:00:00")])
        write_transactions(root / "b.csv", [transaction_row(source_trans_id="early", source_date_created="2026-02-01 10:00:00")])
        manifest = root / "manifest.json"
        manifest.write_text(json.dumps(manifest_data([stream("a", "a.csv"), stream("b", "b.csv")])))
        return parser().parse_args(["--manifest", str(manifest), "--pii-key-file", str(root / "key"),
                                   "--transactions", "2", "--data-model-url", "http://data",
                                   "--ingestion-url", "http://ingestion", "--decision-engine-url", "http://decision"])

    def test_sources_are_sorted_minimized_and_do_not_require_staff(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            args = self.make_sources(root)
            (root / "staff.csv").unlink()
            guard = DiskSpaceGuard(root, 1)
            _, references, selected, count = prepare_sources(args, root, guard)
            rows = list(iter_merged_events([selected]))
        self.assertEqual(count, 2)
        self.assertEqual([row.stream_id for row in rows], ["b", "a"])
        self.assertEqual(len(references[0].records), 1)
        self.assertEqual(rows[0].fields["account_ref"], rows[1].fields["account_ref"])
        self.assertTrue(rows[0].fields["account_ref"].startswith("hmac256:"))
        self.assertNotIn("account_name", rows[0].fields)
        self.assertNotIn("payment_msisdn", rows[0].fields)

    def test_offset_selects_exact_count_and_short_sources_fail_before_any_api_calls(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            args = self.make_sources(root)
            args.transactions, args.transaction_offset = 1, 1
            _, _, selected, _ = prepare_sources(args, root, DiskSpaceGuard(root, 1))
            self.assertEqual([row.stream_id for row in iter_merged_events([selected])], ["a"])
            args.transactions = 2
            with self.assertRaisesRegex(ValueError, "not enough"):
                prepare_sources(args, root, DiskSpaceGuard(root, 1))

    def test_invalid_configuration_is_rejected(self):
        with TemporaryDirectory() as directory:
            args = self.make_sources(Path(directory))
            args.evaluation_concurrency = 0
            with self.assertRaises(ValueError):
                validate_arguments(args)
            args.evaluation_concurrency = 10
            args.decision_engine_url = "https://user:password@example.com"
            with self.assertRaises(ValueError):
                validate_arguments(args)

    def test_pre_sanitized_accounts_are_not_rehashed(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            args = self.make_sources(root)
            token = "hmac256:" + "a" * 64
            for name in ("a.csv", "b.csv"):
                write_transactions(root / name, [transaction_row(source_account_no=token)])
            args.pre_sanitized_source = True
            args.pii_key_file = None
            (root / "key").unlink()
            _, _, selected, _ = prepare_sources(args, root, DiskSpaceGuard(root, 1))
            self.assertTrue(all(row.fields["account_ref"] == token for row in iter_merged_events([selected])))

    def test_without_a_key_identifiers_are_unchanged_and_unused_fields_are_dropped(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            args = self.make_sources(root)
            args.pii_key_file = None
            (root / "key").unlink()
            _, _, selected, _ = prepare_sources(args, root, DiskSpaceGuard(root, 1))
            rows = list(iter_merged_events([selected]))
            self.assertTrue(all(row.fields["account_ref"] == "233200000001" for row in rows))
            self.assertTrue(all(row.object_id.startswith("production-replay:") for row in rows))
            self.assertTrue(all("account_name" not in row.fields and "narration" not in row.fields for row in rows))

    def test_parser_does_not_require_a_key(self):
        args = parser().parse_args(["--data-model-url", "http://data", "--ingestion-url", "http://ingestion",
                                   "--decision-engine-url", "http://decision"])
        self.assertIsNone(args.pii_key_file)

    def test_duplicate_selected_ids_are_rejected(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            args = self.make_sources(root)
            def duplicates(*_args):
                yield event(1)
                yield event(1)
            with patch("production_replay.internal_sync_replay.iter_merged_events", duplicates):
                with self.assertRaisesRegex(ValueError, "duplicate selected"):
                    prepare_sources(args, root, DiskSpaceGuard(root, 1))


class SetupTests(unittest.IsolatedAsyncioTestCase):
    async def test_tables_and_fields_are_created_only_through_data_model_api(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            args = SourceTests().make_sources(root)
            manifest = load_manifest(args.manifest)
            refs = (load_merchants(manifest.merchant_files()), load_merchant_products(manifest.merchant_product_files()))
            calls = []
            data_model = object()
            async def request(client, method, path, expected, **kwargs):
                self.assertIs(client, data_model)
                calls.append((method, path, kwargs.get("json")))
                if method == "GET" and path.endswith("/tables"):
                    return {"tables": []}
                if method == "POST" and path.endswith("/tables"):
                    name = kwargs["json"]["name"]
                    return {"table": {"id": name, "name": name}}
                if method == "GET" and path.endswith("/fields"):
                    return {"fields": []}
                if method == "POST" and path.endswith("/fields"):
                    return {"field": {"id": kwargs["json"]["name"], **kwargs["json"]}}
                if method == "GET" and path.endswith("/data-model"):
                    return {}
                raise AssertionError(f"unexpected request {method} {path}")
            clients = SimpleNamespace(data_model=data_model, request=request)
            setup = EnvironmentSetup(manifest, clients, "tenant-1", "test", SCENARIO_SET_INTERNAL, merchant_references=refs)
            await setup._ensure_model()
            tables = [body["name"] for method, path, body in calls if method == "POST" and path.endswith("/tables")]
            self.assertEqual(tables, ["transactions", "merchants", "merchant_products"])
            self.assertTrue(all("tenant-1" in path for method, path, _ in calls if path.endswith("/tables")))
            self.assertTrue(any(body["name"] == "merchant_product_id" for method, path, body in calls
                                if method == "POST" and path == "/v1/tables/merchant_products/fields"))

    async def test_internal_setup_adds_merchants_without_changing_default_scale_model(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            args = SourceTests().make_sources(root)
            manifest = load_manifest(args.manifest)
            refs = (load_merchants(manifest.merchant_files()), load_merchant_products(manifest.merchant_product_files()))
            clients = SimpleNamespace(wait_until_ready=AsyncMock(), ingest_batch=AsyncMock())
            clients.ingest_batch.side_effect = lambda tenant, table, batch, key: {
                "results": [{"object_id": row["object_id"], "action": "created", "revision_id": "revision"} for row in batch]}
            setup = EnvironmentSetup(manifest, clients, "tenant-1", "test", SCENARIO_SET_INTERNAL, merchant_references=refs)
            self.assertEqual(set(setup.table_fields), {"transactions", "merchants", "merchant_products"})
            default = EnvironmentSetup(manifest, clients, "tenant-1", "test", SCENARIO_SET_INTERNAL)
            self.assertEqual(default.table_fields, INTERNAL_TABLE_FIELDS)
            with patch.object(setup, "_ensure_tenant", AsyncMock()), patch.object(setup, "_ensure_no_scenario_collisions", AsyncMock()), \
                 patch.object(setup, "_ensure_model", AsyncMock()), patch.object(setup, "_create_scenarios", AsyncMock(return_value={})), \
                 patch.object(setup, "_ensure_reference_lists", AsyncMock()) as staff:
                report = await setup.run()
            self.assertEqual([call.args[1] for call in clients.ingest_batch.await_args_list], ["merchants", "merchant_products"])
            self.assertTrue(all(call.args[0] == "tenant-1" for call in clients.ingest_batch.await_args_list))
            staff.assert_not_awaited()
            self.assertEqual(report["reference_data"]["merchants"]["unique_keys"], 1)

    async def test_incomplete_reference_batch_aborts_before_scenario_publication(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            args = SourceTests().make_sources(root)
            manifest = load_manifest(args.manifest)
            refs = (load_merchants(manifest.merchant_files()), load_merchant_products(manifest.merchant_product_files()))
            clients = SimpleNamespace(wait_until_ready=AsyncMock(), ingest_batch=AsyncMock(return_value={"results": []}))
            setup = EnvironmentSetup(manifest, clients, "tenant-1", "test", SCENARIO_SET_INTERNAL, merchant_references=refs)
            with patch.object(setup, "_ensure_tenant", AsyncMock()), patch.object(setup, "_ensure_no_scenario_collisions", AsyncMock()), \
                 patch.object(setup, "_ensure_model", AsyncMock()), patch.object(setup, "_create_scenarios", AsyncMock()) as publish:
                with self.assertRaisesRegex(APIError, "invalid_reference"):
                    await setup.run()
                publish.assert_not_awaited()

    async def test_prepare_only_never_constructs_api_clients(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            args = SourceTests().make_sources(root)
            args.min_free_disk_mib = 1
            args.output_root = root / "results"
            with patch("production_replay.internal_sync_replay.ServiceClients") as clients:
                self.assertEqual(await execute(args), 0)
                clients.assert_not_called()
            summary = json.loads(next(args.output_root.glob("*/summary.json")).read_text())
            self.assertEqual(summary["status"], "prepared_only")
            self.assertFalse(summary["mutations_performed"])
            self.assertEqual(list(args.output_root.glob("*/sources-*")), [])

    async def test_partial_setup_failure_records_tenant_without_deleting_it(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            args = SourceTests().make_sources(root)
            args.min_free_disk_mib = 1
            args.output_root = root / "results"
            args.execute = True
            clients = AsyncMock()
            clients.__aenter__.return_value = clients
            setup = SimpleNamespace(tenant_id="partially-created-tenant", run=AsyncMock(side_effect=APIError("private details")))
            with patch("production_replay.internal_sync_replay.ServiceClients", return_value=clients), \
                 patch("production_replay.internal_sync_replay.EnvironmentSetup", return_value=setup):
                with self.assertRaises(APIError):
                    await execute(args)
            report_text = next(args.output_root.glob("*/summary.json")).read_text()
            report = json.loads(report_text)
            self.assertEqual(report["status"], "failed")
            self.assertEqual(report["failed_stage"], "setup")
            self.assertEqual(report["tenant_id"], "partially-created-tenant")
            self.assertNotIn("private details", report_text)
            self.assertEqual(list(args.output_root.glob("*/sources-*")), [])


if __name__ == "__main__":
    unittest.main()
