"""Shared monotonic fixed-arrival schedule; consumers own admission and accounting."""
from __future__ import annotations

import asyncio
import time
from collections.abc import AsyncIterator, Awaitable, Callable
from typing import Any


async def wait_until(when: float) -> None:
    await asyncio.sleep(max(0, when - time.monotonic()))


async def arrivals(stages: list[Any], started: float, *,
                   wait: Callable[[float], Awaitable[None]] = wait_until,
                   on_stage: Callable[[str], None] | None = None) -> AsyncIterator[tuple[Any, int, float]]:
    """Preserve original arrival times, including when consumers are behind schedule."""
    offset = 0.0
    for stage in stages:
        if on_stage is not None:
            on_stage(stage.name)
        for slot in range(stage.count):
            scheduled = started + offset + slot / stage.rate
            await wait(scheduled)
            yield stage, slot, scheduled
        offset += stage.seconds
        await wait(started + offset)
