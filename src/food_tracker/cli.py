from __future__ import annotations

import asyncio

from food_tracker.config import Settings
from food_tracker.infrastructure.google_sheets import GoogleSheetsRepository


async def setup() -> None:
    settings = Settings()
    settings.validate_service_account_file()
    repository = GoogleSheetsRepository(
        spreadsheet_id=settings.GOOGLE_SHEETS_SPREADSHEET_ID,
        service_account_file=str(settings.GOOGLE_SERVICE_ACCOUNT_FILE),
        allowed_user_id=settings.ALLOWED_TELEGRAM_USER_ID,
        timezone=settings.timezone,
    )
    await repository.bootstrap()
    print("Google Sheets schema is ready.")


def main() -> None:
    asyncio.run(setup())


if __name__ == "__main__":
    main()
