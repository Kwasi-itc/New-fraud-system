import contextlib
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parent))
import run
from pipeline import Pipeline


class RunnerTests(unittest.TestCase):
    def test_current_inventory_counts(self):
        counts = {name: len(run.inventory(run.REPO / "backend" / service / "internal/httpapi/openapi.yaml"))
                  for name, service in run.SERVICES.items()}
        self.assertEqual(counts, {"data-model": 43, "ingestion": 15, "decision": 120})

    def test_missing_endpoint_is_a_gap_even_when_other_tests_pass(self):
        operations = ["GET /a", "POST /b"]
        report = {"coverage": [{"operation": "GET /a", "covered": True,
                                "successfulRequests": 1, "suites": ["behavior"]}]}
        result = run.coverage("ingestion", operations, report)
        self.assertEqual(result["verified"], 1)
        self.assertEqual(result["missing"], ["POST /b"])

    def test_error_probe_does_not_count_as_success(self):
        report = {"matrix": [{"method": "POST", "path": "/a", "status": "coverage_gap",
                              "probeStatus": 400, "successfulSuites": []}]}
        self.assertEqual(run.coverage("decision", ["POST /a"], report)["verified"], 0)

    def test_claim_without_positive_evidence_does_not_cover_endpoint(self):
        report = {"coverage": [{"operation": "GET /a", "covered": True,
                                "successfulRequests": 0, "suites": []}]}
        self.assertEqual(run.coverage("data-model", ["GET /a"], report)["missing"], ["GET /a"])

    def test_stale_inventory_and_duplicate_entries_are_detected(self):
        item = {"operation": "GET /old", "covered": True, "successfulRequests": 1, "suites": ["x"]}
        self.assertEqual(run.coverage("data-model", ["GET /new"], {"coverage": [item]})["stale_inventory"], ["GET /old"])
        with self.assertRaisesRegex(ValueError, "Duplicate coverage"):
            run.coverage("data-model", [], {"coverage": [item, item]})

    def test_duplicate_spec_entries_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "spec.yaml"
            path.write_text("paths:\n  /a:\n    get:\n    get:\ncomponents:\n")
            with self.assertRaisesRegex(ValueError, "duplicate"):
                run.inventory(path)

    def test_child_failure_propagates_with_log(self):
        with tempfile.TemporaryDirectory() as temp, contextlib.redirect_stdout(io.StringIO()):
            log = Path(temp) / "child.log"
            code = run.execute([sys.executable, "-c", "print('failure evidence'); raise SystemExit(7)"], log)
            self.assertEqual(code, 7)
            self.assertIn("failure evidence", log.read_text())

    def test_timeout_terminates_child(self):
        with tempfile.TemporaryDirectory() as temp, contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaisesRegex(TimeoutError, "Stage exceeded"):
                run.execute([sys.executable, "-c", "import time; time.sleep(20)"], Path(temp) / "timeout.log", timeout=0.1)

    def test_restoration_unpauses_and_starts_worker(self):
        before = {"ingestion-worker": {"callback_mode": "accept"}}
        with patch.object(run, "inspect", return_value={"paused": True, "running": False}), \
             patch.object(run, "capture") as capture, patch.object(run, "wait_healthy") as healthy:
            self.assertEqual(run.restore(before, Path("/unused")), [])
            self.assertEqual([c.args[:2] for c in capture.call_args_list], [("docker", "unpause"), ("docker", "start")])
            healthy.assert_called_once_with("ingestion-worker")

    def test_cleanup_failure_is_reported(self):
        with patch.object(run, "inspect", side_effect=RuntimeError("Docker unavailable")):
            self.assertIn("Docker unavailable", run.restore({"ingestion-worker": {}}, Path("/unused"))[0])

    def test_empty_or_partial_reference_is_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            local = Path(temp)
            (local / 'plans').mkdir()
            (local / 'plans/data_model.json').write_text(json.dumps({'steps': [{}, {}]}))
            with patch.object(run, 'LOCAL', local):
                with patch.object(run, 'capture', return_value='2\t2\n'):
                    run.reference_ready()
                for value in ('0\tNULL\n', '2\t1\n', '1\t1\n'):
                    with patch.object(run, 'capture', return_value=value):
                        with self.assertRaisesRegex(RuntimeError, 'Reference MySQL lacks'):
                            run.reference_ready()

    def test_pipeline_failed_assertion_remains_failure(self):
        with tempfile.TemporaryDirectory() as temp, contextlib.redirect_stdout(io.StringIO()):
            pipeline = Pipeline(Path(temp))
            def fail():
                raise RuntimeError("wrong persisted score")
            self.assertFalse(pipeline.test("contract", fail))
            self.assertFalse(pipeline.cases[0]["passed"])
            self.assertIn("persisted score", pipeline.cases[0]["error"])

    def test_http_negative_expectations_are_explicit_and_traced(self):
        class Response(io.BytesIO):
            code = 403
        with tempfile.TemporaryDirectory() as temp:
            pipeline = Pipeline(Path(temp))
            with patch("pipeline.request.urlopen", return_value=Response(b'{"error":"denied"}')):
                self.assertEqual(pipeline.call("ingestion", "GET", "/foreign", expected=403)["error"], "denied")
            trace = json.loads((Path(temp) / "http.jsonl").read_text())
            self.assertEqual(trace["status"], 403)
            self.assertEqual(trace["expected"], 403)


if __name__ == "__main__":
    unittest.main()
