from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path
from typing import Any

from production_replay.api_client import APIError
from production_replay.cli import _verify_existing_seed
from production_replay.manifest import load_manifest
from production_replay.seed import seed_transactions
from production_replay.tests.helpers import (
    manifest_data,
    stream,
    transaction_row,
    write_minimal_sources,
    write_transactions,
)


class SeedTests(unittest.IsolatedAsyncioTestCase):
    async def test_existing_seed_verification_uses_an_indexed_object_lookup(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            write_minimal_sources(root)
            transaction_path = root / "seed-transactions.csv"
            write_transactions(transaction_path, [transaction_row(source_trans_id="seed-1")])
            manifest_path = root / "manifest.json"
            manifest_path.write_text(
                json.dumps(manifest_data([stream("seed-stream", transaction_path.name)])),
                encoding="utf-8",
            )

            class FakeClients:
                def __init__(self) -> None:
                    self.ingestion = object()
                    self.path = ""

                async def request(
                    self,
                    client: object,
                    method: str,
                    path: str,
                    expected: int,
                ) -> dict[str, Any]:
                    self.assert_request(client, method, expected)
                    self.path = path
                    return {"record": {}}

                def assert_request(self, client: object, method: str, expected: int) -> None:
                    if client is not self.ingestion or method != "GET" or expected != 200:
                        raise AssertionError("unexpected existing-seed verification request")

            clients = FakeClients()
            object_id = await _verify_existing_seed(  # type: ignore[arg-type]
                clients,
                load_manifest(manifest_path),
                "tenant-1",
            )

        self.assertTrue(object_id.startswith("production-replay:seed-stream:"))
        self.assertEqual(
            clients.path,
            f"/v1/tenants/tenant-1/records/transactions/{object_id.replace(':', '%3A')}",
        )

    async def test_seed_batch_ingests_every_transaction_without_decisions(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            write_minimal_sources(root)
            transaction_path = root / "seed-transactions-a.csv"
            write_transactions(
                transaction_path,
                [transaction_row(source_trans_id=f"seed-a-{index}") for index in range(3)],
            )
            second_transaction_path = root / "seed-transactions-b.csv"
            write_transactions(
                second_transaction_path,
                [transaction_row(source_trans_id=f"seed-b-{index}") for index in range(2)],
            )
            manifest_path = root / "manifest.json"
            manifest_path.write_text(
                json.dumps(
                    manifest_data(
                        [
                            stream("seed-stream-a", transaction_path.name),
                            stream("seed-stream-b", second_transaction_path.name),
                        ]
                    )
                ),
                encoding="utf-8",
            )
            manifest = load_manifest(manifest_path)

            class FakeClients:
                def __init__(self) -> None:
                    self.batches: list[list[dict[str, Any]]] = []
                    self.keys: list[str] = []

                async def ingest_batch(
                    self,
                    _tenant_id: str,
                    object_type: str,
                    records: list[dict[str, Any]],
                    idempotency_key: str,
                ) -> dict[str, Any]:
                    self.assert_transactions(object_type)
                    self.batches.append(records)
                    self.keys.append(idempotency_key)
                    return {"results": [{} for _record in records]}

                @staticmethod
                def assert_transactions(object_type: str) -> None:
                    if object_type != "transactions":
                        raise AssertionError(f"unexpected object type: {object_type}")

            clients = FakeClients()
            result = await seed_transactions(  # type: ignore[arg-type]
                clients,
                manifest,
                "tenant-1",
                batch_size=2,
                max_in_flight=2,
            )

        self.assertEqual(sorted(len(batch) for batch in clients.batches), [1, 2, 2])
        self.assertEqual(result["records"], 5)
        self.assertEqual(result["batches"], 3)
        self.assertEqual(result["inserted_records"], 5)
        self.assertEqual(result["inserted_batches"], 3)
        self.assertEqual(result["replayed_records"], 0)
        self.assertEqual(result["replayed_batches"], 0)
        self.assertEqual(len(set(clients.keys)), 3)
        self.assertTrue(all(key.startswith("production-replay-seed:") for key in clients.keys))

    async def test_seed_resume_counts_replayed_batches_without_new_writes(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            write_minimal_sources(root)
            transaction_path = root / "seed-transactions.csv"
            write_transactions(
                transaction_path,
                [transaction_row(source_trans_id=f"seed-{index}") for index in range(4)],
            )
            manifest_path = root / "manifest.json"
            manifest_path.write_text(
                json.dumps(manifest_data([stream("seed-stream", transaction_path.name)])),
                encoding="utf-8",
            )

            class FakeClients:
                def __init__(self) -> None:
                    self.calls = 0

                async def ingest_batch(
                    self,
                    _tenant_id: str,
                    _object_type: str,
                    records: list[dict[str, Any]],
                    _idempotency_key: str,
                ) -> dict[str, Any]:
                    self.calls += 1
                    replayed = self.calls == 1
                    return {"results": [{"replayed": replayed} for _record in records]}

            progress: list[tuple[int, int, int, int]] = []
            result = await seed_transactions(  # type: ignore[arg-type]
                FakeClients(),
                load_manifest(manifest_path),
                "tenant-1",
                batch_size=2,
                max_in_flight=1,
                progress=lambda *counts: progress.append(counts),
            )

        self.assertEqual(result["records"], 4)
        self.assertEqual(result["batches"], 2)
        self.assertEqual(result["replayed_records"], 2)
        self.assertEqual(result["replayed_batches"], 1)
        self.assertEqual(result["inserted_records"], 2)
        self.assertEqual(result["inserted_batches"], 1)
        self.assertEqual(progress[-1], (4, 2, 2, 2))

    async def test_seed_rejects_incomplete_batch_response(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            write_minimal_sources(root)
            transaction_path = root / "seed-transactions.csv"
            write_transactions(transaction_path, [transaction_row(source_trans_id="seed-1")])
            manifest_path = root / "manifest.json"
            manifest_path.write_text(
                json.dumps(manifest_data([stream("seed-stream", transaction_path.name)])),
                encoding="utf-8",
            )

            class FakeClients:
                async def ingest_batch(self, *_args: Any, **_kwargs: Any) -> dict[str, Any]:
                    return {"results": []}

            with self.assertRaisesRegex(APIError, "returned 0 results for 1 records"):
                await seed_transactions(  # type: ignore[arg-type]
                    FakeClients(),
                    load_manifest(manifest_path),
                    "tenant-1",
                )


if __name__ == "__main__":
    unittest.main()
