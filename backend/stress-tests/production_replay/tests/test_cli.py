from __future__ import annotations

import argparse
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

from production_replay.cli import _setup, build_parser


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

    def test_setup_reuse_is_explicit(self) -> None:
        args = build_parser().parse_args(
            ["setup", "--manifest", "manifest.json", "--reuse-existing"]
        )
        self.assertTrue(args.reuse_existing)


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
            self.assertEqual(clients.requests, [("GET", "/v1/tenants/tenant-1")])
            verify.assert_awaited_once()
            environment_setup.assert_not_called()
            setup_files = list((root / "runs").glob("setup-*/setup.json"))
            self.assertEqual(len(setup_files), 1)
            setup_result = json.loads(setup_files[0].read_text(encoding="utf-8"))
            self.assertTrue(setup_result["reused_existing_setup"])
            self.assertFalse(setup_result["mutations_performed"])


if __name__ == "__main__":
    unittest.main()
