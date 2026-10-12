from __future__ import annotations

import json
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import AsyncMock, patch

from system_tests.local_stress import build_parser, exercise, local_fixture, validate


class LocalStressTests(unittest.IsolatedAsyncioTestCase):
    def args(self, *extra):
        args = build_parser().parse_args(["--output", "unused", *extra])
        validate(args)
        return args

    def test_isolated_disk_database_and_loopback_apis(self):
        services = local_fixture("system-tests-unit")["services"]
        dbs = {s["environment"]["DATABASE_URL"].split("?")[0] for s in services.values()
               if "DATABASE_URL" in s["environment"]}
        self.assertEqual(len(dbs), 1)
        self.assertNotIn("tmpfs", services["postgres"])
        self.assertEqual(services["postgres"]["volumes"], ["/var/lib/postgresql/data"])
        self.assertNotIn("ports", services["postgres"])
        for service in services.values():
            for port in service.get("ports", []):
                self.assertTrue(port.startswith("127.0.0.1::"))

    def test_invalid_load_fails_before_starting_services(self):
        for flags in [("--max-inflight", "0"), ("--deadline", "nan"),
                      ("--stage", "big:1000001:1"),
                      ("--stage", "same:1:1", "--stage", "same:2:1")]:
            with self.subTest(flags=flags), self.assertRaises(ValueError):
                self.args(*flags)

    async def test_setup_identity_schedule_and_failures_are_propagated(self):
        args = self.args("--stage", "first:2:2", "--stage", "second:3:1", "--path", "ingest-async")
        endpoints = {role: "http://127.0.0.1:1234" for role in ("data-model", "ingestion", "decision-engine")}
        for exit_code in (0, 2):
            with self.subTest(exit_code=exit_code), TemporaryDirectory() as directory:
                output = Path(directory)
                with patch("system_tests.local_stress.EnvironmentSetup") as setup, \
                     patch("system_tests.local_stress.run_experiment", new_callable=AsyncMock) as run:
                    setup.return_value.run = AsyncMock(return_value={"tenant_id": "new-tenant", "scenarios": [1, 2, 3, 4]})
                    run.return_value = exit_code
                    result = await exercise(output, endpoints, args)
                self.assertEqual(result["passed"], exit_code == 0)
                command = run.call_args.args[0]
                self.assertEqual(command[command.index("--tenant-id") + 1], "new-tenant")
                self.assertEqual(command[command.index("--expected-scenarios") + 1], "4")
                rows = [json.loads(line) for line in (output / "source/transactions.ndjson").read_text().splitlines()]
                self.assertEqual(len(rows), 7)
                self.assertEqual(len({row["object_id"] for row in rows}), 7)

    async def test_setup_failure_never_submits_load(self):
        with TemporaryDirectory() as directory, patch("system_tests.local_stress.EnvironmentSetup") as setup, \
             patch("system_tests.local_stress.run_experiment", new_callable=AsyncMock) as run:
            setup.return_value.run = AsyncMock(side_effect=RuntimeError("setup failed"))
            with self.assertRaisesRegex(RuntimeError, "setup failed"):
                await exercise(Path(directory), {role: "http://127.0.0.1:1234" for role in
                    ("data-model", "ingestion", "decision-engine")}, self.args())
            run.assert_not_awaited()
