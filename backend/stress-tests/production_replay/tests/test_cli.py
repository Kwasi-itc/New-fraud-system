from __future__ import annotations

import argparse
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

from production_replay.cli import _load_profile, _seed, _setup, build_parser


class CLITests(unittest.TestCase):
    def test_run_defaults_to_async_decision_mode(self) -> None:
        parser = build_parser()
        args = parser.parse_args(
            [
                "run",
                "--manifest",
                "manifest.json",
            ]
        )
        self.assertEqual(args.decision_mode, "async")

    def test_seed_defaults_to_maximum_ingestion_batch_size(self) -> None:
        args = build_parser().parse_args(["seed", "--manifest", "manifest.json"])
        self.assertEqual(args.batch_size, 500)
        self.assertEqual(args.max_in_flight, 10)
        self.assertFalse(args.reuse_existing)

    def test_existing_seed_reuse_is_explicit(self) -> None:
        args = build_parser().parse_args(
            ["seed", "--manifest", "manifest.json", "--reuse-existing"]
        )
        self.assertTrue(args.reuse_existing)

    def test_setup_reuse_is_explicit(self) -> None:
        args = build_parser().parse_args(
            ["setup", "--manifest", "manifest.json", "--reuse-existing"]
        )
        self.assertTrue(args.reuse_existing)

    def test_profile_input_is_reused_after_fingerprint_validation(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "profile.json"
            path.write_text(
                json.dumps(
                    {
                        "profile_version": 1,
                        "source_fingerprint": "source-1",
                        "transactions": {
                            "row_count": 1,
                            "earliest": "2026-07-01T00:00:00Z",
                            "latest": "2026-07-01T00:00:00Z",
                            "average_events_per_second": 1,
                            "p95_events_per_second": 1,
                            "p99_events_per_second": 1,
                            "peak_events_per_second": 1,
                        },
                        "reference_data": {
                            "merchants": {"unique_keys": 1},
                            "merchant_products": {"unique_keys": 1},
                            "staff": {"source_rows": 1},
                        },
                    }
                ),
                encoding="utf-8",
            )
            manifest = SimpleNamespace(source_fingerprint=lambda: "source-1")

            profile = _load_profile(str(path), manifest)  # type: ignore[arg-type]

            self.assertEqual(profile["transactions"]["row_count"], 1)

    def test_profile_input_rejects_changed_sources(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "profile.json"
            path.write_text(
                json.dumps(
                    {
                        "profile_version": 1,
                        "source_fingerprint": "old-source",
                        "transactions": {},
                        "reference_data": {},
                    }
                ),
                encoding="utf-8",
            )
            manifest = SimpleNamespace(source_fingerprint=lambda: "current-source")

            with self.assertRaisesRegex(ValueError, "fingerprint does not match"):
                _load_profile(str(path), manifest)  # type: ignore[arg-type]


class SetupReuseTests(unittest.IsolatedAsyncioTestCase):
    async def test_reuse_verifies_without_running_environment_setup(self) -> None:
        class FakeClients:
            def __init__(self) -> None:
                self.data_model = object()
                self.requests: list[tuple[str, str]] = []

            async def __aenter__(self) -> "FakeClients":
                return self

            async def __aexit__(self, *_args: object) -> None:
                return None

            async def wait_until_ready(self) -> None:
                return None

            async def request(
                self,
                _client: object,
                method: str,
                path: str,
                _expected: int,
            ) -> dict[str, object]:
                self.requests.append((method, path))
                return {"tenant": {"name": "Existing Replay"}}

            async def backfill_aggregate_facts(
                self,
                tenant_id: str,
                *,
                timeout_seconds: float,
            ) -> dict[str, object]:
                self.requests.append(("BACKFILL", tenant_id))
                self.assert_timeout = timeout_seconds
                return {
                    "aggregate_fact_backfill": {
                        "rebuilt": False,
                        "status": {"ready": True},
                    }
                }

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest_path = root / "manifest.json"
            manifest_path.write_text("{}\n", encoding="utf-8")
            args = argparse.Namespace(
                execute=True,
                publication_timeout=900.0,
                output_root=str(root / "runs"),
                reuse_existing=True,
                tenant_id="tenant-1",
                tenant_name="unused",
                data_model_url="http://data-model",
                ingestion_url="http://ingestion",
                decision_engine_url="http://decision",
                auth_token=None,
                timeout=30.0,
            )
            clients = FakeClients()
            verify = AsyncMock()
            with (
                patch("production_replay.cli.ServiceClients", return_value=clients),
                patch("production_replay.cli._verify_replay_tenant", verify),
                patch("production_replay.cli.EnvironmentSetup") as environment_setup,
            ):
                result = await _setup(
                    args,
                    SimpleNamespace(path=manifest_path),  # type: ignore[arg-type]
                    {"transactions": {}},
                )

            self.assertEqual(result, 0)
            self.assertEqual(
                clients.requests,
                [("GET", "/v1/tenants/tenant-1"), ("BACKFILL", "tenant-1")],
            )
            verify.assert_awaited_once()
            environment_setup.assert_not_called()
            setup_files = list((root / "runs").glob("setup-*/setup.json"))
            self.assertEqual(len(setup_files), 1)
            setup_result = json.loads(setup_files[0].read_text(encoding="utf-8"))
            self.assertTrue(setup_result["reused_existing_setup"])
            self.assertFalse(setup_result["mutations_performed"])


class SeedReuseTests(unittest.IsolatedAsyncioTestCase):
    async def test_reuse_verifies_existing_seed_without_ingesting(self) -> None:
        class FakeClients:
            async def __aenter__(self) -> "FakeClients":
                return self

            async def __aexit__(self, *_args: object) -> None:
                return None

            async def wait_until_ingestion_ready(self) -> None:
                return None

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest_path = root / "manifest.json"
            manifest_path.write_text("{}\n", encoding="utf-8")
            args = argparse.Namespace(
                execute=False,
                reuse_existing=True,
                tenant_id="tenant-1",
                batch_size=500,
                max_in_flight=10,
                progress_every=100,
                output_root=str(root / "runs"),
                data_model_url="http://data-model",
                ingestion_url="http://ingestion",
                decision_engine_url="http://decision",
                auth_token=None,
                timeout=30.0,
            )
            verify_seed_tenant = AsyncMock()
            verify_existing_seed = AsyncMock(return_value="seed-object-1")
            with (
                patch("production_replay.cli.ServiceClients", return_value=FakeClients()),
                patch("production_replay.cli._verify_seed_tenant", verify_seed_tenant),
                patch("production_replay.cli._verify_existing_seed", verify_existing_seed),
                patch("production_replay.cli.seed_transactions", new_callable=AsyncMock) as ingest,
            ):
                result = await _seed(
                    args,
                    SimpleNamespace(path=manifest_path),  # type: ignore[arg-type]
                )

            self.assertEqual(result, 0)
            verify_seed_tenant.assert_awaited_once()
            verify_existing_seed.assert_awaited_once()
            ingest.assert_not_awaited()
            summary_files = list((root / "runs").glob("seed-*/summary.json"))
            self.assertEqual(len(summary_files), 1)
            summary = json.loads(summary_files[0].read_text(encoding="utf-8"))
            self.assertEqual(summary["status"], "reused_existing")
            self.assertIsNone(summary["records"])
            self.assertFalse(summary["mutations_performed"])


if __name__ == "__main__":
    unittest.main()
