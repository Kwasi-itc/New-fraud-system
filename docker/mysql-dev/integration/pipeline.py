#!/usr/bin/env python3
"""Assert data-model publication, ingestion, stored-record evaluation and workers."""
import argparse
import json
from pathlib import Path
import sys
import time
from urllib import error, parse, request
import uuid

REPO = Path(__file__).resolve().parents[3]
LOCAL = REPO / ".local/mysql-dev"
ORIGINS = {"model": "http://localhost:8080", "ingestion": "http://localhost:8081", "decision": "http://localhost:8082"}


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


class Pipeline:
    def __init__(self, output):
        self.output = output
        self.prefix = "integration-" + str(uuid.uuid4())
        self.case = "setup"
        self.cases = []
        self.fixtures = {"prefix": self.prefix}
        self.state = {}
        self.published = False

    def call(self, service, method, path, body=None, expected=200, key=None):
        entry = {"case": self.case, "service": service, "method": method, "path": path, "expected": expected}
        headers = {"Content-Type": "application/json"}
        if key:
            headers["Idempotency-Key"] = key
        req = request.Request(ORIGINS[service] + path, method=method, headers=headers,
                              data=None if body is None else json.dumps(body).encode())
        try:
            try:
                response = request.urlopen(req, timeout=30)
            except error.HTTPError as exc:
                response = exc
            with response:
                raw = response.read().decode()
                entry.update(status=response.code, response=raw)
                require(response.code in ([expected] if isinstance(expected, int) else expected),
                        f"{method} {path}: HTTP {response.code}: {raw[:1000]}")
                return json.loads(raw) if raw else None
        except (OSError, ValueError, RuntimeError) as exc:
            entry["error"] = str(exc)
            raise
        finally:
            with (self.output / "http.jsonl").open("a") as trace:
                trace.write(json.dumps(entry) + "\n")

    def test(self, name, fn):
        self.case = name
        started = time.monotonic()
        try:
            fn()
            self.cases.append({"name": name, "passed": True})
        except (OSError, ValueError, KeyError, IndexError, TypeError, RuntimeError) as exc:
            self.cases.append({"name": name, "passed": False, "error": str(exc)})
        self.cases[-1]["elapsed_seconds"] = round(time.monotonic() - started, 2)
        print(f"{'PASS' if self.cases[-1]['passed'] else 'FAIL'} pipeline/{name}", flush=True)
        return self.cases[-1]["passed"]

    def setup(self):
        scope = dict(line.split("=", 1) for line in (LOCAL / "ingestion.env").read_text().splitlines() if "=" in line)
        tenant, obj = scope["INGESTION_TENANT_ID"], scope["INGESTION_OBJECT_TYPE"]
        uuid.UUID(tenant)
        self.root = f"/v1/tenants/{tenant}"
        self.tenant, self.object = tenant, obj
        self.ingest = self.root + "/ingest/" + obj
        self.records = self.root + "/records/" + obj
        self.fixtures.update(tenant_id=tenant, object_type=obj)
        require(self.call("model", "GET", self.root)["tenant"]["status"] == "active", "Tenant must be active")
        tables = self.call("model", "GET", self.root + "/tables")["tables"]
        table = next((t for t in tables if t["name"] == obj and not t["archived"]), None)
        require(table is not None, "Configured ingestion table is missing")
        fields = {f["name"]: f for f in self.call("model", "GET", f"/v1/tables/{table['id']}/fields")["fields"]}
        require(fields["amount"]["data_type"] == "float" and fields["currency"]["data_type"] == "string",
                "Pipeline uses the setup-mac.py demo model: amount=float, currency=string")
        model = self.call("model", "GET", self.root + "/data-model")["data_model"]
        require(model["tables"][obj]["id"] == table["id"], "Published model/table identity mismatch")
        self.fixtures["table_id"] = table["id"]
        parent = self.call("decision", "POST", self.root + "/scenarios", {
            "name": self.prefix, "trigger_object_type": obj}, 201)["scenario"]
        self.parent = self.root + "/scenarios/" + parent["id"]
        self.fixtures["scenario_id"] = parent["id"]
        iteration = self.call("decision", "POST", self.parent + "/iterations", {}, 201)["iteration"]
        self.fixtures["iteration_id"] = iteration["id"]
        path = self.parent + "/iterations/" + iteration["id"]
        self.call("decision", "PUT", path, {
            "trigger_formula": {"function": "eq", "children": [
                {"function": "field_ref", "named_children": {"field": {"constant": "currency"}}},
                {"constant": self.prefix}]},
            "score_review_threshold": 30, "score_block_and_review_threshold": 60, "score_decline_threshold": 90})
        self.call("decision", "POST", path + "/rules", {
            "name": "Pipeline amount threshold", "display_order": 1, "score_modifier": 40,
            "rule_group": "integration", "formula": {"function": "gte", "children": [
                {"function": "field_ref", "named_children": {"field": {"constant": "amount"}}},
                {"constant": 10000}]}}, 201)
        require(self.call("decision", "POST", path + "/validate", {})["validation"]["valid"], "Rule validation failed")
        self.call("decision", "POST", path + "/commit", {})
        self.call("decision", "POST", self.parent + "/publications", {"action": "publish", "iteration_id": iteration["id"]})
        self.published = True

    def ingest_replay(self):
        self.record = self.prefix + "-record"
        body = {"object_id": self.record, "amount": 12500.0, "currency": self.prefix}
        key = self.prefix + "-create"
        require(self.call("ingestion", "POST", self.ingest, body, key=key)["result"]["action"] == "created", "Record not created")
        require(self.call("ingestion", "POST", self.ingest, body, key=key)["result"]["replayed"] is True, "Receipt was not replayed")
        self.call("ingestion", "POST", self.ingest, {**body, "amount": 1}, 409, key=key)
        saved = self.call("ingestion", "GET", self.records + "/" + self.record)["record"]
        require(saved["fields"]["amount"] == 12500 and saved["fields"]["currency"] == self.prefix, "Ingested values differ")

    def decision(self, object_id, score):
        result = self.call("decision", "POST", self.root + "/decisions", {
            "scenario_id": self.fixtures["scenario_id"], "object_type": self.object, "object_id": object_id})["result"]
        value = result["decision"]
        require(result["triggered"] is True and value["tenant_id"] == self.tenant and
                value["scenario_id"] == self.fixtures["scenario_id"] and value["object_id"] == object_id,
                "Stored-record decision identity mismatch")
        require(value["score"] == score and value["outcome"] == ("review" if score else "approve"), "Stored-record rule result mismatch")
        detail = self.call("decision", "GET", self.root + "/decisions/" + value["id"])
        require(detail["decision"]["id"] == value["id"] and detail["decision"]["score"] == score,
                "Decision response was not persisted correctly")
        require(len(detail["rule_executions"]) == 1, "Rule execution evidence missing")
        return value

    def stored_decision(self):
        self.fixtures["decision_id"] = self.decision(self.record, 40)["id"]

    def wait(self, path, key):
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            wire = self.call("decision", "GET", path)
            value = wire.get(key, wire)
            require(value["status"] not in ("failed", "cancelled"), f"Worker failed: {value}")
            if value["status"] == "completed":
                return value
            time.sleep(0.3)
        raise TimeoutError(f"Worker did not complete: {path}")

    def history(self, object_id):
        page = self.call("decision", "GET", self.parent + "/decisions?limit=100&object_id=" + parse.quote(object_id, safe=""))
        return page["decisions"]

    def workers(self):
        item = {"object_id": self.record, "object_type": self.object}
        body = {"scenario_id": self.fixtures["scenario_id"], "object_type": self.object,
                "idempotency_key": self.prefix + "-async", "items": [item]}
        before = {d["id"] for d in self.history(self.record)}
        execution = self.call("decision", "POST", self.root + "/async-decision-executions", body, (200, 202))["async_decision_execution"]
        self.wait(self.root + "/async-decision-executions/" + execution["id"], "async_decision_execution")
        replay = self.call("decision", "POST", self.root + "/async-decision-executions", body, (200, 202))
        require(replay["replay"] is True and replay["async_decision_execution"]["id"] == execution["id"], "Async replay created different work")
        after = self.history(self.record)
        require(len({d["id"] for d in after} - before) == 1 and all(d["score"] == 40 for d in after), "Async worker persistence mismatch")
        scheduled = self.call("decision", "POST", self.parent + "/scheduled-executions", {
            "scheduled_for": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "idempotency_key": self.prefix + "-scheduled", "items": [item]}, 202)["scheduled_execution"]
        self.wait(self.parent + "/scheduled-executions/" + scheduled["id"], "scheduled_execution")
        require(len(self.history(self.record)) == len(after) + 1, "Scheduled worker decision missing")
        self.fixtures.update(async_execution_id=execution["id"], scheduled_execution_id=scheduled["id"])

    def mutation_visibility(self):
        self.call("ingestion", "PATCH", self.ingest, {"object_id": self.record, "amount": 9999}, key=self.prefix + "-patch")
        require(self.call("ingestion", "GET", self.records + "/" + self.record)["record"]["fields"]["currency"] == self.prefix,
                "PATCH lost an unchanged field")
        self.decision(self.record, 0)

    def rollback_and_tenant_isolation(self):
        good, bad = self.prefix + "-atomic-good", self.prefix + "-atomic-bad"
        self.call("ingestion", "POST", self.ingest + "/batch", [
            {"object_id": good, "amount": 1, "currency": self.prefix},
            {"object_id": bad, "amount": "invalid-number", "currency": self.prefix}], 422, key=self.prefix + "-atomic")
        self.call("ingestion", "GET", self.records + "/" + good, expected=404)
        self.call("decision", "POST", self.root + "/decisions", {
            "scenario_id": self.fixtures["scenario_id"], "object_type": self.object,
            "object_id": self.prefix + "-invalid", "fields": {"amount": "invalid-number", "currency": self.prefix}}, 422)
        foreign = self.call("model", "POST", "/v1/tenants", {
            "name": "Integration isolation", "external_key": self.prefix + "-foreign"}, 201)["tenant"]["id"]
        self.call("model", "POST", f"/v1/tenants/{foreign}/provision", {})
        self.fixtures["foreign_tenant_id"] = foreign
        self.call("ingestion", "GET", self.records.replace(self.tenant, foreign) + "/" + self.record, expected=403)
        self.call("decision", "GET", f"/v1/tenants/{foreign}/scenarios/{self.fixtures['scenario_id']}", expected=404)

    def run(self):
        setup_ok = self.test("published-model-and-rule-setup", self.setup)
        if setup_ok:
            ingest_ok = self.test("ingestion-readback-replay-and-conflict", self.ingest_replay)
            if ingest_ok:
                self.test("synchronous-decision-from-stored-record", self.stored_decision)
                self.test("async-scheduled-workers-and-replay", self.workers)
                self.test("ingestion-update-visible-to-next-decision", self.mutation_visibility)
                self.test("batch-rollback-and-tenant-isolation", self.rollback_and_tenant_isolation)
        if self.published:
            self.test("unpublish-owned-scenario", lambda: self.call("decision", "POST", self.parent + "/publications", {
                "action": "unpublish", "iteration_id": self.fixtures["iteration_id"]}))
        report = {"passed": len(self.cases) == 7 and all(c["passed"] for c in self.cases),
                  "cases": self.cases, "fixtures": self.fixtures,
                  "limitations": ["Uses the existing configured ingestion tenant/table; fixtures retained",
                                  "Tests local providers; no production authentication or throughput claim"]}
        (self.output / "report.json").write_text(json.dumps(report, indent=2) + "\n")
        return 0 if report["passed"] else 1


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    sys.exit(Pipeline(args.output).run())
