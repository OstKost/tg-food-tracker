from __future__ import annotations

import asyncio
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, date, datetime, timedelta
from uuid import uuid4
from zoneinfo import ZoneInfo

from food_tracker.domain.errors import EditSessionExpired, MealValidationError, NoMealToEdit
from food_tracker.domain.models import (
    DailySummary,
    MealAnalysis,
    MealItem,
    MealRecord,
    RecordMealResult,
    RecordMealStatus,
)
from food_tracker.ports import FoodAnalyzer, SheetsRepository


@dataclass(slots=True)
class _PendingMeal:
    event_key: str
    user_id: int
    raw_text: str
    analysis: MealAnalysis
    expires_at: datetime


@dataclass(slots=True)
class _Clarification:
    event_key: str
    user_id: int
    raw_text: str
    expires_at: datetime


@dataclass(slots=True)
class _EditSession:
    meal_id: str
    expires_at: datetime


class MealService:
    """Application use cases, independent of Telegram and provider SDKs."""

    def __init__(
        self,
        repository: SheetsRepository,
        analyzer: FoodAnalyzer,
        timezone: ZoneInfo,
        *,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        self._repository = repository
        self._analyzer = analyzer
        self._timezone = timezone
        self._clock = clock or (lambda: datetime.now(tz=timezone))
        self._state_lock = asyncio.Lock()
        self._inflight: dict[str, asyncio.Future[RecordMealResult]] = {}
        self._pending: dict[str, _PendingMeal] = {}
        self._clarifications: dict[int, _Clarification] = {}
        self._edits: dict[int, _EditSession] = {}

    async def record_meal(self, user_id: int, event_key: str, text: str) -> RecordMealResult:
        existing = await self._repository.find_by_event_key(event_key)
        if existing is not None:
            return await self._duplicate_result(existing)

        future, owner = await self._join_or_create(event_key)
        if not owner:
            return await asyncio.shield(future)

        try:
            result = await self._record_new(user_id, event_key, text)
            future.set_result(result)
            return result
        except BaseException as exc:
            if not future.done():
                future.set_exception(exc)
                future.exception()
            raise
        finally:
            async with self._state_lock:
                self._inflight.pop(event_key, None)

    async def answer_clarification(
        self, user_id: int, answer_event_key: str, answer: str
    ) -> RecordMealResult | None:
        async with self._state_lock:
            pending = self._clarifications.get(user_id)
            if pending is None:
                return None
            if self._now() > pending.expires_at:
                self._clarifications.pop(user_id, None)
                return None
            self._clarifications.pop(user_id, None)
        combined = f"{pending.raw_text}\nУточнение пользователя: {answer}"
        return await self.record_meal(user_id, pending.event_key, combined)

    async def confirm_meal(self, user_id: int, token: str) -> RecordMealResult:
        async with self._state_lock:
            pending = self._pending.pop(token, None)
        if pending is None or pending.user_id != user_id or self._now() > pending.expires_at:
            raise MealValidationError("Подтверждение устарело. Отправьте приём пищи ещё раз.")

        existing = await self._repository.find_by_event_key(pending.event_key)
        if existing is not None:
            return await self._duplicate_result(existing)
        meal = self._build_meal(
            user_id=pending.user_id,
            event_key=pending.event_key,
            raw_text=pending.raw_text,
            analysis=pending.analysis,
        )
        stored = await self._repository.append_meal(meal)
        daily = await self._repository.get_today_summary(stored.local_date)
        return RecordMealResult(status=RecordMealStatus.STORED, meal=stored, daily=daily)

    async def begin_edit(self, user_id: int) -> MealRecord:
        meal = await self._repository.get_last_meal()
        if meal is None:
            raise NoMealToEdit("Пока нет записей, которые можно исправить.")
        async with self._state_lock:
            self._edits[user_id] = _EditSession(
                meal_id=meal.meal_id,
                expires_at=self._now() + timedelta(minutes=15),
            )
        return meal

    async def has_edit_session(self, user_id: int) -> bool:
        async with self._state_lock:
            session = self._edits.get(user_id)
            if session is None:
                return False
            if self._now() > session.expires_at:
                self._edits.pop(user_id, None)
                return False
            return True

    async def correct_last(
        self, user_id: int, edit_event_key: str, correction_text: str
    ) -> RecordMealResult:
        duplicate = await self._repository.find_by_event_key(edit_event_key)
        if duplicate is not None:
            return await self._duplicate_result(duplicate)

        async with self._state_lock:
            session = self._edits.get(user_id)
            if session is None or self._now() > session.expires_at:
                self._edits.pop(user_id, None)
                raise EditSessionExpired("Режим исправления истёк. Снова вызовите /edit_last.")

        meal = await self._repository.get_last_meal()
        if meal is None or meal.meal_id != session.meal_id:
            raise NoMealToEdit("Последняя запись изменилась. Снова вызовите /edit_last.")

        correction = await self._analyzer.analyze_correction(correction_text, meal)
        if correction.needs_clarification:
            return RecordMealResult(
                status=RecordMealStatus.NEEDS_CLARIFICATION,
                meal=meal,
                prompt=correction.clarification_question,
            )

        items: list[MealItem] = []
        found = False
        for item in meal.items:
            if item.item_id != correction.item_id:
                items.append(item)
                continue
            found = True
            ratio = correction.new_amount_g / item.amount_g
            items.append(
                item.model_copy(
                    update={
                        "amount_g": correction.new_amount_g,
                        "kcal": item.kcal * ratio,
                        "protein_g": item.protein_g * ratio,
                        "fat_g": item.fat_g * ratio,
                        "carbs_g": item.carbs_g * ratio,
                    }
                )
            )
        if not found:
            raise MealValidationError("Не удалось однозначно найти позицию для исправления.")

        updated = meal.model_copy(
            update={
                "items": items,
                "kcal": sum(item.kcal for item in items),
                "protein_g": sum(item.protein_g for item in items),
                "fat_g": sum(item.fat_g for item in items),
                "carbs_g": sum(item.carbs_g for item in items),
                "revision": meal.revision + 1,
                "last_edit_event_key": edit_event_key,
            }
        )
        updated = MealRecord.model_validate(updated.model_dump())
        stored = await self._repository.update_meal(updated)
        async with self._state_lock:
            self._edits.pop(user_id, None)
        daily = await self._repository.get_today_summary(stored.local_date)
        return RecordMealResult(status=RecordMealStatus.STORED, meal=stored, daily=daily)

    async def today(self, local_date: date | None = None) -> DailySummary:
        return await self._repository.get_today_summary(local_date or self._now().date())

    async def history(self, limit: int = 10) -> list[MealRecord]:
        return await self._repository.get_recent_meals(limit=limit)

    async def _join_or_create(
        self, event_key: str
    ) -> tuple[asyncio.Future[RecordMealResult], bool]:
        async with self._state_lock:
            current = self._inflight.get(event_key)
            if current is not None:
                return current, False
            future = asyncio.get_running_loop().create_future()
            self._inflight[event_key] = future
            return future, True

    async def _record_new(self, user_id: int, event_key: str, text: str) -> RecordMealResult:
        analysis = await self._analyzer.analyze(text)
        if analysis.needs_clarification:
            async with self._state_lock:
                self._clarifications[user_id] = _Clarification(
                    event_key=event_key,
                    user_id=user_id,
                    raw_text=text,
                    expires_at=self._now() + timedelta(minutes=15),
                )
            return RecordMealResult(
                status=RecordMealStatus.NEEDS_CLARIFICATION,
                prompt=analysis.clarification_question,
            )

        if analysis.kcal > 10_000:
            raise MealValidationError(
                "Получилось больше 10 000 ккал. Уточните продукты и размеры порций."
            )
        if analysis.kcal > 5_000:
            if not analysis.high_calorie_reason:
                raise MealValidationError(
                    "Получилось больше 5 000 ккал, но причина неясна. Уточните порцию."
                )
            token = uuid4().hex
            async with self._state_lock:
                self._pending[token] = _PendingMeal(
                    event_key=event_key,
                    user_id=user_id,
                    raw_text=text,
                    analysis=analysis,
                    expires_at=self._now() + timedelta(minutes=15),
                )
            return RecordMealResult(
                status=RecordMealStatus.NEEDS_CONFIRMATION,
                prompt=(
                    f"Получилось {analysis.kcal:.0f} ккал. Причина: "
                    f"{analysis.high_calorie_reason}. Записать?"
                ),
                confirmation_token=token,
            )

        meal = self._build_meal(user_id, event_key, text, analysis)
        stored = await self._repository.append_meal(meal)
        daily = await self._repository.get_today_summary(stored.local_date)
        return RecordMealResult(status=RecordMealStatus.STORED, meal=stored, daily=daily)

    def _build_meal(
        self, user_id: int, event_key: str, raw_text: str, analysis: MealAnalysis
    ) -> MealRecord:
        now = self._now()
        return MealRecord(
            event_key=event_key,
            meal_id=str(uuid4()),
            telegram_user_id=user_id,
            logged_at=now,
            local_date=now.date(),
            raw_text=raw_text,
            summary=analysis.summary,
            kcal=analysis.kcal,
            protein_g=analysis.protein_g,
            fat_g=analysis.fat_g,
            carbs_g=analysis.carbs_g,
            items=analysis.items,
            assumptions=analysis.assumptions,
        )

    async def _duplicate_result(self, meal: MealRecord) -> RecordMealResult:
        daily = await self._repository.get_today_summary(meal.local_date)
        return RecordMealResult(status=RecordMealStatus.DUPLICATE, meal=meal, daily=daily)

    def _now(self) -> datetime:
        now = self._clock()
        if now.tzinfo is None or now.utcoffset() is None:
            now = now.replace(tzinfo=UTC)
        return now.astimezone(self._timezone)

