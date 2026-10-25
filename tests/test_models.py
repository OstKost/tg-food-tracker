import math

import pytest
from pydantic import ValidationError

from food_tracker.domain.models import MealAnalysis, MealItem


def test_rejects_non_finite_number() -> None:
    with pytest.raises(ValidationError):
        MealItem(
            item_id="item-1",
            name="Еда",
            amount_g=100,
            kcal=math.inf,
            protein_g=1,
            fat_g=1,
            carbs_g=1,
            confidence=1,
        )


def test_limits_item_count() -> None:
    item = MealItem(
        item_id="item-1",
        name="Еда",
        amount_g=1,
        kcal=1,
        protein_g=0,
        fat_g=0,
        carbs_g=0,
        confidence=1,
    )
    with pytest.raises(ValidationError):
        MealAnalysis(items=[item] * 21, assumptions=[], confidence=1)


def test_clarification_requires_question() -> None:
    item = MealItem(
        item_id="item-1",
        name="Еда",
        amount_g=1,
        kcal=1,
        protein_g=0,
        fat_g=0,
        carbs_g=0,
        confidence=0.2,
    )
    with pytest.raises(ValidationError):
        MealAnalysis(
            items=[item],
            assumptions=[],
            confidence=0.2,
            needs_clarification=True,
        )

