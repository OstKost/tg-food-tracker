from __future__ import annotations

from collections.abc import Callable
from datetime import datetime
from typing import Any
from zoneinfo import ZoneInfo

import pytest

from food_tracker.domain.errors import SchemaError
from food_tracker.domain.models import MealAnalysis, MealRecord
from food_tracker.infrastructure.google_sheets import (
    DASHBOARD_FORMULAS,
    DASHBOARD_HEADERS,
    MEALS_HEADERS,
    REPORT_HEADERS,
    GoogleSheetsRepository,
)


class _Request:
    def __init__(self, callback: Callable[[], dict[str, Any]]) -> None:
        self._callback = callback

    def execute(self) -> dict[str, Any]:
        return self._callback()


class FakeSheetsApi:
    def __init__(self) -> None:
        self.timezone = "UTC"
        self.sheets: set[str] = set()
        self.ranges: dict[str, list[list[Any]]] = {}
        self.fail_append_after_write = False

    def spreadsheets(self) -> FakeSheetsApi:
        return self

    def values(self) -> FakeSheetsApi:
        return self

    def get(self, **kwargs: Any) -> _Request:
        if "range" in kwargs:
            range_name = str(kwargs["range"])
            return _Request(lambda: {"values": self.ranges.get(range_name, [])})
        return _Request(
            lambda: {
                "properties": {"timeZone": self.timezone},
                "sheets": [{"properties": {"title": title}} for title in sorted(self.sheets)],
            }
        )

    def batchUpdate(self, **kwargs: Any) -> _Request:
        body = kwargs["body"]

        def apply() -> dict[str, Any]:
            for request in body["requests"]:
                if "updateSpreadsheetProperties" in request:
                    self.timezone = request["updateSpreadsheetProperties"]["properties"][
                        "timeZone"
                    ]
                if "addSheet" in request:
                    self.sheets.add(request["addSheet"]["properties"]["title"])
            return {}

        return _Request(apply)

    def update(self, **kwargs: Any) -> _Request:
        range_name = str(kwargs["range"])
        values = kwargs["body"]["values"]
        return _Request(lambda: self._store(range_name, values))

    def append(self, **kwargs: Any) -> _Request:
        range_name = str(kwargs["range"])
        target = {"Meals!A:N": "Meals!A2:N", "ReportLog!A:E": "ReportLog!A2:E"}[
            range_name
        ]
        values = kwargs["body"]["values"]

        def apply() -> dict[str, Any]:
            self.ranges.setdefault(target, []).extend(values)
            if self.fail_append_after_write:
                self.fail_append_after_write = False
                raise TimeoutError("unknown outcome")
            return {}

        return _Request(apply)

    def clear(self, **kwargs: Any) -> _Request:
        range_name = str(kwargs["range"])
        return _Request(lambda: self._store(range_name, []))

    def _store(self, range_name: str, values: list[list[Any]]) -> dict[str, Any]:
        self.ranges[range_name] = values
        return {}


def repository(api: FakeSheetsApi) -> GoogleSheetsRepository:
    return GoogleSheetsRepository(
        "spreadsheet-id",
        "unused.json",
        42,
        ZoneInfo("Europe/Moscow"),
        service=api,
    )


@pytest.mark.asyncio
async def test_bootstrap_is_idempotent() -> None:
    api = FakeSheetsApi()
    repo = repository(api)

    await repo.bootstrap()
    await repo.bootstrap()

    assert api.sheets == {"Meals", "Dashboard", "ReportLog"}
    assert api.timezone == "Europe/Moscow"
    assert api.ranges["Meals!A1:N1"][0] == MEALS_HEADERS
    assert api.ranges["ReportLog!A1:E1"][0] == REPORT_HEADERS
    assert api.ranges["Dashboard!A1:E1"][0] == DASHBOARD_HEADERS
    assert api.ranges["Dashboard!A2:E2"][0] == DASHBOARD_FORMULAS


@pytest.mark.asyncio
async def test_bootstrap_rejects_damaged_existing_header() -> None:
    api = FakeSheetsApi()
    api.sheets = {"Meals", "Dashboard", "ReportLog"}
    api.ranges["Meals!A1:N1"] = [["wrong"]]
    repo = repository(api)

    with pytest.raises(SchemaError):
        await repo.bootstrap()


@pytest.mark.asyncio
async def test_append_timeout_is_reconciled_by_event_key(
    analysis: MealAnalysis, fixed_now: datetime
) -> None:
    api = FakeSheetsApi()
    repo = repository(api)
    await repo.bootstrap()
    api.fail_append_after_write = True
    meal = MealRecord(
        event_key="100:1",
        meal_id="meal-1",
        telegram_user_id=42,
        logged_at=fixed_now,
        local_date=fixed_now.date(),
        raw_text="еда",
        summary=analysis.summary,
        kcal=analysis.kcal,
        protein_g=analysis.protein_g,
        fat_g=analysis.fat_g,
        carbs_g=analysis.carbs_g,
        items=analysis.items,
        assumptions=analysis.assumptions,
    )

    stored = await repo.append_meal(meal)

    assert stored.event_key == "100:1"
    assert len(api.ranges["Meals!A2:N"]) == 1
