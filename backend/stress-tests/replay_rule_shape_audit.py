from __future__ import annotations

import argparse
import json
import re
from collections import Counter
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from production_replay.manifest import ReferenceSources, ReplayManifest, TransactionStream
from production_replay.scenarios import RuleDef, build_portable_scenarios


RULE_NAME_PATTERN = re.compile(r'evaluate rule \\"([^"]+)\\"|evaluate rule "([^"]+)"')
SUPPORTED_AGGREGATES = {"COUNT", "COUNT_DISTINCT", "SUM", "AVG", "MIN", "MAX"}
SUPPORTED_FILTER_OPERATORS = {
    "=",
    "eq",
    "!=",
    "neq",
    ">",
    "gt",
    ">=",
    "gte",
    "<",
    "lt",
    "<=",
    "lte",
    "isinlist",
    "stringstartswith",
    "stringendswith",
}
SUPPORTED_FILTER_FUNCTIONS = {"filter", "list", "and", "or", "not"}


@dataclass(frozen=True)
class RuleClassification:
    name: str
    category: str
    functions: list[str]
    uses_aggregator: bool
    uses_related_count: bool
    uses_related_records: bool
    uses_related_field: bool
    aggregate_pushdown_supported: bool | None
    aggregate_pushdown_reasons: list[str]


def load_json(path_value: str) -> dict[str, Any]:
    path = Path(path_value)
    if not path.exists():
        raise SystemExit(f"path does not exist: {path}")
    return json.loads(path.read_text(encoding="utf-8"))


def canonical_function_name(node: dict[str, Any]) -> str:
    function = node.get("function")
    if not isinstance(function, str):
        return ""
    return function.strip().lower()


def walk_nodes(node: Any) -> list[dict[str, Any]]:
    if not isinstance(node, dict):
        return []
    nodes = [node]
    for child in node.get("children", []) or []:
        nodes.extend(walk_nodes(child))
    named_children = node.get("named_children", {}) or {}
    if isinstance(named_children, dict):
        for child in named_children.values():
            nodes.extend(walk_nodes(child))
    return nodes


def constant_value(node: dict[str, Any]) -> Any:
    if not isinstance(node, dict):
        return None
    function = node.get("function")
    if function not in ("", None, "constant", "Constant"):
        return None
    return node.get("constant")


def classify_filter_node(node: dict[str, Any]) -> tuple[bool, list[str]]:
    function = canonical_function_name(node)
    if function not in SUPPORTED_FILTER_FUNCTIONS:
        return False, [f'filter function "{node.get("function")}" is not supported for pushdown']
    if function == "filter":
        operator_node = (node.get("named_children") or {}).get("operator")
        operator = constant_value(operator_node)
        if not isinstance(operator, str) or operator.strip().lower() not in SUPPORTED_FILTER_OPERATORS:
            return False, [f'filter operator "{operator}" is not supported for pushdown']
        if operator.strip().lower() == "isinlist":
            value = constant_value(((node.get("named_children") or {}).get("value")))
            if not isinstance(value, list):
                return False, ['filter operator "isinlist" requires a constant list value for pushdown']
        return True, []
    if function == "not":
        children = node.get("children", []) or []
        if len(children) != 1:
            return False, ['filter function "not" requires exactly one child for pushdown']
    reasons: list[str] = []
    for child in node.get("children", []) or []:
        supported, child_reasons = classify_filter_node(child)
        if not supported:
            reasons.extend(child_reasons)
    return len(reasons) == 0, reasons


def classify_aggregator_node(node: dict[str, Any]) -> tuple[bool, list[str]]:
    named_children = node.get("named_children", {}) or {}
    aggregator_name = constant_value(named_children.get("aggregator"))
    if not isinstance(aggregator_name, str) or aggregator_name.strip().upper() not in SUPPORTED_AGGREGATES:
        return False, [f'aggregate "{aggregator_name}" is not supported for pushdown']
    filters_node = named_children.get("filters")
    if not isinstance(filters_node, dict):
        return True, []
    return classify_filter_node(filters_node)


def classify_rule(rule: RuleDef) -> RuleClassification:
    nodes = walk_nodes(rule.formula)
    functions = sorted({node.get("function", "") for node in nodes if isinstance(node.get("function"), str) and node.get("function", "")})
    canonical_functions = {name.strip().lower() for name in functions}
    uses_aggregator = "aggregator" in canonical_functions
    uses_related_count = "related_count" in canonical_functions
    uses_related_records = "related_records" in canonical_functions
    uses_related_field = "related_field" in canonical_functions

    aggregate_pushdown_supported: bool | None = None
    aggregate_pushdown_reasons: list[str] = []
    if uses_aggregator:
        aggregate_pushdown_supported = True
        for node in nodes:
            if canonical_function_name(node) != "aggregator":
                continue
            supported, reasons = classify_aggregator_node(node)
            if not supported:
                aggregate_pushdown_supported = False
                aggregate_pushdown_reasons.extend(reasons)

    if uses_related_count or uses_related_records:
        category = "broad_list_read_helper"
    elif uses_aggregator and aggregate_pushdown_supported:
        category = "aggregate_pushdown_supported_shape"
    elif uses_aggregator:
        category = "aggregate_pushdown_unsupported_shape"
    elif uses_related_field or "database_access" in canonical_functions:
        category = "explicit_record_lookup"
    else:
        category = "no_broad_read_helper_detected"

    return RuleClassification(
        name=rule.name,
        category=category,
        functions=functions,
        uses_aggregator=uses_aggregator,
        uses_related_count=uses_related_count,
        uses_related_records=uses_related_records,
        uses_related_field=uses_related_field,
        aggregate_pushdown_supported=aggregate_pushdown_supported,
        aggregate_pushdown_reasons=sorted(set(aggregate_pushdown_reasons)),
    )


def build_production_replay_rule_catalog() -> dict[str, RuleDef]:
    manifest = ReplayManifest(
        path=Path("stress-tests/production_replay/manifests/fraud-data.json"),
        version=1,
        timezone="UTC",
        reference_data=ReferenceSources(
            merchant_globs=("merchant.csv",),
            merchant_product_globs=("merchant_product.csv",),
            staff_csv="staff.csv",
            merchant_watchlist_xlsx="merchant_watchlist.xlsx",
        ),
        transaction_streams=(
            TransactionStream("wallet", "uniwallet", ("wallet.csv",), "wallet", "incoming", "wallet_transfer", "UTC"),
            TransactionStream("card", "uniwallet", ("card.csv",), "card", "outgoing", "card_payment", "UTC"),
            TransactionStream("bank", "uniwallet", ("bank.csv",), "bank", "outgoing", "bank_transfer", "UTC"),
            TransactionStream("cashout", "uniwallet", ("cashout.csv",), "wallet", "outgoing", "cash_out", "UTC"),
            TransactionStream("cash", "uniwallet", ("cash.csv",), "cash", "incoming", "cash_deposit", "UTC"),
            TransactionStream("electronic", "uniwallet", ("electronic.csv",), "electronic_transfer", "outgoing", "electronic_transfer", "UTC"),
        ),
    )
    catalog: dict[str, RuleDef] = {}
    for scenario in build_portable_scenarios(manifest):
        for rule in scenario.rules:
            catalog[rule.name] = rule
    return catalog


def load_rule_catalog_from_json(path_value: str) -> dict[str, RuleDef]:
    payload = load_json(path_value)
    setup = payload.get("setup", payload)
    raw_rules = setup.get("rules", {})
    if not isinstance(raw_rules, dict):
        raise SystemExit(f"rule catalog JSON does not expose setup.rules: {path_value}")
    catalog: dict[str, RuleDef] = {}
    for scenario_rules in raw_rules.values():
        if not isinstance(scenario_rules, list):
            continue
        for item in scenario_rules:
            if not isinstance(item, dict):
                continue
            name = item.get("name")
            formula = item.get("formula")
            if not isinstance(name, str) or not isinstance(formula, dict):
                continue
            catalog[name] = RuleDef(
                name=name,
                group=str(item.get("group", "")),
                description=str(item.get("description", "")),
                score=int(item.get("score", 0)),
                formula=formula,
            )
    return catalog


def build_default_rule_catalog() -> dict[str, RuleDef]:
    catalog = build_production_replay_rule_catalog()
    default_demo_catalog = Path("new/backend/stress-tests/demo-runs/full-scenario-catalog-2026-07-20T141651-631508Z.json")
    if default_demo_catalog.exists():
        catalog.update(load_rule_catalog_from_json(str(default_demo_catalog)))
    return catalog


def extract_failing_rule_names(summary: dict[str, Any]) -> list[str]:
    names: list[str] = []
    for item in summary.get("sampled_errors", []):
        error_text = str(item.get("error", ""))
        match = RULE_NAME_PATTERN.search(error_text)
        if not match:
            continue
        names.append(match.group(1) or match.group(2))
    return names


def build_report(summary: dict[str, Any], rule_catalog: dict[str, RuleDef]) -> dict[str, Any]:
    extracted_names = extract_failing_rule_names(summary)
    unique_names = sorted(set(extracted_names))
    classifications: list[RuleClassification] = []
    unresolved: list[str] = []
    for name in unique_names:
        rule = rule_catalog.get(name)
        if rule is None:
            unresolved.append(name)
            continue
        classifications.append(classify_rule(rule))

    category_counts = Counter(item.category for item in classifications)
    return {
        "summary_version": 1,
        "sampled_error_count": len(summary.get("sampled_errors", [])),
        "sampled_rule_occurrence_count": len(extracted_names),
        "unique_sampled_rule_count": len(unique_names),
        "category_counts": dict(sorted(category_counts.items())),
        "rules": [
            {
                "name": item.name,
                "category": item.category,
                "functions": item.functions,
                "uses_aggregator": item.uses_aggregator,
                "uses_related_count": item.uses_related_count,
                "uses_related_records": item.uses_related_records,
                "uses_related_field": item.uses_related_field,
                "aggregate_pushdown_supported": item.aggregate_pushdown_supported,
                "aggregate_pushdown_reasons": item.aggregate_pushdown_reasons,
            }
            for item in sorted(classifications, key=lambda entry: entry.name)
        ],
        "unresolved_rule_names": unresolved,
        "inference": {
            "sampled_rules_are_aggregate_pushdown_candidates": bool(classifications) and all(
                item.category == "aggregate_pushdown_supported_shape" for item in classifications
            ),
            "sampled_rules_include_broad_list_helpers": any(
                item.category == "broad_list_read_helper" for item in classifications
            ),
        },
    }


def print_report(report: dict[str, Any]) -> None:
    print("")
    print("Replay Rule Shape Audit")
    print(f"  sampled errors:       {report['sampled_error_count']}")
    print(f"  sampled rule hits:    {report['sampled_rule_occurrence_count']}")
    print(f"  unique sampled rules: {report['unique_sampled_rule_count']}")
    print("")
    print("Categories")
    for name, count in sorted(report["category_counts"].items()):
        print(f"  {name}: {count}")
    if report["unresolved_rule_names"]:
        print("")
        print("Unresolved Rule Names")
        for name in report["unresolved_rule_names"]:
            print(f"  {name}")
    if report["rules"]:
        print("")
        print("Resolved Rules")
        for item in report["rules"]:
            reasons = item["aggregate_pushdown_reasons"]
            suffix = f" ({'; '.join(reasons)})" if reasons else ""
            print(f"  {item['name']}: {item['category']}{suffix}")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Classify sampled failing replay rules as aggregate pushdown candidates or broad-read helpers."
    )
    parser.add_argument("--summary", required=True, help="Replay summary.json path")
    parser.add_argument(
        "--catalog-json",
        action="append",
        default=[],
        help="Optional rule catalog JSON exposing setup.rules. May be passed multiple times.",
    )
    parser.add_argument("--output", help="Optional output JSON path")
    return parser


def main() -> None:
    args = build_parser().parse_args()
    summary = load_json(args.summary)
    rule_catalog = build_default_rule_catalog()
    for path_value in args.catalog_json:
        rule_catalog.update(load_rule_catalog_from_json(path_value))
    report = build_report(summary, rule_catalog)
    if args.output:
        output_path = Path(args.output)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print_report(report)


if __name__ == "__main__":
    main()
