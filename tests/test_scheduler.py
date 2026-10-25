from datetime import datetime, time
from zoneinfo import ZoneInfo

import pytest

from food_tracker.domain.models import MealAnalysis
from food_tracker.infrastructure.fakes import (
    FakeFoodAnalyzer,
    FakeSheetsRepository,
    FakeTelegramTransport,
)
from food_tracker.scheduler import DailyReportScheduler
from food_tracker.services import MealService


@pytest.mark.asyncio
async def test_report_is_sent_once(
    analysis: MealAnalysis, fixed_now: datetime, moscow: ZoneInfo
) -> None:
    repository = FakeSheetsRepository()
    service = MealService(repository, FakeFoodAnalyzer(analysis), moscow, clock=lambda: fixed_now)
    await service.record_meal(42, "100:15", "еда")
    transport = FakeTelegramTransport()
    scheduler = DailyReportScheduler(repository, transport, 42, moscow, time(21, 0))
    report_time = fixed_now.replace(hour=21)

    assert await scheduler.run_once(report_time) is True
    restarted_scheduler = DailyReportScheduler(
        repository, transport, 42, moscow, time(21, 0)
    )
    assert await restarted_scheduler.run_once(report_time) is False
    assert len(transport.messages) == 1
    assert next(iter(repository.reports.values())).status == "sent"


@pytest.mark.asyncio
async def test_empty_day_is_skipped(fixed_now: datetime, moscow: ZoneInfo) -> None:
    repository = FakeSheetsRepository()
    scheduler = DailyReportScheduler(
        repository, FakeTelegramTransport(), 42, moscow, time(21, 0)
    )

    assert await scheduler.run_once(fixed_now.replace(hour=21)) is False
    assert not repository.reports


@pytest.mark.asyncio
async def test_report_before_21_is_skipped(
    analysis: MealAnalysis, fixed_now: datetime, moscow: ZoneInfo
) -> None:
    repository = FakeSheetsRepository()
    service = MealService(repository, FakeFoodAnalyzer(analysis), moscow, clock=lambda: fixed_now)
    await service.record_meal(42, "100:16", "еда")
    scheduler = DailyReportScheduler(
        repository, FakeTelegramTransport(), 42, moscow, time(21, 0)
    )

    assert await scheduler.run_once(fixed_now.replace(hour=20, minute=59)) is False


@pytest.mark.asyncio
async def test_next_local_day_gets_a_new_report(
    analysis: MealAnalysis, fixed_now: datetime, moscow: ZoneInfo
) -> None:
    repository = FakeSheetsRepository()
    current = [fixed_now]
    service = MealService(repository, FakeFoodAnalyzer(analysis), moscow, clock=lambda: current[0])
    transport = FakeTelegramTransport()
    scheduler = DailyReportScheduler(repository, transport, 42, moscow, time(21, 0))
    await service.record_meal(42, "100:17", "еда")
    await scheduler.run_once(current[0].replace(hour=21))

    current[0] = fixed_now.replace(day=15)
    await service.record_meal(42, "100:18", "еда")
    assert await scheduler.run_once(current[0].replace(hour=21)) is True
    assert len(transport.messages) == 2
