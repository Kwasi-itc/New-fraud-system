"""Bounded-memory response reconciliation; SQLite is a local test artifact only."""
from __future__ import annotations

import hashlib
import hmac
import secrets
import sqlite3
import time
from collections import Counter
from pathlib import Path
from typing import Any


class LedgerInvariantError(RuntimeError):
    pass


class IdentityLedger:
    """Single event-loop owner. Checkpoints commit at most 256 transitions apart.

    Tokens are keyed per artifact; neither the key nor raw identities are saved.
    This detects response-level duplication, not unobserved server-side effects.
    """

    def __init__(self, path: Path) -> None:
        # Refuse replacement, including an interrupted run's evidence.
        with path.open("xb"):
            pass
        self.path = path
        self.key = secrets.token_bytes(32)
        self.connection = sqlite3.connect(path)
        self.connection.executescript("""
            PRAGMA cache_size = -4096;
            CREATE TABLE identities (
                identity TEXT PRIMARY KEY, state TEXT NOT NULL,
                attempts INTEGER NOT NULL DEFAULT 0, updated_seconds REAL NOT NULL
            );
            CREATE TABLE decision_effects (
                decision TEXT PRIMARY KEY, identity TEXT NOT NULL
            );
            CREATE INDEX effects_identity ON decision_effects(identity);
            CREATE TABLE seeds (identity TEXT PRIMARY KEY);
            CREATE TABLE persisted (
                identity TEXT PRIMARY KEY, transactions INTEGER, ingestion_audit INTEGER,
                outbox_events INTEGER, decisions INTEGER, rule_executions INTEGER,
                idempotency_keys INTEGER, decision_outbox INTEGER
            );
            CREATE TABLE persisted_decisions (decision TEXT PRIMARY KEY, identity TEXT NOT NULL);
            CREATE TABLE shared_seed_records (idempotency_keys INTEGER, batch_outbox INTEGER);
        """)
        self.counts: Counter[str] = Counter()
        self.duplicates = 0
        self.effects = 0
        self.pending = 0
        self.started = time.perf_counter()

    def __enter__(self) -> IdentityLedger:
        return self

    def __exit__(self, *_args: Any) -> None:
        try:
            self.connection.commit()
        finally:
            self.connection.close()

    def token(self, tenant: str, identity: str) -> str:
        return hmac.new(self.key, f"{tenant}\0{identity}".encode(), hashlib.sha256).hexdigest()

    def _checkpoint(self) -> None:
        self.pending += 1
        if self.pending >= 256:
            self.connection.commit()
            self.pending = 0

    def register(self, tenant: str, identity: str) -> None:
        if self.connection.execute("SELECT 1 FROM seeds WHERE identity=?", (self.token(tenant, identity),)).fetchone():
            raise LedgerInvariantError("seed_evaluation_overlap")
        try:
            self.connection.execute("INSERT INTO identities VALUES (?, 'ingesting', 0, ?)",
                                    (self.token(tenant, identity), time.perf_counter() - self.started))
        except sqlite3.IntegrityError as exc:
            self.duplicates += 1
            raise LedgerInvariantError("duplicate_input_identity") from exc
        self.counts["ingesting"] += 1
        self._checkpoint()

    def register_seed(self, tenant: str, identity: str) -> None:
        try:
            self.connection.execute("INSERT INTO seeds VALUES (?)", (self.token(tenant, identity),))
        except sqlite3.IntegrityError as exc:
            raise LedgerInvariantError("duplicate_seed_identity") from exc
        self._checkpoint()

    def transition(self, tenant: str, identity: str, previous: str, state: str,
                   *, attempts: int = 0, decisions: tuple[str, ...] = ()) -> None:
        allowed = {("ingesting", "ingested"), ("ingesting", "ingestion_failed"),
                   ("ingested", "evaluating"), ("evaluating", "completed"),
                   ("evaluating", "decision_failed"), ("evaluating", "unresolved")}
        if (previous, state) not in allowed or (decisions and state != "completed"):
            raise LedgerInvariantError("invalid_ledger_transition")
        token = self.token(tenant, identity)
        if not self.connection.in_transaction:
            self.connection.execute("BEGIN")
        self.connection.execute("SAVEPOINT transition")
        try:
            updated = self.connection.execute(
                "UPDATE identities SET state=?, attempts=attempts+?, updated_seconds=? "
                "WHERE identity=? AND state=?",
                (state, attempts, time.perf_counter() - self.started, token, previous))
            if updated.rowcount != 1:
                raise LedgerInvariantError("unexpected_identity_state")
            for decision in decisions:
                self.connection.execute("INSERT INTO decision_effects VALUES (?, ?)",
                                        (self.token(tenant, decision), token))
        except BaseException as exc:
            self.connection.execute("ROLLBACK TO transition")
            self.connection.execute("RELEASE transition")
            if isinstance(exc, sqlite3.IntegrityError):
                self.duplicates += 1
                raise LedgerInvariantError("duplicate_decision_identity") from exc
            raise
        self.connection.execute("RELEASE transition")
        self.counts[previous] -= 1
        self.counts[state] += 1
        self.effects += len(decisions)
        self._checkpoint()

    def summary(self, target: int) -> dict[str, Any]:
        self.connection.commit()
        self.pending = 0
        unfinished = sum(self.counts[state] for state in ("ingesting", "ingested", "evaluating", "unresolved"))
        valid = (self.counts["completed"] == target and sum(self.counts.values()) == target
                 and not unfinished and not self.duplicates)
        return {"artifact": self.path.name, "scope": "response_identity_reconciliation",
                "states": dict(self.counts), "registered": sum(self.counts.values()),
                "decision_ids": self.effects, "duplicate_observations": self.duplicates,
                "unfinished": unfinished, "valid": valid,
                "durable_database_effects_verified": False,
                "limitations": ["evaluation_only_not_seed_records", "response_evidence_only",
                                "abrupt_kill_can_lose_up_to_255_transitions",
                                "local_ledger_io_included_in_measured_throughput"]}
