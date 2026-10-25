from __future__ import annotations

from datetime import time
from pathlib import Path
from typing import Literal
from zoneinfo import ZoneInfo, ZoneInfoNotFoundError

from pydantic import Field, SecretStr, field_validator, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Runtime configuration loaded from environment variables or ``.env``."""

    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
        case_sensitive=True,
    )

    TELEGRAM_BOT_TOKEN: SecretStr
    ALLOWED_TELEGRAM_USER_ID: int = Field(gt=0)
    AI_PROVIDER: Literal["openai", "gemini"] = "openai"
    OPENAI_API_KEY: SecretStr | None = None
    OPENAI_MODEL: str = "gpt-5.4-mini-2026-03-17"
    OPENAI_BASE_URL: str = "https://api.openai.com/v1"
    GEMINI_API_KEY: SecretStr | None = None
    GEMINI_MODEL: str = "gemini-3.8-flash"
    GEMINI_BASE_URL: str = "https://generativelanguage.googleapis.com/v1beta"
    GOOGLE_SHEETS_SPREADSHEET_ID: str = Field(min_length=10)
    GOOGLE_SERVICE_ACCOUNT_FILE: Path
    APP_TIMEZONE: str = "Europe/Moscow"
    REPORT_TIME: time = time(hour=21)
    OPENAI_TIMEOUT_SECONDS: float = Field(default=30.0, gt=0, le=120)

    @field_validator("APP_TIMEZONE")
    @classmethod
    def timezone_must_exist(cls, value: str) -> str:
        try:
            ZoneInfo(value)
        except ZoneInfoNotFoundError as exc:
            raise ValueError(f"unknown timezone: {value}") from exc
        return value

    @model_validator(mode="after")
    def selected_provider_requires_a_key(self) -> Settings:
        selected_key = self.OPENAI_API_KEY if self.AI_PROVIDER == "openai" else self.GEMINI_API_KEY
        if selected_key is None or not selected_key.get_secret_value().strip():
            variable = "OPENAI_API_KEY" if self.AI_PROVIDER == "openai" else "GEMINI_API_KEY"
            raise ValueError(f"{variable} is required when AI_PROVIDER={self.AI_PROVIDER}")
        return self

    @property
    def timezone(self) -> ZoneInfo:
        return ZoneInfo(self.APP_TIMEZONE)

    def validate_service_account_file(self) -> None:
        if not self.GOOGLE_SERVICE_ACCOUNT_FILE.is_file():
            raise ValueError(
                "GOOGLE_SERVICE_ACCOUNT_FILE does not point to a readable JSON file: "
                f"{self.GOOGLE_SERVICE_ACCOUNT_FILE}"
            )
