from __future__ import annotations

import math
from datetime import date, datetime
from enum import StrEnum
from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator

FiniteNonNegative = Annotated[float, Field(ge=0, allow_inf_nan=False)]


class FoodGroup(StrEnum):
    PROTEIN = "protein"
    VEGETABLE = "vegetable"
    FRUIT = "fruit"
    GRAIN = "grain"
    DAIRY = "dairy"
    FAT = "fat"
    SWEET = "sweet"
    DRINK = "drink"
    OTHER = "other"


class MealItem(BaseModel):
    model_config = ConfigDict(extra="forbid")

    item_id: str = Field(min_length=1, max_length=64)
    name: str = Field(min_length=1, max_length=160)
    amount_g: Annotated[float, Field(gt=0, le=5000, allow_inf_nan=False)]
    kcal: Annotated[float, Field(ge=0, le=10000, allow_inf_nan=False)]
    protein_g: Annotated[float, Field(ge=0, le=1000, allow_inf_nan=False)]
    fat_g: Annotated[float, Field(ge=0, le=1000, allow_inf_nan=False)]
    carbs_g: Annotated[float, Field(ge=0, le=2000, allow_inf_nan=False)]
    food_groups: list[FoodGroup] = Field(default_factory=list, max_length=5)
    confidence: Annotated[float, Field(ge=0, le=1, allow_inf_nan=False)]
    assumptions: list[str] = Field(default_factory=list, max_length=10)

    @field_validator("name")
    @classmethod
    def strip_name(cls, value: str) -> str:
        stripped = value.strip()
        if not stripped:
            raise ValueError("name cannot be blank")
        return stripped


class MealAnalysis(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: Literal["1.0"] = "1.0"
    items: list[MealItem] = Field(min_length=1, max_length=20)
    assumptions: list[str] = Field(default_factory=list, max_length=20)
    confidence: Annotated[float, Field(ge=0, le=1, allow_inf_nan=False)]
    needs_clarification: bool = False
    clarification_question: str | None = Field(default=None, max_length=500)
    high_calorie_reason: str | None = Field(default=None, max_length=500)

    @model_validator(mode="after")
    def clarification_has_question(self) -> MealAnalysis:
        if self.needs_clarification and not self.clarification_question:
            raise ValueError("clarification_question is required")
        item_ids = [item.item_id for item in self.items]
        if len(item_ids) != len(set(item_ids)):
            raise ValueError("item_id values must be unique")
        return self

    @property
    def kcal(self) -> float:
        return sum(item.kcal for item in self.items)

    @property
    def protein_g(self) -> float:
        return sum(item.protein_g for item in self.items)

    @property
    def fat_g(self) -> float:
        return sum(item.fat_g for item in self.items)

    @property
    def carbs_g(self) -> float:
        return sum(item.carbs_g for item in self.items)

    @property
    def summary(self) -> str:
        return ", ".join(item.name for item in self.items)


class CorrectionAnalysis(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: Literal["1.0"] = "1.0"
    item_id: str = Field(min_length=1, max_length=64)
    new_amount_g: Annotated[float, Field(gt=0, le=5000, allow_inf_nan=False)]
    confidence: Annotated[float, Field(ge=0, le=1, allow_inf_nan=False)]
    needs_clarification: bool = False
    clarification_question: str | None = Field(default=None, max_length=500)

    @model_validator(mode="after")
    def clarification_has_question(self) -> CorrectionAnalysis:
        if self.needs_clarification and not self.clarification_question:
            raise ValueError("clarification_question is required")
        return self


class MealRecord(BaseModel):
    model_config = ConfigDict(extra="forbid")

    event_key: str = Field(min_length=3, max_length=100)
    meal_id: str = Field(min_length=1, max_length=64)
    telegram_user_id: int = Field(gt=0)
    logged_at: datetime
    local_date: date
    raw_text: str = Field(min_length=1, max_length=4000)
    summary: str = Field(min_length=1, max_length=1000)
    kcal: FiniteNonNegative
    protein_g: FiniteNonNegative
    fat_g: FiniteNonNegative
    carbs_g: FiniteNonNegative
    items: list[MealItem] = Field(min_length=1, max_length=20)
    assumptions: list[str] = Field(default_factory=list, max_length=20)
    revision: int = Field(default=1, ge=1)
    last_edit_event_key: str | None = Field(default=None, max_length=100)

    @model_validator(mode="after")
    def values_match_items(self) -> MealRecord:
        if self.logged_at.utcoffset() is None:
            raise ValueError("logged_at must include a UTC offset")
        expected = (self.kcal, self.protein_g, self.fat_g, self.carbs_g)
        actual = (
            sum(item.kcal for item in self.items),
            sum(item.protein_g for item in self.items),
            sum(item.fat_g for item in self.items),
            sum(item.carbs_g for item in self.items),
        )
        if any(
            not math.isclose(left, right, abs_tol=0.05)
            for left, right in zip(expected, actual, strict=True)
        ):
            raise ValueError("meal totals must be calculated from items")
        return self


class DailySummary(BaseModel):
    model_config = ConfigDict(extra="forbid")

    local_date: date
    kcal: FiniteNonNegative = 0
    protein_g: FiniteNonNegative = 0
    fat_g: FiniteNonNegative = 0
    carbs_g: FiniteNonNegative = 0


class ReportRecord(BaseModel):
    model_config = ConfigDict(extra="forbid")

    report_key: str
    local_date: date
    status: Literal["sending", "sent"]
    sent_at: datetime | None = None
    telegram_message_id: int | None = None


class RecordMealStatus(StrEnum):
    STORED = "stored"
    DUPLICATE = "duplicate"
    NEEDS_CLARIFICATION = "needs_clarification"
    NEEDS_CONFIRMATION = "needs_confirmation"


class RecordMealResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    status: RecordMealStatus
    meal: MealRecord | None = None
    daily: DailySummary | None = None
    prompt: str | None = None
    confirmation_token: str | None = None


class DeleteResult(BaseModel):
    deleted_meals: int = Field(ge=0)
    deleted_reports: int = Field(ge=0)
