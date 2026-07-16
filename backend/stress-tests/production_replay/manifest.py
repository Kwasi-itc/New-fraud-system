from __future__ import annotations

import glob
import hashlib
import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any


class ManifestError(ValueError):
    pass


@dataclass(frozen=True)
class ReferenceSources:
    merchant_globs: tuple[str, ...]
    merchant_product_globs: tuple[str, ...]
    staff_csv: str
    merchant_watchlist_xlsx: str | None = None


@dataclass(frozen=True)
class TransactionStream:
    id: str
    adapter: str
    globs: tuple[str, ...]
    channel: str
    direction: str
    system_type: str
    timezone: str


@dataclass(frozen=True)
class ReplayManifest:
    path: Path
    version: int
    timezone: str
    reference_data: ReferenceSources
    transaction_streams: tuple[TransactionStream, ...]

    def resolve_pattern(self, pattern: str) -> str:
        candidate = Path(pattern).expanduser()
        if candidate.is_absolute():
            return str(candidate)
        return str((self.path.parent / candidate).resolve())

    def resolve_globs(self, patterns: tuple[str, ...], *, label: str) -> list[Path]:
        matches: set[Path] = set()
        for pattern in patterns:
            for match in glob.glob(self.resolve_pattern(pattern), recursive=True):
                path = Path(match)
                if path.is_file():
                    matches.add(path.resolve())
        if not matches:
            raise ManifestError(f"{label} did not match any files")
        return sorted(matches)

    def merchant_files(self) -> list[Path]:
        return self.resolve_globs(self.reference_data.merchant_globs, label="reference_data.merchant_globs")

    def merchant_product_files(self) -> list[Path]:
        return self.resolve_globs(
            self.reference_data.merchant_product_globs,
            label="reference_data.merchant_product_globs",
        )

    def staff_file(self) -> Path:
        path = Path(self.resolve_pattern(self.reference_data.staff_csv))
        if not path.is_file():
            raise ManifestError(f"reference_data.staff_csv does not exist: {path}")
        return path.resolve()

    def merchant_watchlist_file(self) -> Path | None:
        if not self.reference_data.merchant_watchlist_xlsx:
            return None
        path = Path(self.resolve_pattern(self.reference_data.merchant_watchlist_xlsx))
        if not path.is_file():
            raise ManifestError(f"reference_data.merchant_watchlist_xlsx does not exist: {path}")
        return path.resolve()

    def stream_files(self, stream: TransactionStream) -> list[Path]:
        return self.resolve_globs(stream.globs, label=f"transaction stream {stream.id!r}")

    def source_fingerprint(self) -> str:
        files = [*self.merchant_files(), *self.merchant_product_files(), self.staff_file()]
        watchlist = self.merchant_watchlist_file()
        if watchlist is not None:
            files.append(watchlist)
        for stream in self.transaction_streams:
            files.extend(self.stream_files(stream))

        digest = hashlib.sha256()
        digest.update(self.path.read_bytes())
        for path in sorted(set(files)):
            stat = path.stat()
            digest.update(str(path).encode())
            digest.update(f":{stat.st_size}:{stat.st_mtime_ns}\n".encode())
        return digest.hexdigest()


def _required_string(value: dict[str, Any], name: str, context: str) -> str:
    result = value.get(name)
    if not isinstance(result, str) or not result.strip():
        raise ManifestError(f"{context}.{name} must be a non-empty string")
    return result.strip()


def _required_strings(value: dict[str, Any], name: str, context: str) -> tuple[str, ...]:
    result = value.get(name)
    if not isinstance(result, list) or not result or not all(isinstance(item, str) and item.strip() for item in result):
        raise ManifestError(f"{context}.{name} must be a non-empty list of strings")
    return tuple(item.strip() for item in result)


def load_manifest(path: str | Path) -> ReplayManifest:
    manifest_path = Path(path).expanduser().resolve()
    try:
        raw = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ManifestError(f"could not read manifest {manifest_path}: {exc}") from exc
    if not isinstance(raw, dict):
        raise ManifestError("manifest root must be an object")
    if raw.get("version") != 1:
        raise ManifestError("manifest.version must be 1")

    timezone = _required_string(raw, "timezone", "manifest")
    reference = raw.get("reference_data")
    if not isinstance(reference, dict):
        raise ManifestError("manifest.reference_data must be an object")
    sources = ReferenceSources(
        merchant_globs=_required_strings(reference, "merchant_globs", "reference_data"),
        merchant_product_globs=_required_strings(reference, "merchant_product_globs", "reference_data"),
        staff_csv=_required_string(reference, "staff_csv", "reference_data"),
        merchant_watchlist_xlsx=(
            _required_string(reference, "merchant_watchlist_xlsx", "reference_data")
            if reference.get("merchant_watchlist_xlsx") is not None
            else None
        ),
    )

    raw_streams = raw.get("transaction_streams")
    if not isinstance(raw_streams, list) or not raw_streams:
        raise ManifestError("manifest.transaction_streams must be a non-empty list")
    streams: list[TransactionStream] = []
    seen_ids: set[str] = set()
    for index, item in enumerate(raw_streams):
        context = f"transaction_streams[{index}]"
        if not isinstance(item, dict):
            raise ManifestError(f"{context} must be an object")
        stream_id = _required_string(item, "id", context)
        if stream_id in seen_ids:
            raise ManifestError(f"duplicate transaction stream id: {stream_id}")
        seen_ids.add(stream_id)
        streams.append(
            TransactionStream(
                id=stream_id,
                adapter=_required_string(item, "adapter", context),
                globs=_required_strings(item, "globs", context),
                channel=_required_string(item, "channel", context),
                direction=_required_string(item, "direction", context),
                system_type=_required_string(item, "system_type", context),
                timezone=str(item.get("timezone") or timezone),
            )
        )
    return ReplayManifest(manifest_path, 1, timezone, sources, tuple(streams))
