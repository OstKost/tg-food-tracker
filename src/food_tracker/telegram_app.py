from __future__ import annotations

from aiogram import Bot, Dispatcher, F, Router
from aiogram.filters import Command
from aiogram.types import CallbackQuery, InlineKeyboardButton, InlineKeyboardMarkup, Message

from food_tracker.domain.errors import FoodTrackerError
from food_tracker.domain.models import DailySummary, MealRecord, RecordMealResult, RecordMealStatus
from food_tracker.ports import SheetsRepository
from food_tracker.services import MealService

HELP_TEXT = """Отправьте обычным сообщением, что вы съели, например:
«гречка 200 г, куриная грудка 150 г и огурец».

Команды:
/today — итог за сегодня
/history — последние 10 записей
/edit_last — исправить массу позиции в последней записи
/delete_me — удалить личные данные

Оценки приблизительные и не заменяют консультацию врача или нутрициолога."""


class AiogramTransport:
    def __init__(self, bot: Bot) -> None:
        self._bot = bot

    async def send_message(self, chat_id: int, text: str) -> int:
        message = await self._bot.send_message(chat_id=chat_id, text=text)
        return message.message_id


def create_dispatcher(
    service: MealService,
    repository: SheetsRepository,
    allowed_user_id: int,
) -> Dispatcher:
    router = Router(name="food-tracker")

    def allowed_message(message: Message) -> bool:
        return message.from_user is not None and message.from_user.id == allowed_user_id

    def allowed_callback(callback: CallbackQuery) -> bool:
        return callback.from_user.id == allowed_user_id

    async def reject_message(message: Message) -> None:
        await message.answer("Доступ закрыт.")

    @router.message(Command("start", "help"))
    async def help_handler(message: Message) -> None:
        if not allowed_message(message):
            await reject_message(message)
            return
        await message.answer(HELP_TEXT)

    @router.message(Command("today"))
    async def today_handler(message: Message) -> None:
        if not allowed_message(message):
            await reject_message(message)
            return
        try:
            await message.answer(format_daily(await service.today()))
        except FoodTrackerError:
            await message.answer("Не удалось прочитать итог. Попробуйте чуть позже.")

    @router.message(Command("history"))
    async def history_handler(message: Message) -> None:
        if not allowed_message(message):
            await reject_message(message)
            return
        try:
            meals = await service.history(limit=10)
        except FoodTrackerError:
            await message.answer("Не удалось прочитать историю. Попробуйте чуть позже.")
            return
        if not meals:
            await message.answer("История пока пуста.")
            return
        lines = ["Последние записи:"]
        lines.extend(
            f"{meal.logged_at:%d.%m %H:%M} — {meal.summary}: {meal.kcal:.0f} ккал"
            for meal in meals
        )
        await message.answer("\n".join(lines))

    @router.message(Command("edit_last"))
    async def edit_last_handler(message: Message) -> None:
        if not allowed_message(message):
            await reject_message(message)
            return
        try:
            meal = await service.begin_edit(allowed_user_id)
        except FoodTrackerError as exc:
            await message.answer(str(exc))
            return
        items = ", ".join(f"{item.name} — {item.amount_g:.0f} г" for item in meal.items)
        await message.answer(
            f"Исправляем: {items}. В течение 15 минут напишите, например: "
            "«курицы было 100 г»."
        )

    @router.message(Command("delete_me"))
    async def delete_handler(message: Message) -> None:
        if not allowed_message(message):
            await reject_message(message)
            return
        keyboard = InlineKeyboardMarkup(
            inline_keyboard=[
                [
                    InlineKeyboardButton(text="Удалить всё", callback_data="delete_confirm"),
                    InlineKeyboardButton(text="Отмена", callback_data="delete_cancel"),
                ]
            ]
        )
        await message.answer(
            "Удалить все записи Meals и ReportLog? Это действие нельзя отменить.",
            reply_markup=keyboard,
        )

    @router.callback_query(F.data == "delete_confirm")
    async def delete_confirm_handler(callback: CallbackQuery) -> None:
        if not allowed_callback(callback):
            await callback.answer("Доступ закрыт.", show_alert=True)
            return
        result = await repository.clear_personal_data()
        await callback.answer()
        if isinstance(callback.message, Message):
            await callback.message.edit_text(
                f"Данные удалены: записей — {result.deleted_meals}, отчётов — "
                f"{result.deleted_reports}."
            )

    @router.callback_query(F.data == "delete_cancel")
    async def delete_cancel_handler(callback: CallbackQuery) -> None:
        if not allowed_callback(callback):
            await callback.answer("Доступ закрыт.", show_alert=True)
            return
        await callback.answer()
        if isinstance(callback.message, Message):
            await callback.message.edit_text("Удаление отменено.")

    @router.callback_query(F.data.startswith("meal_confirm:"))
    async def meal_confirm_handler(callback: CallbackQuery) -> None:
        if not allowed_callback(callback):
            await callback.answer("Доступ закрыт.", show_alert=True)
            return
        token = (callback.data or "").partition(":")[2]
        try:
            result = await service.confirm_meal(allowed_user_id, token)
        except FoodTrackerError as exc:
            await callback.answer(str(exc), show_alert=True)
            return
        await callback.answer("Записано")
        if isinstance(callback.message, Message):
            await callback.message.edit_text(format_result(result))

    @router.message(F.text)
    async def text_handler(message: Message) -> None:
        if not allowed_message(message):
            await reject_message(message)
            return
        if message.text is None:
            return
        event_key = f"{message.chat.id}:{message.message_id}"
        try:
            if await service.has_edit_session(allowed_user_id):
                result = await service.correct_last(allowed_user_id, event_key, message.text)
            else:
                clarified = await service.answer_clarification(
                    allowed_user_id, event_key, message.text
                )
                if clarified is None:
                    result = await service.record_meal(
                        allowed_user_id,
                        event_key,
                        message.text,
                    )
                else:
                    result = clarified
        except FoodTrackerError as exc:
            await message.answer(str(exc))
            return
        await send_result(message, result)

    dispatcher = Dispatcher()
    dispatcher.include_router(router)
    return dispatcher


async def send_result(message: Message, result: RecordMealResult) -> None:
    if result.status == RecordMealStatus.NEEDS_CONFIRMATION:
        keyboard = InlineKeyboardMarkup(
            inline_keyboard=[
                [
                    InlineKeyboardButton(
                        text="Записать",
                        callback_data=f"meal_confirm:{result.confirmation_token}",
                    )
                ]
            ]
        )
        await message.answer(result.prompt or "Подтвердите запись.", reply_markup=keyboard)
        return
    if result.status == RecordMealStatus.NEEDS_CLARIFICATION:
        await message.answer(result.prompt or "Уточните размер порции.")
        return
    await message.answer(format_result(result))


def format_result(result: RecordMealResult) -> str:
    if result.meal is None or result.daily is None:
        return "Запись не готова."
    prefix = "Уже было записано" if result.status == RecordMealStatus.DUPLICATE else "Записал"
    return (
        f"{prefix}: {format_meal(result.meal)}\n\n"
        f"{format_daily(result.daily)}"
    )


def format_meal(meal: MealRecord) -> str:
    return (
        f"{meal.summary} — {meal.kcal:.0f} ккал; "
        f"Б/Ж/У {meal.protein_g:.1f}/{meal.fat_g:.1f}/{meal.carbs_g:.1f} г"
    )


def format_daily(summary: DailySummary) -> str:
    return (
        f"Сегодня: {summary.kcal:.0f} ккал; "
        f"Б/Ж/У {summary.protein_g:.1f}/{summary.fat_g:.1f}/{summary.carbs_g:.1f} г"
    )
