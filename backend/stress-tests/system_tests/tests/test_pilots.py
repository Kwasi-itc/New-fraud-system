from __future__ import annotations

import json
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from system_tests.prepare_inputs import prepare, synthetic_fields
from system_tests.service_pilot import fixture
from system_tests.report import write_report
from system_tests.postgres_pilot import require


class PilotTests(unittest.TestCase):
    def test_live_sql_contract_failure_cannot_report_success(self):
        with self.assertRaisesRegex(RuntimeError, "postgres_contract_failed:read_only"):
            require(False, "read_only")

    def test_fixture_uses_one_disposable_database_and_ordered_migrations(self):
        services = fixture("system-tests-unit")["services"]
        databases = {s["environment"]["DATABASE_URL"].split("?")[0] for s in services.values() if "DATABASE_URL" in s["environment"]}
        self.assertEqual(len(databases), 1)
        self.assertTrue(next(iter(databases)).endswith("/system_tests"))
        self.assertIn("tmpfs", services["postgres"])
        self.assertNotIn("volumes", services["postgres"])
        self.assertNotIn("ports", services["postgres"])
        self.assertIn("ingestion-migrate", services["decision-engine-migrate"]["depends_on"])
        for service in services.values():
            self.assertEqual(service["labels"]["system-tests.project"], "system-tests-unit")

    def test_preparation_selects_exact_cohort_and_refuses_overwrite(self):
        with TemporaryDirectory() as directory:
            path = Path(directory) / "source.ndjson"
            evidence = prepare(path, synthetic_fields("a", 5), count=2, skip=2)
            rows = [json.loads(line) for line in path.read_text().splitlines()]
            self.assertEqual(evidence["records"], 2)
            self.assertEqual(rows[0]["object_id"], "a-000000002")
            with self.assertRaises(FileExistsError):
                prepare(path, synthetic_fields("b", 1), 1)
            missing = Path(directory) / "short.ndjson"
            with self.assertRaises(ValueError):
                prepare(missing, synthetic_fields("b", 1), 2)
            self.assertFalse(missing.exists())

    def test_report_escapes_untrusted_labels_and_marks_missing_evidence(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            state = {"status": "incomplete", "planned_runs": 2, "runs": [{"name": "one",
                "profile": "<script>alert(1)</script>", "status": "failed"}]}
            content = write_report(root, state).read_text()
            self.assertNotIn("<script>", content)
            self.assertIn("no report", content)
            self.assertIn("Discovery evidence only", content)


if __name__ == "__main__":
    unittest.main()
