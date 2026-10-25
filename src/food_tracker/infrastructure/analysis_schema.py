from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from food_tracker.domain.models import FoodGroup


class ProviderMealItem(BaseModel):
    """Strict, provider-neutral structured-output payload for one food item."""

    model_config = ConfigDict(extra="forbid")

    item_id: str = Field(min_length=1, max_length=64)
    name: str = Field(min_length=1, max_length=160)
    amount_g: float = Field(gt=0, le=5000, allow_inf_nan=False)
    kcal: float = Field(ge=0, le=10000, allow_inf_nan=False)
    protein_g: float = Field(ge=0, le=1000, allow_inf_nan=False)
    fat_g: float = Field(ge=0, le=1000, allow_inf_nan=False)
    carbs_g: float = Field(ge=0, le=2000, allow_inf_nan=False)
    food_groups: list[FoodGroup] = Field(max_length=5)
    confidence: float = Field(ge=0, le=1, allow_inf_nan=False)
    assumptions: list[str] = Field(max_length=10)


class ProviderMealAnalysis(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: Literal["1.0"]
    items: list[ProviderMealItem] = Field(min_length=1, max_length=20)
    assumptions: list[str] = Field(max_length=20)
    confidence: float = Field(ge=0, le=1, allow_inf_nan=False)
    needs_clarification: bool
    clarification_question: str | None = Field(max_length=500)
    high_calorie_reason: str | None = Field(max_length=500)


class ProviderCorrection(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: Literal["1.0"]
    item_id: str = Field(min_length=1, max_length=64)
    new_amount_g: float = Field(gt=0, le=5000, allow_inf_nan=False)
    confidence: float = Field(ge=0, le=1, allow_inf_nan=False)
    needs_clarification: bool
    clarification_question: str | None = Field(max_length=500)


SYSTEM_PROMPT = """Ты извлекаешь еду из короткого сообщения на русском языке.
Верни только данные по JSON Schema. Оцени массу и КБЖУ приблизительно и консервативно.
Каждой позиции дай стабильный внутри ответа item_id: item-1, item-2 и т.д.
Если порция или продукт настолько неоднозначны, что оценка ненадёжна, установи
needs_clarification=true и задай один короткий вопрос. Не давай медицинских советов.
Если сумма превышает 5000 ккал, объясни правдоподобную причину в high_calorie_reason;
иначе верни null. Все списки должны присутствовать, даже если они пустые.
"""
