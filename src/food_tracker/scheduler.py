from __future__ import annotations

import asyncio
from collections.abc import Callable
from datetime import UTC, datetime, time
from zoneinfo import ZoneInfo

from food_tracker.ports import SheetsRepository, TelegramTransport
from food_tracker.recommendations import daily_recommendation


class DailyReportScheduler:
    def __init__(
        self,
        repository: SheetsRepository,
        transport: TelegramTransport,
        telegram_user_id: int,
        timezone: ZoneInfo,
        report_time: time,
        *,
        clock: Callable[[], datetime] | None = None,
    ) -> None:
        self._repository = repository
        self._transport = transport
        self._telegram_user_id = telegram_user_id
        self._timezone = timezone
        self._report_time = report_time
        self._clock = clock or (lambda: datetime.now(tz=timezone))

    async def run_once(self, now: datetime | None = None) -> bool:
        current = (now or self._clock()).astimezone(self._timezone)
        if current.time().replace(tzinfo=None) < self._report_time:
            return False
        local_date = current.date()
        summary = await self._repository.get_today_summary(local_date)
        if not any(
            (summary.kcal, summary.protein_g, summary.fat_g, summary.carbs_g)
        ):
            return False
        report_key = f"daily:{self._telegram_user_id}:{local_date.isoformat()}"
        if not await self._repository.claim_report(report_key, local_date):
            return False

        recent = await self._repository.get_recent_meals(limit=100)
        today_meals = [meal for meal in recent if meal.local_date == local_date]
        recommendation = daily_recommendation(summary, today_meals)
        text = (
            f"Итог за {local_date:%d.%m.%Y}\n"
            f"Калории: {summary.kcal:.0f} ккал\n"
            f"Б/Ж/У: {summary.protein_g:.1f} / {summary.fat_g:.1f} / "
            f"{summary.carbs_g:.1f} г\n\n{recommendation}"
        )
        message_id = await self._transport.send_message(self._telegram_user_id, text)
        await self._repository.mark_report_sent(
            report_key,
            sent_at=current if current.tzinfo is not None else current.replace(tzinfo=UTC),
            telegram_message_id=message_id,
        )
        return True

    async def run_forever(self, stop: asyncio.Event) -> None:
        while not stop.is_set():
            await self.run_once()
            try:
                await asyncio.wait_for(stop.wait(), timeout=30)
            except TimeoutError:
                continue
