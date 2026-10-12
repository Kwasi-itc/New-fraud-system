"""Bounded-memory response reconciliation; SQLite is a local test artifact only."""
from __future__ import annotations

import hashlib
import hmac
import secrets
import sqlite3
import sys
import time
from collections import Counter
from contextlib import contextmanager
from collections.abc import Iterator
from pathlib import Path
from typing import Any

from .local_storage import DiskSpaceGuard, LocalStorageError


# The named SQLite error constants/error attributes are unavailable in Python 3.10.
SQLITE_FULL_PRIMARY_CODE = 13


class LedgerInvariantError(RuntimeError):
    pass


class LedgerStorageError(LocalStorageError):
    pass


class LedgerCleanupError(LocalStorageError):
    pass


class IdentityLedger:
    """Single event-loop owner. Checkpoints commit at most 256 transitions apart.

    Tokens are keyed per artifact; neither the key nor raw identities are saved.
    This detects response-level duplication, not unobserved server-side effects.
    """

    def __init__(self, path: Path, storage_guard: DiskSpaceGuard | None = None,
                 *, retain_artifact: bool = True) -> None:
        # Only these exact files can belong to this ledger. Never glob/delete a
        # run directory or adopt sidecars left by an earlier ledger.
        self.artifact_paths = (path, *(Path(str(path) + suffix) for suffix in ("-journal", "-wal", "-shm")))
        for sidecar in self.artifact_paths[1:]:
            if sidecar.exists() or sidecar.is_symlink():
                raise FileExistsError(f"ledger sidecar already exists: {sidecar.name}")
        # Refuse replacement, including an interrupted run's evidence.
        with path.open("xb"):
            pass
        self.path = path
        self.retain_artifact = retain_artifact
        self.artifact_state = "active"
        self.cleanup_error: dict[str, Any] | None = None
        self.key = secrets.token_bytes(32)
        self.storage_guard = storage_guard
        self.storage_error: LedgerStorageError | None = None
        self.counts: Counter[str] = Counter()
        self._committed_counts: Counter[str] = Counter()
        self._committed_effects = 0
        self.duplicates = 0
        self.effects = 0
        self.pending = 0
        self.started = time.perf_counter()
        try:
            self.connection = sqlite3.connect(path)
            with self.storage_operation("initialize"):
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
        except BaseException as exc:
            self._close(exc)
            raise

    def __enter__(self) -> IdentityLedger:
        return self

    def __exit__(self, exc_type: Any, exc: BaseException | None, traceback: Any) -> None:
        failure = exc
        try:
            if self.storage_error is None:
                try:
                    self.flush()
                except LedgerStorageError:
                    if exc is None:
                        raise
                    # Do not replace the workload error with a failed final commit.
                    print("warning: final identity ledger checkpoint failed", file=sys.stderr)
        except BaseException as close_error:
            failure = close_error
            raise
        finally:
            self._close(failure)

    def artifact_summary(self) -> dict[str, Any]:
        return {"artifact": self.path.name, "retention": "retained" if self.retain_artifact else "delete_after_phase",
                "status": self.artifact_state, "cleanup_error": self.cleanup_error}

    def _close(self, failure: BaseException | None) -> None:
        # Unlink only after SQLite has released its database/journal handles.
        try:
            connection = getattr(self, "connection", None)
            if connection is not None:
                connection.close()
        except sqlite3.Error:
            self.artifact_state = "close_failed"
            if failure is None:
                raise
            print("warning: identity ledger close failed; artifact retained", file=sys.stderr)
            return
        if self.retain_artifact:
            self.artifact_state = "retained"
            return
        try:
            # Remove sidecars first; if deletion fails, keep the main ledger.
            for path in (*self.artifact_paths[1:], self.path):
                path.unlink(missing_ok=True)
        except OSError as exc:
            self.artifact_state = "cleanup_failed"
            self.cleanup_error = {"category": "identity_ledger_cleanup_failed", "errno": exc.errno}
            if failure is None:
                raise LedgerCleanupError("identity_ledger_cleanup_failed: closed ledger artifacts "
                                         "could not be removed; check local file permissions", self.cleanup_error) from exc
            print("warning: identity ledger cleanup failed; original run error preserved", file=sys.stderr)
        else:
            self.artifact_state = "deleted"

    def token(self, tenant: str, identity: str) -> str:
        return hmac.new(self.key, f"{tenant}\0{identity}".encode(), hashlib.sha256).hexdigest()

    @contextmanager
    def storage_operation(self, operation: str, *, writing: bool = True) -> Iterator[None]:
        if self.storage_error is not None:
            raise self.storage_error
        if writing and self.storage_guard is not None:
            self.storage_guard.check()
        try:
            yield
        except sqlite3.IntegrityError:
            # Constraint failures do not automatically discard earlier writes.
            # The caller's savepoint/domain validation owns their interpretation.
            raise
        except sqlite3.DatabaseError as exc:
            # SQLITE_FULL and some I/O failures auto-roll back the *whole*
            # transaction, including earlier successful transitions. Poison the
            # ledger before another worker can reinterpret that loss as a state
            # invariant violation. Never retry an uncertain write/commit.
            rollback_succeeded = False
            try:
                self.connection.rollback()
                rollback_succeeded = True
            except sqlite3.DatabaseError:
                pass  # Original storage failure is retained below, not ignored.
            self.counts = self._committed_counts.copy()
            self.effects = self._committed_effects
            self.pending = 0
            # sqlite_errorcode is unavailable in Python 3.10. Match only this
            # known SQLite message; never put arbitrary exception text in JSON.
            code = getattr(exc, "sqlite_errorcode", None)
            full = (code is not None and code & 0xff == SQLITE_FULL_PRIMARY_CODE) or str(exc) == "database or disk is full"
            category = "identity_ledger_disk_full" if full else "identity_ledger_storage_failure"
            self.storage_error = LedgerStorageError(
                f"{category}: {operation} failed; run aborted. Ledger counts now "
                "describe only the last successful checkpoint; later writes are unverified. "
                "Check local artifact disk space and I/O before rerunning.",
                {"category": category, "operation": operation, "sqlite_error_code": code,
                 "rollback_succeeded": rollback_succeeded,
                 "counts_basis": "last_successful_checkpoint",
                 "durability_uncertain": operation == "commit" or not rollback_succeeded},
            )
            raise self.storage_error from exc

    def flush(self) -> None:
        # A low-space guard must still allow final commits within the reserve.
        with self.storage_operation("commit", writing=False):
            self.connection.commit()
        self._committed_counts = self.counts.copy()
        self._committed_effects = self.effects
        self.pending = 0

    def _checkpoint(self) -> None:
        self.pending += 1
        if self.pending >= 256:
            self.flush()

    def register(self, tenant: str, identity: str) -> None:
        with self.storage_operation("register"):
            self._register(tenant, identity)

    def _register(self, tenant: str, identity: str) -> None:
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
        with self.storage_operation("register_seed"):
            self._register_seed(tenant, identity)

    def _register_seed(self, tenant: str, identity: str) -> None:
        try:
            self.connection.execute("INSERT INTO seeds VALUES (?)", (self.token(tenant, identity),))
        except sqlite3.IntegrityError as exc:
            raise LedgerInvariantError("duplicate_seed_identity") from exc
        self._checkpoint()

    def transition(self, tenant: str, identity: str, previous: str, state: str,
                   *, attempts: int = 0, decisions: tuple[str, ...] = ()) -> None:
        with self.storage_operation("transition"):
            self._transition(tenant, identity, previous, state, attempts=attempts, decisions=decisions)

    def _transition(self, tenant: str, identity: str, previous: str, state: str,
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
            if isinstance(exc, sqlite3.DatabaseError) and not isinstance(exc, sqlite3.IntegrityError):
                # The savepoint may no longer exist after an automatic rollback.
                # The enclosing storage operation owns rollback/error reporting.
                raise
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
        if self.storage_error is None:
            self.flush()
        unfinished = sum(self.counts[state] for state in ("ingesting", "ingested", "evaluating", "unresolved"))
        valid = (self.counts["completed"] == target and sum(self.counts.values()) == target
                 and not unfinished and not self.duplicates and self.storage_error is None)
        return {"artifact": self.path.name, "scope": "response_identity_reconciliation",
                "states": dict(self.counts), "registered": sum(self.counts.values()),
                "decision_ids": self.effects, "duplicate_observations": self.duplicates,
                "unfinished": unfinished, "valid": valid,
                "storage_error": self.storage_error.details if self.storage_error is not None else None,
                "counts_basis": "last_successful_checkpoint" if self.storage_error is not None else "committed_ledger",
                "durable_database_effects_verified": False,
                "limitations": ["evaluation_only_not_seed_records", "response_evidence_only",
                                "abrupt_kill_can_lose_up_to_255_transitions",
                                "local_ledger_io_included_in_measured_throughput"]}
