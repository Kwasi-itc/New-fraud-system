"""Versioned, sequential experiment campaigns with input and pool-budget preflight."""
from __future__ import annotations

import argparse
import asyncio
import json
import math
from pathlib import Path
from tempfile import TemporaryDirectory
from typing import Any
from urllib.parse import urlsplit

from production_replay.benchmark_reporting import write_json_atomic
from production_replay.identity_ledger import IdentityLedger
from .input_source import inspect_source, strict_json_loads
from .queue_campaign import async_main as run_experiment, build_parser, parse_stage
from .report import write_report


def exact_keys(value: Any, allowed: set[str], required: set[str]) -> None:
    if not isinstance(value, dict) or set(value) - allowed or required - set(value):
        raise ValueError("unknown_or_missing_configuration_keys")


def load_campaign(path: Path) -> dict[str, Any]:
    if path.stat().st_size > 1_048_576:
        raise ValueError("campaign_configuration_too_large")
    config = strict_json_loads(path.read_text(encoding="utf-8"))
    exact_keys(config, {"schema_version", "mode", "defaults", "runs", "continue_on_failure", "connection_budget"},
               {"schema_version", "mode", "runs"})
    if type(config["schema_version"]) is not int or config["schema_version"] != 1 or config["mode"] != "discovery":
        raise ValueError("only_version_1_discovery_campaigns_supported")
    if type(config.get("continue_on_failure", False)) is not bool:
        raise ValueError("continue_on_failure_must_be_boolean")
    runs = config["runs"]
    if not isinstance(runs, list) or not 1 <= len(runs) <= 100:
        raise ValueError("campaign_requires_1_to_100_runs")
    defaults = config.get("defaults", {})
    # Use the actual runner option inventory: no shell command interpolation.
    actions = {a.dest: a for a in build_parser()._actions if a.option_strings
               and a.dest not in {"help", "output", "dry_run"}}
    if not isinstance(defaults, dict) or set(defaults) - set(actions):
        raise ValueError("unknown_default_options")
    names = set()
    resolved = []
    for run in runs:
        exact_keys(run, {"name", "options", "profile", "hypothesis"}, {"name", "options"})
        name = run["name"]
        if not isinstance(name, str):
            raise ValueError("invalid_run_name")
        parse_stage(f"{name}:1:1")
        if name in names:
            raise ValueError("duplicate_run_name")
        names.add(name)
        if any(not isinstance(run.get(key, ""), str) for key in ("profile", "hypothesis")):
            raise ValueError("profile_and_hypothesis_must_be_strings")
        if not isinstance(run["options"], dict) or set(run["options"]) - set(actions):
            raise ValueError("unknown_run_options")
        options = {**defaults, **run["options"]}
        if any(key not in options for key in ("input", "tenant_id", "stage", "expected_scenarios", "deadline")):
            raise ValueError("missing_run_options")
        argv = []
        for key, value in options.items():
            action = actions[key]
            flag = action.option_strings[0]
            if isinstance(action, argparse._StoreTrueAction):
                if type(value) is not bool:
                    raise ValueError("boolean_option_required")
                if value:
                    argv.append(flag)
                continue
            values = value if isinstance(action, argparse._AppendAction) else [value]
            if not isinstance(values, list) or not values:
                raise ValueError("nonempty_option_array_required")
            for item in values:
                if type(item) not in (str, int, float) or (type(item) is float and not math.isfinite(item)):
                    raise ValueError("invalid_option_value")
                if key.endswith("_url"):
                    url = urlsplit(str(item))
                    if url.scheme not in {"http", "https"} or not url.hostname or url.username or url.password or url.query or url.fragment:
                        raise ValueError("service_url_must_not_contain_credentials_or_query")
                if key == "input":
                    item = str((path.parent / str(item)).resolve())
                argv.extend([flag, str(item)])
        parsed = build_parser().parse_args([*argv, "--output", "unused"])
        if not parsed.tenant_id.strip():
            raise ValueError("tenant_id_required")
        stages = parsed.stage
        planned = sum(s.count for s in stages)
        if planned > parsed.max_operations or sum(s.seconds for s in stages) > parsed.max_duration:
            raise ValueError("experiment_exceeds_resource_bounds")
        resolved.append({"name": name, "profile": run.get("profile", "unspecified"),
            "hypothesis": run.get("hypothesis", ""), "argv": argv,
            "input": parsed.input, "tenant": parsed.tenant_id, "planned": planned})
    budget = config.get("connection_budget")
    if budget is not None:
        exact_keys(budget, {"max_connections", "reserve", "pools"}, {"max_connections", "reserve", "pools"})
        if (type(budget["max_connections"]) is not int or budget["max_connections"] <= 0
                or type(budget["reserve"]) is not int or budget["reserve"] < 0
                or not isinstance(budget["pools"], list) or not budget["pools"]):
            raise ValueError("invalid_shared_connection_budget")
        total = budget["reserve"]
        roles = set()
        for pool in budget["pools"]:
            exact_keys(pool, {"role", "replicas", "max_connections"}, {"role", "replicas", "max_connections"})
            if (not isinstance(pool["role"], str) or not pool["role"] or pool["role"] in roles
                    or any(type(pool[k]) is not int or pool[k] <= 0 for k in ("replicas", "max_connections"))):
                raise ValueError("invalid_pool_allocation")
            roles.add(pool["role"])
            total += pool["replicas"] * pool["max_connections"]
        if total > budget["max_connections"]:
            raise ValueError("shared_database_connection_budget_exceeded")
        budget = {**budget, "declared_total": total, "effective_deployment_verified": False}
    return {"schema_version": 1, "mode": "discovery", "runs": resolved,
            "continue_on_failure": config.get("continue_on_failure", False), "connection_budget": budget}


def preflight(config: dict[str, Any]) -> list[dict[str, Any]]:
    results = []
    with TemporaryDirectory(prefix="system-campaign-") as directory:
        with IdentityLedger(Path(directory) / "identities.sqlite3") as ledger:
            for run in config["runs"]:
                evidence = inspect_source(run["input"], run["planned"], tenant=run["tenant"], ledger=ledger)
                run["input_evidence"] = evidence
                results.append({"name": run["name"], "planned": run["planned"], "input": evidence})
    return results


async def execute(config: dict[str, Any], output: Path) -> int:
    # Preflight every cohort before the first mutation. Same tenant/ID cannot be reused across runs.
    sources = await asyncio.to_thread(preflight, config)
    output.mkdir(parents=True, exist_ok=False)
    state = {"schema_version": 1, "mode": "discovery", "status": "running", "runs": [],
             "planned_runs": len(config["runs"]), "sources": sources,
             "connection_budget": config["connection_budget"], "production_qualified": False,
             "acceptance": {"evaluated": False, "passed": False}}
    write_json_atomic(output / "summary.json", state)
    try:
        for run in config["runs"]:
            # Detect changes since campaign-wide preflight before invoking a run.
            evidence = await asyncio.to_thread(inspect_source, run["input"], run["planned"])
            if evidence != run["input_evidence"]:
                raise ValueError("campaign_input_changed")
            entry = {"name": run["name"], "profile": run["profile"], "status": "running"}
            state["runs"].append(entry)
            write_json_atomic(output / "summary.json", state)
            try:
                code = await run_experiment([*run["argv"], "--output", str(output / run["name"])])
                result = json.loads((output / run["name"] / "summary.json").read_text())
                entry.update(status="completed", exit_code=code, acceptance=result["acceptance"],
                             path=result.get("path", "ingest-async"))
            except Exception as exc:
                entry.update(status="failed", error_type=type(exc).__name__,
                             acceptance={"evaluated": False, "passed": False})
                code = 1
            write_json_atomic(output / "summary.json", state)
            if code and not config["continue_on_failure"]:
                break
        complete = len(state["runs"]) == state["planned_runs"] and all(r["status"] == "completed" for r in state["runs"])
        state["status"] = "completed" if complete else "incomplete"
        state["acceptance"] = {"evaluated": complete, "passed": complete and
                               all(r["acceptance"]["passed"] for r in state["runs"])}
        return 0 if state["acceptance"]["passed"] else 2
    except BaseException as exc:
        state.update(status="interrupted_or_failed", error_type=type(exc).__name__)
        raise
    finally:
        write_json_atomic(output / "summary.json", state)
        write_report(output, state)


async def async_main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--validate-only", action="store_true")
    args = parser.parse_args(argv)
    config = load_campaign(args.config.resolve())
    if args.validate_only:
        evidence = await asyncio.to_thread(preflight, config)
        # Validate the runner's semantic options as well; --dry-run sends no requests.
        for run in config["runs"]:
            await run_experiment([*run["argv"], "--output", "unused", "--dry-run"])
        print(json.dumps({"valid": True, "runs": evidence, "requests_sent": 0}))
        return 0
    if args.output is None:
        parser.error("--output is required unless --validate-only")
    # Full option validation before executing any run.
    for run in config["runs"]:
        await run_experiment([*run["argv"], "--output", "unused", "--dry-run"])
    return await execute(config, args.output.resolve())


def main():
    try:
        raise SystemExit(asyncio.run(async_main()))
    except KeyboardInterrupt:
        raise SystemExit(130) from None
    except Exception as exc:
        print(f"Campaign failed: {type(exc).__name__}")
        raise SystemExit(1) from None


if __name__ == "__main__":
    main()
