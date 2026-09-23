from __future__ import annotations

import csv
import json
import unittest
from datetime import datetime, timezone
from pathlib import Path
from tempfile import TemporaryDirectory
from typing import Any

from production_replay.api_client import APIError
from production_replay.database_scale_suite import _rebase_manifest, _run_fixed_pipeline
from production_replay.domain import TransactionEvent
from production_replay.manifest import load_manifest
from production_replay.privacy import InternalPrivacyTransformer
from production_replay.sanitize_scale_sources import OUTPUT_COLUMNS, _sanitize_file
from production_replay.scenarios import SCENARIO_SET_INTERNAL, build_portable_scenarios
from production_replay.tests.helpers import manifest_data, stream, write_minimal_sources, write_transactions


def event(number: int) -> TransactionEvent:
    fields = {
        "object_id": f"object-{number}",
        "transaction_id": f"object-{number}",
        "date": "2026-01-01T00:00:00Z",
        "amount": 10.0,
        "channel": "wallet",
        "direction": "incoming",
        "system_type": "wallet_transfer",
        "stream_id": "stream-1",
        "merchant_id": "merchant-1",
        "account_ref": "233200000001",
        "source_account_no": "233200000001",
        "terminal_id": "terminal-1",
        "account_name": "Sensitive Name",
        "payment_msisdn": "233200000001",
        "narration": "Sensitive narration",
        "raw_account_ref": "233200000001",
        "raw_account_name": "Sensitive Name",
    }
    return TransactionEvent(
        datetime(2026, 1, 1, tzinfo=timezone.utc),
        "stream-1",
        f"object-{number}",
        fields,
        Path("source.csv"),
        number + 2,
    )


class DatabaseScaleSuiteTests(unittest.IsolatedAsyncioTestCase):
    def test_manifest_paths_can_be_rebased_for_the_production_host(self) -> None:
        with TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "source"
            source.mkdir()
            write_minimal_sources(source)
            write_transactions(source / "transactions.csv", [])
            manifest_path = source / "manifest.json"
            manifest_path.write_text(
                json.dumps(manifest_data([stream("stream-1", str(source / "transactions.csv"))])),
                encoding="utf-8",
            )
            target = root / "production-data"
            rebased = _rebase_manifest(load_manifest(manifest_path), str(target))

        self.assertEqual(
            rebased.transaction_streams[0].globs,
            (str(target.resolve() / "transactions.csv"),),
        )

    def test_internal_privacy_drops_unused_pii_and_tokenizes_account(self) -> None:
        transformer = InternalPrivacyTransformer(b"0123456789abcdef0123456789abcdef")
        first = transformer.transform_event(event(1))
        second = transformer.transform_event(event(2))

        self.assertEqual(first.fields["account_ref"], second.fields["account_ref"])
        self.assertTrue(first.fields["account_ref"].startswith("hmac256:"))
        self.assertNotIn("source_account_no", first.fields)
        self.assertNotIn("terminal_id", first.fields)
        self.assertNotIn("account_name", first.fields)
        self.assertNotIn("payment_msisdn", first.fields)
        self.assertNotIn("narration", first.fields)

    def test_csv_sanitizer_preserves_row_and_emits_only_minimal_tokenized_columns(self) -> None:
        with TemporaryDirectory() as directory:
            root = Path(directory)
            source_root = root / "source"
            source = source_root / "transactions" / "stream" / "inflow" / "2026-07-01.csv"
            source.parent.mkdir(parents=True)
            with source.open("w", encoding="utf-8", newline="") as handle:
                writer = csv.DictWriter(
                    handle,
                    fieldnames=[
                        "transtype",
                        "source_date_created",
                        "source_account_no",
                        "source_trans_id",
                        "thirdparty_id",
                        "terminal_id",
                        "merchant_id",
                        "product_id",
                        "accountname",
                        "paymentmsisdn",
                        "narration",
                        "amount",
                        "currency",
                    ],
                )
                writer.writeheader()
                writer.writerow(
                    {
                        "transtype": "inflow",
                        "source_date_created": "2026-07-01 00:00:00",
                        "source_account_no": "233200000001",
                        "source_trans_id": "transaction-1",
                        "thirdparty_id": "third-party-1",
                        "terminal_id": "terminal-1",
                        "merchant_id": "merchant-1",
                        "product_id": "product-1",
                        "accountname": "Sensitive Name",
                        "paymentmsisdn": "233200000001",
                        "narration": "Sensitive narration",
                        "amount": "10.00",
                        "currency": "GHS",
                    }
                )
            destination = root / "output" / source.relative_to(source_root)
            result = _sanitize_file(
                str(source),
                str(destination),
                str(source_root),
                b"0123456789abcdef0123456789abcdef",
            )
            with destination.open("r", encoding="utf-8", newline="") as handle:
                rows = list(csv.DictReader(handle))
            sanitized_content = destination.read_text(encoding="utf-8")

        self.assertEqual(result.rows, 1)
        self.assertEqual(tuple(rows[0]), OUTPUT_COLUMNS)
        self.assertTrue(rows[0]["source_account_no"].startswith("hmac256:"))
        self.assertEqual(rows[0]["source_trans_id"], "")
        self.assertNotIn("Sensitive Name", sanitized_content)

    def test_internal_scenario_set_contains_four_scenarios_and_six_rules(self) -> None:
        definitions = build_portable_scenarios(object(), SCENARIO_SET_INTERNAL)  # type: ignore[arg-type]
        self.assertEqual(len(definitions), 4)
        self.assertEqual(sum(len(item.rules) for item in definitions), 6)

    async def test_fixed_pipeline_replaces_failed_ingest_and_submits_exact_target(self) -> None:
        class FakeClients:
            def __init__(self) -> None:
                self.ingestion_calls = 0
                self.decisions: list[str] = []

            async def ingest_one(self, *_args: Any, **_kwargs: Any) -> tuple[dict[str, Any], int]:
                self.ingestion_calls += 1
                if self.ingestion_calls == 1:
                    raise APIError("failed", status_code=400)
                return {}, 1

            async def record_ingested(
                self, _tenant_id: str, object_id: str, *_args: Any, **_kwargs: Any
            ) -> tuple[dict[str, Any], int, dict[str, Any]]:
                self.decisions.append(object_id)
                return {}, 200, {}

        clients = FakeClients()
        result = await _run_fixed_pipeline(
            clients,  # type: ignore[arg-type]
            "tenant-1",
            [event(index) for index in range(5)],
            target=3,
            ingestion_concurrency=2,
            evaluation_concurrency=2,
            segment_size=2,
        )

        self.assertEqual(clients.ingestion_calls, 4)
        self.assertEqual(len(clients.decisions), 3)
        self.assertEqual(result["decision"]["attempts"], 3)
        self.assertEqual(result["ingestion"]["failures"], 1)


if __name__ == "__main__":
    unittest.main()
