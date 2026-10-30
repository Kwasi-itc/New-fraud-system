"""No service/database needed. Fake HTTP deliberately corrupts business results."""
from __future__ import annotations

import asyncio
import copy
import json
import sys
import tempfile
import time
import unittest
from dataclasses import replace
from pathlib import Path

HERE = Path(__file__).resolve().parents[1]
sys.path[:0] = [str(HERE), str(HERE.parent)]

import httpx
from decision_benchmarks.api import CorrectnessError, decision_path, verify
from decision_benchmarks.cli import fixture, load
from decision_benchmarks.model import Experiment, digest, expected, formula, history_oracle, payload, reference_cases
from decision_benchmarks.report import report
from decision_benchmarks.run import phase, latency_summary, ingestion_interference
from production_replay.replay import LatencyMetric
from system_tests.arrival_schedule import arrivals
from system_tests.queue_campaign import Stage


def fixture_data(scenarios=1, rules=1, triggered=True):
    return {"tenant_id": "tenant-a", "scenarios": [
        {"id": f"s{s}", "iteration_id": f"i{s}", "triggered": triggered,
         "rules": [{"id": f"r{s}-{r}", "index": r, "name": f"rule-{r}"} for r in range(rules)]}
        for s in range(scenarios)]}


def good_response(fields):
    # Hand-built one-rule numeric contract, independent from the harness verifier.
    matched = float(fields["amount"]) > 3000
    return {"result": {"model_revision": "model-v1", "triggered": True,
        "decision": {"id": "d-"+fields["object_id"], "tenant_id": "tenant-a", "scenario_id": "s0",
                     "scenario_iteration_id": "i0", "object_id": fields["object_id"], "object_type": "transactions",
                     "triggered": True, "score": int(matched), "outcome": "review" if matched else "approve"},
        "rule_executions": [{"id": "re", "decision_id": "d-"+fields["object_id"], "rule_id": "r0-0",
                            "rule_name": "rule-0", "score_modifier": 1, "outcome": "hit" if matched else "no_hit",
                            "evaluation": {"function": "gt", "return_value": matched}}]}}


class Contracts(unittest.TestCase):
    def test_invalid_config_and_resource_limits(self):
        for change in ({"rules": 0}, {"rate": float("nan")}, {"concurrency": True}, {"families": ["related_records"]},
                       {"route": "single", "scenarios": 2}, {"combination": "none", "depth": 2}, {"distinct": "yes"},
                       {"unknown": 1}, {"name": "../escape"}, {"duration": 1e9}):
            with self.subTest(change=change), self.assertRaises(ValueError):
                Experiment.parse(change)

    def test_seed_independent_of_order(self):
        exp = Experiment()
        expected_records = [payload(exp, i, "run") for i in range(50)]
        by_index = {i: payload(exp, i, "run") for i in reversed(range(50))}
        self.assertEqual(expected_records, [by_index[i] for i in range(50)])
        self.assertNotEqual(digest(exp.manifest()), digest(replace(exp, seed=1).manifest()))

    def test_hand_calculated_history_and_empty(self):
        value = history_oracle(Experiment(history_rows=20, history_keys=2))["m0"]
        self.assertEqual(value, {"count": 4, "sum": 21, "avg": 7, "min": 3, "max": 13, "count_distinct": 4})
        self.assertEqual(history_oracle(Experiment(history_rows=0)), {})
        self.assertEqual(expected(Experiment(families=("avg",)), 0, {"merchant_id": "absent"}, {}), (False, None))

    def test_comparison_boundaries_and_nullable_normalization(self):
        fields = payload(Experiment(), 0, "run", match=True)
        fields["amount"] = 3000
        for operator, want in (("gt", False), ("gte", True), ("lt", False), ("lte", True), ("eq", True), ("neq", False)):
            self.assertEqual(expected(Experiment(operator=operator), 0, fields, {})[0], want)
        fields.pop("merchant_id")
        self.assertFalse(expected(Experiment(families=("nonnull",)), 0, fields, {})[0])
        fields["merchant_id"] = ""
        self.assertTrue(expected(Experiment(families=("nonnull",)), 0, fields, {})[0])

    def test_generated_json_is_real_ast(self):
        self.assertEqual(formula(Experiment(), 0), {"function": "gt", "children": [
            {"function": "field_ref", "named_children": {"field": {"constant": "amount"}}}, {"constant": 3000}]})
        self.assertEqual(formula(Experiment(families=("count",)), 0)["children"][0]["named_children"]["aggregator"], {"constant": "COUNT"})

    def test_reject_mutated_response_contracts(self):
        exp, fix = Experiment(), fixture_data()
        fields = payload(exp, 0, "test", match=True)
        body = good_response(fields)
        self.assertEqual(len(verify(exp, fix, fields, body, 200, {})), 1)
        changes = [lambda b: b["result"]["decision"].update(tenant_id="other"),
                   lambda b: b["result"]["decision"].update(score=0),
                   lambda b: b["result"]["decision"].update(outcome="approve"),
                   lambda b: b["result"]["rule_executions"][0].update(outcome="no_hit"),
                   lambda b: b["result"]["rule_executions"][0].update(rule_id="foreign"),
                   lambda b: b["result"]["rule_executions"].clear(),
                   lambda b: b["result"].update(triggered=False),
                   lambda b: b.update(deferred=True)]
        for change in changes:
            bad = copy.deepcopy(body)
            change(bad)
            with self.assertRaises(CorrectnessError):
                verify(exp, fix, fields, bad, 200, {})
        with self.assertRaises(CorrectnessError):
            verify(exp, fix, fields, body, 202, {})

    def test_rejection_and_trigger_miss_are_not_decisions(self):
        exp = Experiment()
        fields = payload(exp, 0, "test")
        body = {"error": {"category": "payload_validation_failed"}}
        self.assertEqual(verify(exp, fixture_data(), fields, body, 422, {}, 422), [])
        with self.assertRaises(CorrectnessError):
            verify(exp, fixture_data(), fields, {"error": {"category": "dependency_failure"}}, 422, {}, 422)
        miss = {"result": {"model_revision": "v1", "triggered": False, "rule_executions": []}}
        self.assertEqual(verify(exp, fixture_data(triggered=False), fields, miss, 200, {}), [])
        with self.assertRaises(CorrectnessError):
            verify(exp, fixture_data(), fields, miss, 200, {})

    def test_aggregate_value_checked_even_if_rule_matches(self):
        exp = Experiment(families=("sum",))
        fields = payload(exp, 0, "test", match=True)
        body = good_response(fields)
        body["result"]["rule_executions"][0]["evaluation"]["children"] = [
            {"function": "Aggregator", "return_value": 7}]
        self.assertEqual(len(verify(exp, fixture_data(), fields, body, 200, {"m0": {"sum": 7}})), 1)
        with self.assertRaises(CorrectnessError):
            verify(exp, fixture_data(), fields, body, 200, {"m0": {"sum": 8}})

    def test_reference_suite_has_invalid_and_boundary_inputs(self):
        cases = list(reference_cases(Experiment()))
        self.assertGreater(len(cases), 30)
        self.assertTrue(any(x[1].get("amount") == 3000 for x in cases))
        self.assertTrue(any(x[2] == 422 and "amount" not in x[1] for x in cases))

    def test_authenticated_disposable_compose(self):
        composed = fixture("system-tests-test", None, "direct_db")
        dbs = set()
        for role, service in composed["services"].items():
            self.assertEqual(service["labels"]["system-tests.disposable"], "true")
            if role != "postgres":
                self.assertEqual(service["environment"]["SERVICE_AUTH_MODE"], "token")
                self.assertNotIn("disabled", service["environment"].values())
                dbs.add(service["environment"]["DATABASE_URL"].split("?")[0])
        self.assertEqual(len(dbs), 1)
        self.assertNotIn("tmpfs", composed["services"]["postgres"])

    def test_percentiles_and_empty_metrics(self):
        metric = LatencyMetric()
        self.assertIsNone(latency_summary(metric)["p90_ms"])
        for x in range(1, 101):
            metric.add(x)
        self.assertEqual(latency_summary(metric)["p90_ms"], 90)

    def test_smoke_manifest(self):
        self.assertEqual(len(load(HERE / "examples/smoke.json")), 3)

    def test_reports_do_not_average_percentiles_or_hide_failed_runs(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for i, valid in enumerate((True, True, False)):
                folder = root / str(i)
                folder.mkdir()
                (folder / "summary.json").write_text(json.dumps({"schema_version": "decision-benchmark/v1",
                    "benchmark_id": "example", "valid": valid, "configuration": Experiment().manifest(),
                    "measurement": {"rates": {"correct_inputs_per_second": 10+i}}}), encoding="utf-8")
            result = report(root)
            self.assertEqual(len(result["runs"]), 3)
            self.assertEqual(result["comparisons"][0]["valid_repeats"], 2)
            self.assertFalse(result["comparisons"][0]["variability_qualified"])


class AsyncContracts(unittest.IsolatedAsyncioTestCase):
    async def test_ingestion_interference_checks_identity_and_never_retries(self):
        class Clients:
            def __init__(self, corrupt):
                self.corrupt = corrupt
                self.calls = 0

            async def ingest_one(self, tenant, table, fields, key, max_attempts):
                self.calls += 1
                self_test.assertEqual((tenant, table, max_attempts), ("tenant-a", "history", 1))
                self_test.assertEqual(fields["merchant_id"], "background-unselected")
                return {"result": {"object_id": "wrong" if self.corrupt else fields["object_id"],
                                   "revision_id": "revision", "replayed": False}}, 1
        self_test = self
        for corrupt in (False, True):
            clients = Clients(corrupt)
            with tempfile.TemporaryDirectory() as directory:
                result = await ingestion_interference(clients, Experiment(ingestion_rate=10), [fixture_data()],
                    Path(directory), "measured", 0.01, time.monotonic())
                self.assertEqual(clients.calls, 1)
                self.assertEqual(result["valid"], not corrupt)

    async def test_schedule_preserves_deadlines_and_stage_boundaries(self):
        waits, starts, slots = [], [], []
        async def wait(when):
            waits.append(when)
        async for stage, slot, scheduled in arrivals([Stage("a",2,1),Stage("b",1,1)], 10, wait=wait, on_stage=starts.append):
            slots.append((stage.name, slot, scheduled))
        self.assertEqual(slots, [("a",0,10),("a",1,10.5),("b",0,11)])
        self.assertEqual(waits[-1], 12)
        self.assertEqual(starts, ["a","b"])

    async def test_fixed_rate_drops_and_latency_accounting(self):
        class Clients:
            pass
        clients = Clients()
        async def handler(request):
            fields = json.loads(request.content)["fields"]
            await asyncio.sleep(0.035)
            return httpx.Response(200, json=good_response(fields))
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler), base_url="http://fixture") as client:
            clients.decision_engine = client
            with tempfile.TemporaryDirectory() as directory:
                exp = Experiment(rate=100, duration=0.1, concurrency=1, warmup=0)
                result = await phase(clients, exp, [fixture_data()], {}, Path(directory), "measured", exp.duration)
                counts = result["counts"]
                self.assertGreater(counts["generator_dropped"], 0)
                self.assertEqual(counts["offered"], counts["submitted"]+counts["generator_dropped"])
                self.assertEqual(counts["correct"], counts["submitted"])
                self.assertEqual(result["latencies"]["all_attempts"]["count"], counts["submitted"])

    async def test_failures_not_retried_or_omitted(self):
        class Clients:
            pass
        clients = Clients()
        calls = 0
        async def handler(request):
            nonlocal calls
            calls += 1
            return httpx.Response(202, json={"deferred": True})
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler), base_url="http://fixture") as client:
            clients.decision_engine = client
            with tempfile.TemporaryDirectory() as directory:
                result = await phase(clients, Experiment(rate=10, duration=0.1), [fixture_data()], {}, Path(directory), "measured", 0.1)
                self.assertEqual(calls, 1)
                self.assertEqual(result["counts"]["correct"], 0)
                self.assertEqual(result["counts"]["failed"], 1)
                self.assertEqual(result["counts"]["unresolved"], 1)
                self.assertEqual(result["latencies"]["failed"]["count"], 1)


if __name__ == "__main__":
    unittest.main()
