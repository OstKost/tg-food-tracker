from __future__ import annotations

import asyncio
from copy import deepcopy
from datetime import date, datetime

from food_tracker.domain.models import (
    CorrectionAnalysis,
    DailySummary,
    DeleteResult,
    MealAnalysis,
    MealRecord,
    ReportRecord,
)


class FakeFoodAnalyzer:
    def __init__(
        self,
        analysis: MealAnalysis,
        correction: CorrectionAnalysis | None = None,
        *,
        delay: float = 0,
    ) -> None:
        self.analysis = analysis
        self.correction = correction
        self.delay = delay
        self.analyze_calls = 0
        self.correction_calls = 0

    async def analyze(self, text: str) -> MealAnalysis:
        del text
        self.analyze_calls += 1
        if self.delay:
            await asyncio.sleep(self.delay)
        return deepcopy(self.analysis)

    async def analyze_correction(self, text: str, meal: MealRecord) -> CorrectionAnalysis:
        del text, meal
        self.correction_calls += 1
        if self.correction is None:
            raise AssertionError("fake correction result was not configured")
        return deepcopy(self.correction)


class FakeSheetsRepository:
    def __init__(self) -> None:
        self.meals: list[MealRecord] = []
        self.reports: dict[str, ReportRecord] = {}
        self.append_calls = 0
        self._lock = asyncio.Lock()
        self.raise_after_append_once = False

    async def find_by_event_key(self, event_key: str) -> MealRecord | None:
        async with self._lock:
            for meal in self.meals:
                if meal.event_key == event_key or meal.last_edit_event_key == event_key:
                    return deepcopy(meal)
        return None

    async def append_meal(self, meal: MealRecord) -> MealRecord:
        async with self._lock:
            for existing in self.meals:
                if existing.event_key == meal.event_key:
                    return deepcopy(existing)
            self.append_calls += 1
            self.meals.append(deepcopy(meal))
            if self.raise_after_append_once:
                self.raise_after_append_once = False
                return deepcopy(meal)
            return deepcopy(meal)

    async def get_today_summary(self, local_date: date) -> DailySummary:
        async with self._lock:
            meals = [meal for meal in self.meals if meal.local_date == local_date]
        return DailySummary(
            local_date=local_date,
            kcal=sum(meal.kcal for meal in meals),
            protein_g=sum(meal.protein_g for meal in meals),
            fat_g=sum(meal.fat_g for meal in meals),
            carbs_g=sum(meal.carbs_g for meal in meals),
        )

    async def get_recent_meals(self, limit: int = 10) -> list[MealRecord]:
        async with self._lock:
            sorted_meals = sorted(self.meals, key=lambda meal: meal.logged_at, reverse=True)
            return deepcopy(sorted_meals[:limit])

    async def get_last_meal(self) -> MealRecord | None:
        recent = await self.get_recent_meals(limit=1)
        return recent[0] if recent else None

    async def update_meal(self, meal: MealRecord) -> MealRecord:
        async with self._lock:
            if meal.last_edit_event_key:
                for existing in self.meals:
                    if existing.last_edit_event_key == meal.last_edit_event_key:
                        return deepcopy(existing)
            for index, existing in enumerate(self.meals):
                if existing.meal_id == meal.meal_id:
                    self.meals[index] = deepcopy(meal)
                    return deepcopy(meal)
        raise KeyError(meal.meal_id)

    async def claim_report(self, report_key: str, local_date: date) -> bool:
        async with self._lock:
            if report_key in self.reports:
                return False
            self.reports[report_key] = ReportRecord(
                report_key=report_key,
                local_date=local_date,
                status="sending",
            )
            return True

    async def is_report_sent(self, report_key: str) -> bool:
        async with self._lock:
            return report_key in self.reports

    async def mark_report_sent(
        self, report_key: str, sent_at: datetime, telegram_message_id: int
    ) -> None:
        async with self._lock:
            record = self.reports[report_key]
            self.reports[report_key] = record.model_copy(
                update={
                    "status": "sent",
                    "sent_at": sent_at,
                    "telegram_message_id": telegram_message_id,
                }
            )

    async def clear_personal_data(self) -> DeleteResult:
        async with self._lock:
            result = DeleteResult(deleted_meals=len(self.meals), deleted_reports=len(self.reports))
            self.meals.clear()
            self.reports.clear()
            return result


class FakeTelegramTransport:
    def __init__(self) -> None:
        self.messages: list[tuple[int, str]] = []

    async def send_message(self, chat_id: int, text: str) -> int:
        self.messages.append((chat_id, text))
        return len(self.messages)
