import pytest
from pydantic import ValidationError

from food_tracker.config import Settings

TEST_TELEGRAM_TOKEN = "test-token"  # noqa: S105 - inert Settings fixture value


def test_gemini_provider_accepts_only_gemini_key() -> None:
    settings = Settings(
        _env_file=None,
        TELEGRAM_BOT_TOKEN=TEST_TELEGRAM_TOKEN,
        ALLOWED_TELEGRAM_USER_ID=42,
        GOOGLE_SHEETS_SPREADSHEET_ID="spreadsheet-123",
        GOOGLE_SERVICE_ACCOUNT_FILE="service-account.json",
        AI_PROVIDER="gemini",
        GEMINI_API_KEY="gemini-key",
    )

    assert settings.AI_PROVIDER == "gemini"
    assert settings.OPENAI_API_KEY is None


def test_selected_provider_requires_its_key() -> None:
    with pytest.raises(ValidationError, match="GEMINI_API_KEY"):
        Settings(
            _env_file=None,
            TELEGRAM_BOT_TOKEN=TEST_TELEGRAM_TOKEN,
            ALLOWED_TELEGRAM_USER_ID=42,
            GOOGLE_SHEETS_SPREADSHEET_ID="spreadsheet-123",
            GOOGLE_SERVICE_ACCOUNT_FILE="service-account.json",
            AI_PROVIDER="gemini",
        )
