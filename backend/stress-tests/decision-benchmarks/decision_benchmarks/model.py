"""Typed experiment contracts and synthetic fixtures; never execute arbitrary ASTs locally."""
from __future__ import annotations

import hashlib
import json
import math
from dataclasses import asdict, dataclass, field
from datetime import datetime, timedelta, timezone
from typing import Any, Iterator

VERSION = "decision-benchmark/v1"
SCALARS = ("numeric", "string", "membership", "boolean", "nonnull", "date", "field")
AGGREGATES = ("count", "sum", "avg", "count_distinct", "min", "max")
FAMILIES = (*SCALARS, *AGGREGATES)
ANCHOR = datetime(2026, 1, 15, 12, tzinfo=timezone.utc)


def digest(value: Any) -> str:
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False).encode()).hexdigest()


@dataclass(frozen=True)
class Experiment:
    name: str = "single-numeric"
    scenarios: int = 1
    rules: int = 1
    families: tuple[str, ...] = ("numeric",)
    operator: str = "gt"
    combination: str = "none"
    predicates: int = 1
    depth: int = 1
    predicate_position: str = "first"
    distinct: bool = False
    rule_mix: str = "same"
    trigger_ratio: float = 1.0
    match_ratio: float = 0.5
    seed: int = 20261028
    history_rows: int = 0
    history_keys: int = 10
    padding_bytes: int = 0
    tenants: int = 1
    route: str = "single"
    mode: str = "fixed"
    rate: float = 1.0
    duration: float = 5.0
    concurrency: int = 1
    warmup: float = 2.0
    repeats: int = 3
    timeout: float = 30.0
    max_lag: float = 0.25
    read_mode: str = "ingestion_http"
    ingestion_rate: float = 0.0
    ingestion_concurrency: int = 1

    @classmethod
    def parse(cls, value: dict) -> "Experiment":
        unknown = set(value) - set(cls.__dataclass_fields__)
        if unknown:
            raise ValueError(f"unknown experiment fields: {sorted(unknown)}")
        value = dict(value)
        if "families" in value:
            if not isinstance(value["families"], list):
                raise ValueError("families must be an array")
            value["families"] = tuple(value["families"])
        result = cls(**value)
        result.validate()
        return result

    def validate(self) -> None:
        import re
        if not isinstance(self.name, str) or not re.fullmatch(r"[a-zA-Z0-9_-]{1,80}", self.name):
            raise ValueError("invalid experiment name")
        for key, low, high in (("scenarios", 1, 50), ("rules", 1, 100), ("predicates", 1, 100),
                               ("depth", 1, 8), ("history_rows", 0, 5_000_000), ("history_keys", 1, 10000),
                               ("padding_bytes", 0, 1_000_000), ("tenants", 1, 10), ("concurrency", 1, 256),
                               ("repeats", 1, 20), ("seed", 0, 2**63-1)):
            val = getattr(self, key)
            if type(val) is not int or not low <= val <= high:
                raise ValueError(f"{key} must be an integer in [{low},{high}]")
        for key, low, high in (("rate", 0.001, 100000), ("duration", 0.01, 7200), ("warmup", 0, 600),
                               ("timeout", 0.01, 300), ("max_lag", 0.001, 30),
                               ("ingestion_rate", 0, 1000),
                               ("trigger_ratio", 0, 1), ("match_ratio", 0, 1)):
            val = getattr(self, key)
            if type(val) not in (int, float) or not math.isfinite(val) or not low <= val <= high:
                raise ValueError(f"invalid {key}")
        if not self.families or any(x not in FAMILIES for x in self.families):
            raise ValueError("unsupported family (disabled helpers are not successful workloads)")
        for key, allowed in (("operator", ("gt", "gte", "lt", "lte", "eq", "neq")),
                             ("combination", ("none", "and", "or", "not")),
                             ("rule_mix", ("same", "alternating")), ("route", ("single", "all")),
                             ("mode", ("fixed", "closed")), ("read_mode", ("ingestion_http", "direct_db"))):
            if getattr(self, key) not in allowed:
                raise ValueError(f"invalid {key}")
        if self.predicate_position not in ("first", "last"):
            raise ValueError("invalid predicate_position")
        if type(self.ingestion_concurrency) is not int or not 1 <= self.ingestion_concurrency <= 64:
            raise ValueError("invalid ingestion_concurrency")
        if type(self.distinct) is not bool:
            raise ValueError("distinct must be boolean")
        if self.route == "single" and self.scenarios != 1:
            raise ValueError("multiple scenarios require all route")
        if self.combination == "none" and (self.predicates != 1 or self.depth != 1):
            raise ValueError("predicates/depth require a logical combination")
        if self.combination == "not" and self.predicates != 1:
            raise ValueError("not has exactly one predicate")
        if (self.duration + self.warmup) * self.rate * self.repeats > 1_000_000:
            raise ValueError("schedule exceeds one million offered operations per experiment")

    def manifest(self) -> dict:
        return asdict(self)


def c(value: Any) -> dict:
    return {"constant": value}


def f(name: str) -> dict:
    return {"function": "field_ref", "named_children": {"field": c(name)}}


def op(name: str, *children: dict) -> dict:
    return {"function": name, "children": list(children)}


def timestamp(value: datetime) -> str:
    return value.isoformat().replace("+00:00", "Z")


def aggregate_node(kind: str) -> dict:
    field_name = {"count": "sequence", "count_distinct": "destination"}.get(kind, "amount")
    def condition(name, operator, value):
        return {"function": "Filter", "named_children": {
            "tableName": c("history"), "fieldName": c(name), "operator": c(operator), "value": value}}
    return {"function": "Aggregator", "named_children": {
        "tableName": c("history"), "fieldName": c(field_name), "aggregator": c(kind.upper()),
        "filters": op("List", condition("merchant_id", "=", f("merchant_id")),
                      condition("timestamp", ">=", op("ParseTime", c(timestamp(ANCHOR-timedelta(minutes=5))))),
                      condition("timestamp", "<", op("ParseTime", c(timestamp(ANCHOR)))))}}


def family(exp: Experiment, rule: int) -> str:
    return exp.families[rule % len(exp.families)]


def formula(exp: Experiment, rule: int) -> dict:
    kind = family(exp, rule)
    threshold = 3000 + (rule if exp.distinct else 0)
    expressions = {
        "numeric": lambda: op(exp.operator, f("amount"), c(threshold)),
        "string": lambda: op("eq", f("channel"), c("mobile")),
        "membership": lambda: op("in", f("country"), c(["GH", "NG", "KE"])),
        "boolean": lambda: op("eq", f("is_verified"), c(False)),
        "nonnull": lambda: op("not", op("is_null", f("merchant_id"))),
        "date": lambda: op("and", op("gte", f("timestamp"), op("ParseTime", c("2026-01-01T00:00:00Z"))),
                           op("lt", f("timestamp"), op("ParseTime", c("2026-02-01T00:00:00Z")))),
        "field": lambda: op("gt", f("amount"), f("limit_amount")),
    }
    base = expressions[kind]() if kind in expressions else op("gt", aggregate_node(kind), c(0))
    if exp.rule_mix == "alternating" and rule % 2:
        base = op("not", base)
    if exp.combination in ("and", "or"):
        # Independent boolean payload guards allow early/late short-circuit probes.
        guards = [op("eq", f("guard"), c(True)) for _ in range(exp.predicates-1)]
        base = op(exp.combination, *([base, *guards] if exp.predicate_position == "first" else [*guards, base]))
        for _ in range(exp.depth-1):
            base = op(exp.combination, base, c(exp.combination == "and"))
    elif exp.combination == "not":
        for _ in range(exp.depth):
            base = op("not", base)
    return base


def payload(exp: Experiment, index: int, prefix: str, *, match: bool | None = None) -> dict:
    # Index-derived randomness is independent of request completion/concurrency order.
    bucket = int(hashlib.sha256(f"{exp.seed}:{index}".encode()).hexdigest()[:16], 16) / 2**64
    yes = bucket < exp.match_ratio if match is None else match
    return {"object_id": f"{prefix}-{index}", "amount": 4000.0 if yes else 2000.0,
            "limit_amount": 3000.0, "channel": "mobile" if yes else "web",
            "country": "GH" if yes else "US", "is_verified": not yes,
            "timestamp": timestamp(ANCHOR) if yes else "2025-12-31T23:59:59Z",
            "merchant_id": "m0" if yes else None, "note": None,
            "guard": yes, "padding": "x" * exp.padding_bytes}


def history(exp: Experiment) -> Iterator[dict]:
    for index in range(exp.history_rows):
        yield {"object_id": f"history-{exp.seed}-{index}", "sequence": index,
               "amount": None if index % 7 == 0 else float(index % 97 + 1),
               "merchant_id": f"m{index % exp.history_keys}",
               "destination": None if index % 11 == 0 else f"d{index % 13}",
               "timestamp": timestamp(ANCHOR + timedelta(seconds=(-301, -300, -1, 0, 1)[(index // exp.history_keys) % 5]))}


def history_oracle(exp: Experiment) -> dict[str, dict]:
    # Streaming independent reductions, not the production AST or SQL builder.
    groups: dict[str, dict] = {}
    for row in history(exp):
        when = datetime.fromisoformat(row["timestamp"].replace("Z", "+00:00"))
        if not ANCHOR-timedelta(minutes=5) <= when < ANCHOR:
            continue
        group = groups.setdefault(row["merchant_id"], {"count": 0, "sum": 0, "n": 0,
                                                       "min": None, "max": None, "distinct": set()})
        group["count"] += 1
        value = row["amount"]
        if value is not None:
            group["n"] += 1
            group["sum"] += value
            group["min"] = value if group["min"] is None else min(value, group["min"])
            group["max"] = value if group["max"] is None else max(value, group["max"])
        if row["destination"] is not None:
            group["distinct"].add(row["destination"])
    return {key: {"count": x["count"], "sum": x["sum"], "avg": x["sum"]/x["n"] if x["n"] else None,
                  "min": x["min"], "max": x["max"], "count_distinct": len(x["distinct"])}
            for key, x in groups.items()}


def expected(exp: Experiment, rule: int, fields: dict, aggregates: dict) -> tuple[bool, Any]:
    kind = family(exp, rule)
    value = None
    if kind == "numeric":
        left, right = float(fields["amount"]), 3000 + (rule if exp.distinct else 0)
        result = {"gt": left > right, "gte": left >= right, "lt": left < right,
                  "lte": left <= right, "eq": left == right, "neq": left != right}[exp.operator]
    elif kind == "string":
        result = fields["channel"] == "mobile"
    elif kind == "membership":
        result = fields["country"] in {"GH", "NG", "KE"}
    elif kind == "boolean":
        result = str(fields["is_verified"]).strip().lower() == "false"
    elif kind == "nonnull":
        result = fields.get("merchant_id") is not None
    elif kind == "date":
        when = datetime.fromisoformat(fields["timestamp"].replace("Z", "+00:00"))
        result = datetime(2026, 1, 1, tzinfo=timezone.utc) <= when < datetime(2026, 2, 1, tzinfo=timezone.utc)
    elif kind == "field":
        result = float(fields["amount"]) > float(fields["limit_amount"])
    else:
        value = aggregates.get(fields.get("merchant_id"), {}).get(kind, 0 if kind in ("count", "sum", "count_distinct") else None)
        result = value is not None and value > 0
    if exp.rule_mix == "alternating" and rule % 2:
        result = not result
    if exp.combination == "and" and exp.predicates > 1:
        result = result and fields["guard"]
    elif exp.combination == "or" and exp.predicates > 1:
        result = result or fields["guard"]
    elif exp.combination == "not" and exp.depth % 2:
        result = not result
    return bool(result), value


def outcome(score: int) -> str:
    return "decline" if score >= 3 else "block_and_review" if score >= 2 else "review" if score >= 1 else "approve"


TRANSACTION_FIELDS = (("amount", "float", False), ("limit_amount", "float", False),
    ("channel", "string", False), ("country", "string", False), ("is_verified", "bool", False),
    ("timestamp", "timestamp", False), ("merchant_id", "string", True), ("note", "string", True),
    ("guard", "bool", False), ("padding", "string", False))
HISTORY_FIELDS = (("sequence", "int", False), ("amount", "float", True),
    ("merchant_id", "string", False), ("destination", "string", True), ("timestamp", "timestamp", False))


def reference_cases(exp: Experiment) -> Iterator[tuple[str, dict, int]]:
    base = payload(exp, 0, "qualification", match=True)
    yield "match", dict(base), 200
    yield "nonmatch", payload(exp, 1, "qualification", match=False), 200
    for field_name, value in (("amount", 3000), ("amount", "3001"), ("channel", "Mobile"),
            ("country", "KE"), ("country", "gh"), ("is_verified", " false "),
            ("merchant_id", ""), ("merchant_id", None), ("timestamp", "2026-01-01T00:00:00Z"),
            ("timestamp", "2026-02-01T00:00:00Z")):
        yield f"boundary-{field_name}-{value}", {**base, field_name: value}, 200
    missing_nullable = dict(base)
    missing_nullable.pop("merchant_id")
    yield "missing-nullable", missing_nullable, 200
    for name, _kind, nullable in TRANSACTION_FIELDS:
        if nullable:
            continue
        missing = dict(base)
        missing.pop(name)
        yield f"missing-{name}", missing, 422
        yield f"null-{name}", {**base, name: None}, 422
    for name, value in (("amount", "bad"), ("amount", []), ("channel", 3), ("is_verified", 0),
                        ("timestamp", "not-a-date"), ("merchant_id", {}), ("unexpected", None)):
        yield f"malformed-{name}-{type(value).__name__}", {**base, name: value}, 422
