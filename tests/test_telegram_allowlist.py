from collections.abc import AsyncGenerator
from datetime import datetime
from typing import Any, TypeVar

import pytest
from aiogram import Bot
from aiogram.client.session.base import BaseSession
from aiogram.methods import TelegramMethod
from aiogram.types import Chat, Message, MessageEntity, Update, User

from food_tracker.domain.models import MealAnalysis
from food_tracker.infrastructure.fakes import FakeFoodAnalyzer, FakeSheetsRepository
from food_tracker.services import MealService
from food_tracker.telegram_app import create_dispatcher

TelegramType = TypeVar("TelegramType")


class CaptureSession(BaseSession):
    def __init__(self) -> None:
        super().__init__()
        self.methods: list[TelegramMethod[Any]] = []

    async def close(self) -> None:
        return None

    async def make_request(
        self,
        bot: Bot,
        method: TelegramMethod[TelegramType],
        timeout: int | None = None,  # noqa: ASYNC109
    ) -> TelegramType:
        del bot, timeout
        self.methods.append(method)
        result = Message(
            message_id=999,
            date=datetime.now(),
            chat=Chat(id=1, type="private"),
        )
        return result  # type: ignore[return-value]

    async def stream_content(
        self,
        url: str,
        headers: dict[str, Any] | None = None,
        timeout: int = 30,  # noqa: ASYNC109
        chunk_size: int = 65536,
        raise_for_status: bool = True,
    ) -> AsyncGenerator[bytes]:
        del url, headers, timeout, chunk_size, raise_for_status
        if False:
            yield b""


@pytest.mark.asyncio
async def test_unauthorized_text_never_reaches_analyzer_or_sheets(
    analysis: MealAnalysis, fixed_now: datetime, moscow: Any
) -> None:
    repository = FakeSheetsRepository()
    analyzer = FakeFoodAnalyzer(analysis)
    service = MealService(repository, analyzer, moscow, clock=lambda: fixed_now)
    dispatcher = create_dispatcher(service, repository, allowed_user_id=42)
    session = CaptureSession()
    bot = Bot("123456:TEST_TOKEN", session=session)
    message = Message(
        message_id=7,
        date=fixed_now,
        chat=Chat(id=99, type="private"),
        from_user=User(id=99, is_bot=False, first_name="Other"),
        text="яблоко",
    )

    await dispatcher.feed_update(bot, Update(update_id=1, message=message))

    assert analyzer.analyze_calls == 0
    assert repository.meals == []
    assert len(session.methods) == 1
    assert getattr(session.methods[0], "text", None) == "Доступ закрыт."
    await bot.session.close()


@pytest.mark.asyncio
async def test_allowed_user_gets_help_without_external_calls(
    analysis: MealAnalysis, fixed_now: datetime, moscow: Any
) -> None:
    repository = FakeSheetsRepository()
    analyzer = FakeFoodAnalyzer(analysis)
    service = MealService(repository, analyzer, moscow, clock=lambda: fixed_now)
    dispatcher = create_dispatcher(service, repository, allowed_user_id=42)
    session = CaptureSession()
    bot = Bot("123456:TEST_TOKEN", session=session)
    message = Message(
        message_id=8,
        date=fixed_now,
        chat=Chat(id=42, type="private"),
        from_user=User(id=42, is_bot=False, first_name="Owner"),
        text="/start",
        entities=[MessageEntity(type="bot_command", offset=0, length=6)],
    )

    await dispatcher.feed_update(bot, Update(update_id=2, message=message))

    assert analyzer.analyze_calls == 0
    assert repository.meals == []
    assert len(session.methods) == 1
    assert "Команды:" in str(getattr(session.methods[0], "text", ""))
    await bot.session.close()
