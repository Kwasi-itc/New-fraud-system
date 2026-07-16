from __future__ import annotations

from pathlib import Path
from typing import Iterator, Protocol

from ..domain import TransactionEvent
from ..manifest import TransactionStream


class AdapterError(ValueError):
    pass


class TransactionAdapter(Protocol):
    name: str

    def iter_events(self, stream: TransactionStream, files: list[Path]) -> Iterator[TransactionEvent]: ...
