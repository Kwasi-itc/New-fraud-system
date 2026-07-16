from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any


@dataclass(frozen=True)
class TransactionEvent:
    occurred_at: datetime
    stream_id: str
    object_id: str
    fields: dict[str, Any]
    source_file: Path
    row_number: int
    sequence: int | None = None

    def to_sort_record(self, sequence: int) -> dict[str, Any]:
        return {
            "occurred_at": self.occurred_at.isoformat(),
            "stream_id": self.stream_id,
            "object_id": self.object_id,
            "fields": self.fields,
            "source_file": self.source_file.name,
            "row_number": self.row_number,
            "sequence": sequence,
        }

    @classmethod
    def from_sort_record(cls, value: dict[str, Any]) -> "TransactionEvent":
        return cls(
            occurred_at=datetime.fromisoformat(value["occurred_at"]),
            stream_id=value["stream_id"],
            object_id=value["object_id"],
            fields=value["fields"],
            source_file=Path(value["source_file"]),
            row_number=int(value["row_number"]),
            sequence=int(value["sequence"]),
        )
