from __future__ import annotations

import asyncio
import contextlib
import logging
import os

from aiogram import Bot

from food_tracker.config import Settings
from food_tracker.infrastructure.gemini_analyzer import GeminiFoodAnalyzer
from food_tracker.infrastructure.google_sheets import GoogleSheetsRepository
from food_tracker.infrastructure.openai_analyzer import OpenAIFoodAnalyzer
from food_tracker.scheduler import DailyReportScheduler
from food_tracker.services import MealService
from food_tracker.telegram_app import AiogramTransport, create_dispatcher


async def run() -> None:
    settings = Settings()
    settings.validate_service_account_file()
    bot = Bot(token=settings.TELEGRAM_BOT_TOKEN.get_secret_value())
    analyzer: OpenAIFoodAnalyzer | GeminiFoodAnalyzer
    if settings.AI_PROVIDER == "openai":
        assert settings.OPENAI_API_KEY is not None
        analyzer = OpenAIFoodAnalyzer(
            api_key=settings.OPENAI_API_KEY.get_secret_value(),
            model=settings.OPENAI_MODEL,
            base_url=settings.OPENAI_BASE_URL,
            timeout_seconds=settings.OPENAI_TIMEOUT_SECONDS,
        )
    else:
        assert settings.GEMINI_API_KEY is not None
        analyzer = GeminiFoodAnalyzer(
            api_key=settings.GEMINI_API_KEY.get_secret_value(),
            model=settings.GEMINI_MODEL,
            base_url=settings.GEMINI_BASE_URL,
            timeout_seconds=settings.OPENAI_TIMEOUT_SECONDS,
        )
    repository = GoogleSheetsRepository(
        spreadsheet_id=settings.GOOGLE_SHEETS_SPREADSHEET_ID,
        service_account_file=str(settings.GOOGLE_SERVICE_ACCOUNT_FILE),
        allowed_user_id=settings.ALLOWED_TELEGRAM_USER_ID,
        timezone=settings.timezone,
    )
    await repository.validate_schema()
    service = MealService(repository, analyzer, settings.timezone)
    dispatcher = create_dispatcher(service, repository, settings.ALLOWED_TELEGRAM_USER_ID)
    scheduler = DailyReportScheduler(
        repository=repository,
        transport=AiogramTransport(bot),
        telegram_user_id=settings.ALLOWED_TELEGRAM_USER_ID,
        timezone=settings.timezone,
        report_time=settings.REPORT_TIME,
    )
    stop = asyncio.Event()
    scheduler_task = asyncio.create_task(scheduler.run_forever(stop), name="daily-report")
    try:
        await dispatcher.start_polling(bot, allowed_updates=dispatcher.resolve_used_update_types())
    finally:
        stop.set()
        scheduler_task.cancel()
        with contextlib.suppress(asyncio.CancelledError):
            await scheduler_task
        await analyzer.aclose()
        await bot.session.close()


def main() -> None:
    log_level = os.getenv("LOG_LEVEL", "INFO").upper()
    logging.basicConfig(
        level=getattr(logging, log_level, logging.INFO),
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
    )
    asyncio.run(run())


if __name__ == "__main__":
    main()
