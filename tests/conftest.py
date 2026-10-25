from datetime import datetime
from zoneinfo import ZoneInfo

import pytest

from food_tracker.domain.models import FoodGroup, MealAnalysis, MealItem


@pytest.fixture
def moscow() -> ZoneInfo:
    return ZoneInfo("Europe/Moscow")


@pytest.fixture
def fixed_now(moscow: ZoneInfo) -> datetime:
    return datetime(2026, 9, 14, 12, 30, tzinfo=moscow)


@pytest.fixture
def analysis() -> MealAnalysis:
    return MealAnalysis(
        items=[
            MealItem(
                item_id="item-1",
                name="Куриная грудка",
                amount_g=150,
                kcal=248,
                protein_g=46.5,
                fat_g=5.4,
                carbs_g=0,
                food_groups=[FoodGroup.PROTEIN],
                confidence=0.9,
                assumptions=[],
            ),
            MealItem(
                item_id="item-2",
                name="Гречка",
                amount_g=200,
                kcal=220,
                protein_g=8,
                fat_g=2,
                carbs_g=42,
                food_groups=[FoodGroup.GRAIN],
                confidence=0.85,
                assumptions=["Вес указан для готовой крупы"],
            ),
        ],
        assumptions=["Курица приготовлена без большого количества масла"],
        confidence=0.85,
    )

