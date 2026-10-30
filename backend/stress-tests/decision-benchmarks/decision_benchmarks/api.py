"""Owner-API setup and exact response contracts for a disposable benchmark tenant."""
from __future__ import annotations

import asyncio
import math
import time
import uuid
from pathlib import Path
from typing import Any

from production_replay.api_client import ServiceClients
from production_replay.benchmark_reporting import write_json_atomic
from .model import (AGGREGATES, HISTORY_FIELDS, TRANSACTION_FIELDS, Experiment, c,
                    digest, expected, family, formula, history, outcome)


class CorrectnessError(RuntimeError):
    pass


def require(condition: bool, category: str) -> None:
    if not condition:
        raise CorrectnessError(category)


def aggregate_evidence(node: dict) -> list[Any]:
    if node.get("skipped"):
        return []
    values = [node.get("return_value")] if node.get("function", "").lower() == "aggregator" else []
    for child in [*node.get("children", []), *node.get("named_children", {}).values()]:
        values.extend(aggregate_evidence(child))
    return values


def verify(exp: Experiment, fixture: dict, fields: dict, response: dict, status: int,
           aggregates: dict, expected_status: int = 200) -> list[str]:
    require(status == expected_status, f"status_expected_{expected_status}_actual_{status}")
    if status == 422:
        require(isinstance(response.get("error"), dict) and
                response["error"].get("category") == "payload_validation_failed", "wrong_rejection_category")
        require(not response.get("result"), "rejected_request_has_result")
        return []
    require(response.get("deferred") is not True, "unexpected_async_acceptance")
    result = response.get("result")
    require(isinstance(result, dict), "missing_result")
    require(isinstance(result.get("model_revision"), str) and bool(result["model_revision"]), "missing_model_revision")
    if exp.route == "all":
        require(result.get("object_id") == fields["object_id"], "wrong_object")
        items = result.get("results")
    else:
        items = [result]
    require(isinstance(items, list) and len(items) == exp.scenarios, "scenario_count")
    scenarios = {x["id"]: x for x in fixture["scenarios"] if x["triggered"]}
    expected_misses = sum(not x["triggered"] for x in fixture["scenarios"])
    misses, seen, ids = 0, set(), []
    for item in items:
        require(type(item.get("triggered")) is bool, "invalid_triggered")
        if not item["triggered"]:
            require(not item.get("decision") and not item.get("rule_executions"), "untriggered_writes")
            misses += 1
            continue
        decision = item.get("decision", {})
        sid = decision.get("scenario_id")
        require(sid in scenarios and sid not in seen, "unexpected_duplicate_scenario")
        seen.add(sid)
        scenario = scenarios[sid]
        require(decision.get("tenant_id") == fixture["tenant_id"], "tenant_leak")
        require(decision.get("object_id") == fields["object_id"] and decision.get("object_type") == "transactions", "object_identity")
        require(decision.get("scenario_iteration_id") == scenario["iteration_id"], "iteration_identity")
        require(decision.get("triggered") is True, "decision_triggered")
        require(isinstance(decision.get("id"), str) and bool(decision["id"]), "decision_id")
        rows = item.get("rule_executions")
        require(isinstance(rows, list) and len(rows) == exp.rules, "rule_count")
        by_id = {x["id"]: x for x in scenario["rules"]}
        seen_rules, score = set(), 0
        for row in rows:
            rid = row.get("rule_id")
            require(rid in by_id and rid not in seen_rules, "unexpected_duplicate_rule")
            seen_rules.add(rid)
            rule = by_id[rid]
            matched, aggregate = expected(exp, rule["index"], fields, aggregates)
            score += int(matched)
            require(row.get("decision_id") == decision["id"], "rule_decision_identity")
            require(row.get("rule_name") == rule["name"] and row.get("score_modifier") == 1, "rule_contract")
            require(row.get("outcome") == ("hit" if matched else "no_hit"), "incorrect_match" if not matched else "missed_match")
            evidence = row.get("evaluation")
            require(isinstance(evidence, dict) and evidence.get("return_value") is matched, "rule_evidence")
            if family(exp, rule["index"]) in AGGREGATES:
                values = aggregate_evidence(evidence)
                skipped = (exp.predicate_position == "last" and exp.predicates > 1 and
                           ((exp.combination == "and" and fields["guard"] is False) or
                            (exp.combination == "or" and fields["guard"] is True)))
                require(bool(values) != skipped, "aggregate_short_circuit_evidence")
                for val in values:
                    require(val is None if aggregate is None else type(val) in (int, float) and
                            math.isclose(val, aggregate, rel_tol=1e-12, abs_tol=1e-9), "aggregate_value")
        require(decision.get("score") == score and decision.get("outcome") == outcome(score), "score_or_outcome")
        ids.append(decision["id"])
    require(misses == expected_misses and seen == set(scenarios), "scenario_selection")
    require(len(ids) == len(set(ids)), "duplicate_decision_id")
    return ids


async def setup(clients: ServiceClients, exp: Experiment, output: Path, token_number: int) -> dict:
    """Fresh tenant only; journal each mutation. Failure retains owned fixtures for diagnosis."""
    state = {"status": "preparing", "tenant_id": None, "scenarios": [], "history_seeded": 0}
    path = output / f"setup-{token_number}.json"
    def save():
        write_json_atomic(path, state)
    save()
    async def request(client, method, route, status, **kw):
        return await clients.request(client, method, route, status, **kw)
    dm, de = clients.data_model, clients.decision_engine
    try:
        tenant = (await request(dm, "POST", "/v1/tenants", 201,
            json={"name": f"decision-benchmark-{uuid.uuid4().hex}", "external_key": uuid.uuid4().hex}))["tenant"]
        tid = state["tenant_id"] = tenant["id"]
        save()
        await request(dm, "POST", f"/v1/tenants/{tid}/provision", 200)
        tables = [("transactions", TRANSACTION_FIELDS)]
        if exp.history_rows or exp.ingestion_rate or any(x in AGGREGATES for x in exp.families):
            tables.append(("history", HISTORY_FIELDS))
        for name, fields in tables:
            table = (await request(dm, "POST", f"/v1/tenants/{tid}/tables", 201,
                json={"name": name, "alias": name.title(), "description": "Synthetic decision benchmark", "semantic_type": "entity"}))["table"]
            for fname, kind, nullable in fields:
                await request(dm, "POST", f"/v1/tables/{table['id']}/fields", 201,
                    json={"name": fname, "data_type": kind, "nullable": nullable, "is_unique": False})
        state["data_model"] = await request(dm, "GET", f"/v1/tenants/{tid}/data-model", 200)
        batch = []
        for record in history(exp):
            batch.append(record)
            if len(batch) == 500:
                await clients.ingest_batch(tid, "history", batch, digest([tid, state["history_seeded"]]), max_attempts=1)
                state["history_seeded"] += len(batch)
                batch = []
                save()
        if batch:
            await clients.ingest_batch(tid, "history", batch, digest([tid, state["history_seeded"]]), max_attempts=1)
            state["history_seeded"] += len(batch)
        for index in range(exp.scenarios):
            scenario = (await request(de, "POST", f"/v1/tenants/{tid}/scenarios", 201,
                json={"name": f"benchmark-{index}", "trigger_object_type": "transactions"}))["scenario"]
            sid = scenario["id"]
            base = f"/v1/tenants/{tid}/scenarios/{sid}"
            iteration = (await request(de, "POST", base + "/iterations", 201))["iteration"]
            ibase = base + f"/iterations/{iteration['id']}"
            item = {"id": sid, "iteration_id": iteration["id"], "rules": [],
                    "triggered": index < math.ceil(exp.scenarios * exp.trigger_ratio)}
            state["scenarios"].append(item)
            save()
            await request(de, "PUT", ibase, 200, json={"trigger_formula": c(item["triggered"]),
                "score_review_threshold": 1, "score_block_and_review_threshold": 2, "score_decline_threshold": 3, "schedule": ""})
            for ridx in range(exp.rules):
                name = f"rule-{ridx}"
                ast = formula(exp, ridx)
                rule = (await request(de, "POST", ibase + "/rules", 201, json={
                    "name": name, "description": "Synthetic benchmark", "formula": ast, "score_modifier": 1,
                    "display_order": ridx, "rule_group": "benchmark", "stable_rule_id": str(uuid.uuid4())}))["rule"]
                item["rules"].append({"id": rule["id"], "index": ridx, "name": name, "formula": ast})
            validation = await request(de, "POST", ibase + "/validate", 200)
            require(validation.get("validation", {}).get("valid") is True, "authoring_validation")
            await request(de, "POST", ibase + "/commit", 200)
            prep = base + "/publications/preparation"
            await request(de, "POST", prep, 202, json={"iteration_id": iteration["id"]})
            deadline = time.monotonic() + 120
            while True:
                response = await request(de, "GET", prep, 200, params={"iteration_id": iteration["id"]})
                prepared = response.get("preparation", response)
                if prepared.get("preparation_finished") is True and prepared.get("preparation_required") is not True:
                    break
                if time.monotonic() >= deadline:
                    raise TimeoutError("index_preparation_timeout")
                await asyncio.sleep(0.25)
            await request(de, "POST", base + "/publications", 200,
                          json={"action": "publish", "iteration_id": iteration["id"]})
            save()
        state["status"] = "ready"
        return state
    except BaseException as exc:
        state.update(status="failed", error_type=type(exc).__name__)
        raise
    finally:
        save()


def decision_path(exp: Experiment, fixture: dict) -> str:
    base = f"/v1/tenants/{fixture['tenant_id']}"
    return base + ("/decisions/all" if exp.route == "all" else f"/scenarios/{fixture['scenarios'][0]['id']}/evaluate")


async def evaluate(clients: ServiceClients, exp: Experiment, fixture: dict, fields: dict):
    # Direct HTTP client preserves status and response duration; no retries.
    response = await clients.decision_engine.post(decision_path(exp, fixture), json={
        "object_id": fields["object_id"], "object_type": "transactions", "fields": fields})
    body = response.json()
    require(isinstance(body, dict), "nonobject_response")
    return body, response.status_code, len(response.content)


async def reconcile(clients: ServiceClients, exp: Experiment, fixture: dict, fields: dict,
                    response: dict, ids: list[str], aggregates: dict) -> None:
    for did in ids:
        stored = await clients.request(clients.decision_engine, "GET",
            f"/v1/tenants/{fixture['tenant_id']}/decisions/{did}", 200)
        expected_item = response["result"] if exp.route == "single" else next(
            x for x in response["result"]["results"] if x.get("decision", {}).get("id") == did)
        decision = stored.get("decision", {})
        require(all(decision.get(key) == value for key, value in expected_item["decision"].items()), "persisted_decision")
        require(sorted(stored.get("rule_executions", []), key=lambda x: x["rule_id"]) ==
                sorted(expected_item["rule_executions"], key=lambda x: x["rule_id"]), "persisted_rules")
