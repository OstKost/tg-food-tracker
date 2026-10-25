from __future__ import annotations

from datetime import date, datetime
from typing import Protocol

from food_tracker.domain.models import (
    CorrectionAnalysis,
    DailySummary,
    DeleteResult,
    MealAnalysis,
    MealRecord,
)


class FoodAnalyzer(Protocol):
    async def analyze(self, text: str) -> MealAnalysis: ...

    async def analyze_correction(self, text: str, meal: MealRecord) -> CorrectionAnalysis: ...


class SheetsRepository(Protocol):
    async def find_by_event_key(self, event_key: str) -> MealRecord | None: ...

    async def append_meal(self, meal: MealRecord) -> MealRecord: ...

    async def get_today_summary(self, local_date: date) -> DailySummary: ...

    async def get_recent_meals(self, limit: int = 10) -> list[MealRecord]: ...

    async def get_last_meal(self) -> MealRecord | None: ...

    async def update_meal(self, meal: MealRecord) -> MealRecord: ...

    async def is_report_sent(self, report_key: str) -> bool: ...

    async def claim_report(self, report_key: str, local_date: date) -> bool: ...

    async def mark_report_sent(
        self, report_key: str, sent_at: datetime, telegram_message_id: int
    ) -> None: ...

    async def clear_personal_data(self) -> DeleteResult: ...


class TelegramTransport(Protocol):
    async def send_message(self, chat_id: int, text: str) -> int: ...
