import asyncio
from datetime import UTC, datetime, timedelta
from zoneinfo import ZoneInfo

import pytest

from food_tracker.domain.errors import MealValidationError
from food_tracker.domain.models import (
    CorrectionAnalysis,
    MealAnalysis,
    MealItem,
    RecordMealStatus,
)
from food_tracker.infrastructure.fakes import FakeFoodAnalyzer, FakeSheetsRepository
from food_tracker.services import MealService


@pytest.mark.asyncio
async def test_records_and_calculates_daily_summary(
    analysis: MealAnalysis, fixed_now: datetime, moscow: ZoneInfo
) -> None:
    repository = FakeSheetsRepository()
    service = MealService(
        repository,
        FakeFoodAnalyzer(analysis),
        moscow,
        clock=lambda: fixed_now,
    )

    result = await service.record_meal(42, "100:7", "курица и гречка")

    assert result.status == RecordMealStatus.STORED
    assert result.meal is not None
    assert result.meal.local_date.isoformat() == "2026-09-14"
    assert result.meal.logged_at.isoformat().endswith("+03:00")
    assert result.meal.kcal == pytest.approx(468)
    assert result.daily is not None


@pytest.mark.asyncio
async def test_duplicate_update_does_not_call_analyzer_twice(
    analysis: MealAnalysis, fixed_now: datetime, moscow: ZoneInfo
) -> None:
    repository = FakeSheetsRepository()
    analyzer = FakeFoodAnalyzer(analysis)
    service = MealService(repository, analyzer, moscow, clock=lambda: fixed_now)

    first = await service.record_meal(42, "100:7", "еда")
    second = await service.record_meal(42, "100:7", "еда")

    assert first.status == RecordMealStatus.STORED
    assert second.status == RecordMealStatus.DUPLICATE
    assert analyzer.analyze_calls == 1
    assert repository.append_calls == 1


@pytest.mark.asyncio
async def test_parallel_duplicate_joins_inflight_analysis(
    analysis: MealAnalysis, fixed_now: datetime, moscow: ZoneInfo
) -> None:
    repository = FakeSheetsRepository()
    analyzer = FakeFoodAnalyzer(analysis, delay=0.02)
    service = MealService(repository, analyzer, moscow, clock=lambda: fixed_now)

    results = await asyncio.gather(
        service.record_meal(42, "100:8", "еда"),
        service.record_meal(42, "100:8", "еда"),
    )

    assert len(results) == 2
    assert analyzer.analyze_calls == 1
    assert repository.append_calls == 1
    assert results[0].meal == results[1].meal


@pytest.mark.asyncio
async def test_requires_confirmation_between_5000_and_10000(
    fixed_now: datetime, moscow: ZoneInfo
) -> None:
    analysis = MealAnalysis(
        items=[
            MealItem(
                item_id="item-1",
                name="Большой заказ",
                amount_g=3000,
                kcal=6000,
                protein_g=200,
                fat_g=250,
                carbs_g=700,
                confidence=0.8,
            )
        ],
        assumptions=[],
        confidence=0.8,
        high_calorie_reason="указана большая общая порция",
    )
    repository = FakeSheetsRepository()
    service = MealService(repository, FakeFoodAnalyzer(analysis), moscow, clock=lambda: fixed_now)

    pending = await service.record_meal(42, "100:9", "большой заказ")
    assert pending.status == RecordMealStatus.NEEDS_CONFIRMATION
    assert not repository.meals

    confirmed = await service.confirm_meal(42, pending.confirmation_token or "")
    assert confirmed.status == RecordMealStatus.STORED
    assert len(repository.meals) == 1


@pytest.mark.asyncio
async def test_rejects_total_over_10000(fixed_now: datetime, moscow: ZoneInfo) -> None:
    items = [
        MealItem(
            item_id=f"item-{index}",
            name="Еда",
            amount_g=1000,
            kcal=3000,
            protein_g=10,
            fat_g=10,
            carbs_g=10,
            confidence=0.5,
        )
        for index in range(4)
    ]
    analysis = MealAnalysis(
        items=items,
        assumptions=[],
        confidence=0.5,
        high_calorie_reason="очень большая порция",
    )
    service = MealService(
        FakeSheetsRepository(), FakeFoodAnalyzer(analysis), moscow, clock=lambda: fixed_now
    )

    with pytest.raises(MealValidationError):
        await service.record_meal(42, "100:10", "еда")


@pytest.mark.asyncio
async def test_edit_last_scales_selected_item_only(
    analysis: MealAnalysis, fixed_now: datetime, moscow: ZoneInfo
) -> None:
    correction = CorrectionAnalysis(
        item_id="item-1",
        new_amount_g=100,
        confidence=0.95,
    )
    repository = FakeSheetsRepository()
    analyzer = FakeFoodAnalyzer(analysis, correction)
    service = MealService(repository, analyzer, moscow, clock=lambda: fixed_now)
    original = await service.record_meal(42, "100:11", "еда")
    assert original.meal is not None
    await service.begin_edit(42)

    result = await service.correct_last(42, "100:12", "курицы было 100 г")

    assert result.meal is not None
    assert result.meal.revision == 2
    assert result.meal.last_edit_event_key == "100:12"
    assert result.meal.items[0].amount_g == 100
    assert result.meal.items[1] == original.meal.items[1]
    assert result.meal.kcal == pytest.approx(248 * 2 / 3 + 220)


@pytest.mark.asyncio
async def test_duplicate_correction_is_safe(
    analysis: MealAnalysis, fixed_now: datetime, moscow: ZoneInfo
) -> None:
    correction = CorrectionAnalysis(item_id="item-1", new_amount_g=100, confidence=1)
    repository = FakeSheetsRepository()
    analyzer = FakeFoodAnalyzer(analysis, correction)
    service = MealService(repository, analyzer, moscow, clock=lambda: fixed_now)
    await service.record_meal(42, "100:13", "еда")
    await service.begin_edit(42)
    first = await service.correct_last(42, "100:14", "100 г")
    second = await service.correct_last(42, "100:14", "100 г")

    assert first.meal == second.meal
    assert second.status == RecordMealStatus.DUPLICATE
    assert analyzer.correction_calls == 1


@pytest.mark.asyncio
async def test_edit_session_expires_after_15_minutes(
    analysis: MealAnalysis, fixed_now: datetime, moscow: ZoneInfo
) -> None:
    repository = FakeSheetsRepository()
    current = [fixed_now]
    service = MealService(
        repository,
        FakeFoodAnalyzer(analysis),
        moscow,
        clock=lambda: current[0],
    )
    await service.record_meal(42, "100:19", "еда")
    await service.begin_edit(42)

    current[0] += timedelta(minutes=16)

    assert await service.has_edit_session(42) is False


@pytest.mark.asyncio
async def test_utc_timestamp_is_assigned_to_local_date(
    analysis: MealAnalysis, moscow: ZoneInfo
) -> None:
    utc_time = datetime(2026, 9, 14, 22, 30, tzinfo=UTC)
    service = MealService(
        FakeSheetsRepository(),
        FakeFoodAnalyzer(analysis),
        moscow,
        clock=lambda: utc_time,
    )

    result = await service.record_meal(42, "100:20", "еда")

    assert result.meal is not None
    assert result.meal.local_date.isoformat() == "2026-09-15"


@pytest.mark.asyncio
async def test_clear_data_is_repeatable(
    analysis: MealAnalysis, fixed_now: datetime, moscow: ZoneInfo
) -> None:
    repository = FakeSheetsRepository()
    service = MealService(
        repository,
        FakeFoodAnalyzer(analysis),
        moscow,
        clock=lambda: fixed_now,
    )
    await service.record_meal(42, "100:21", "еда")
    await repository.claim_report("daily:42:2026-09-14", fixed_now.date())

    first = await repository.clear_personal_data()
    second = await repository.clear_personal_data()

    assert first.deleted_meals == 1
    assert first.deleted_reports == 1
    assert second.deleted_meals == 0
