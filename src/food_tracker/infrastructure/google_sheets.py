from __future__ import annotations

import asyncio
import json
from collections.abc import Callable
from datetime import date, datetime
from typing import Any, TypeVar
from zoneinfo import ZoneInfo

from google.oauth2.service_account import Credentials
from googleapiclient.discovery import build

from food_tracker.domain.errors import RepositoryError, SchemaError
from food_tracker.domain.models import (
    DailySummary,
    DeleteResult,
    MealItem,
    MealRecord,
    ReportRecord,
)

MEALS_HEADERS = [
    "event_key",
    "meal_id",
    "logged_at",
    "local_date",
    "raw_text",
    "summary",
    "kcal",
    "protein_g",
    "fat_g",
    "carbs_g",
    "items_json",
    "assumptions",
    "revision",
    "last_edit_event_key",
]
REPORT_HEADERS = ["report_key", "local_date", "status", "sent_at", "telegram_message_id"]
DASHBOARD_HEADERS = ["date", "kcal", "protein_g", "fat_g", "carbs_g"]
DASHBOARD_FORMULAS = [
    '=TEXT(TODAY(),"yyyy-mm-dd")',
    "=SUMIF(Meals!D2:D10000,A2,Meals!G2:G10000)",
    "=SUMIF(Meals!D2:D10000,A2,Meals!H2:H10000)",
    "=SUMIF(Meals!D2:D10000,A2,Meals!I2:I10000)",
    "=SUMIF(Meals!D2:D10000,A2,Meals!J2:J10000)",
]
SCOPES = ["https://www.googleapis.com/auth/spreadsheets"]

T = TypeVar("T")


class GoogleSheetsRepository:
    """Async facade over the synchronous Google Sheets API client."""

    def __init__(
        self,
        spreadsheet_id: str,
        service_account_file: str,
        allowed_user_id: int,
        timezone: ZoneInfo,
        *,
        service: Any | None = None,
    ) -> None:
        self._spreadsheet_id = spreadsheet_id
        self._allowed_user_id = allowed_user_id
        self._timezone = timezone
        if service is None:
            credentials = Credentials.from_service_account_file(  # type: ignore[no-untyped-call]
                service_account_file,
                scopes=SCOPES,
            )
            service = build("sheets", "v4", credentials=credentials, cache_discovery=False)
        self._service = service
        self._lock = asyncio.Lock()

    async def bootstrap(self) -> None:
        async with self._lock:
            await self._thread(self._bootstrap_sync)

    async def validate_schema(self) -> None:
        async with self._lock:
            await self._thread(self._validate_schema_sync)

    async def find_by_event_key(self, event_key: str) -> MealRecord | None:
        async with self._lock:
            return await self._thread(self._find_by_event_key_sync, event_key)

    async def append_meal(self, meal: MealRecord) -> MealRecord:
        async with self._lock:
            existing = await self._thread(self._find_by_event_key_sync, meal.event_key)
            if existing is not None:
                return existing
            try:
                await self._thread(
                    self._append_values_sync,
                    "Meals!A:N",
                    [self._meal_to_row(meal)],
                )
            except Exception as exc:
                # The append may have succeeded even when the client observed a timeout.
                reconciled = await self._thread(self._find_by_event_key_sync, meal.event_key)
                if reconciled is not None:
                    return reconciled
                raise RepositoryError("Google Sheets append failed") from exc
            return meal

    async def get_today_summary(self, local_date: date) -> DailySummary:
        async with self._lock:
            if local_date == datetime.now(tz=self._timezone).date():
                try:
                    dashboard = await self._thread(self._dashboard_summary_sync)
                except SchemaError:
                    # Sheets may briefly return empty/stale formula values after an append.
                    # The source rows are authoritative and avoid failing the user request.
                    dashboard = None
                if dashboard is not None and dashboard.local_date == local_date:
                    return dashboard
            meals = await self._thread(self._read_meals_sync)
        selected = [meal for meal in meals if meal.local_date == local_date]
        return DailySummary(
            local_date=local_date,
            kcal=sum(meal.kcal for meal in selected),
            protein_g=sum(meal.protein_g for meal in selected),
            fat_g=sum(meal.fat_g for meal in selected),
            carbs_g=sum(meal.carbs_g for meal in selected),
        )

    async def get_recent_meals(self, limit: int = 10) -> list[MealRecord]:
        async with self._lock:
            meals = await self._thread(self._read_meals_sync)
        return sorted(meals, key=lambda meal: meal.logged_at, reverse=True)[:limit]

    async def get_last_meal(self) -> MealRecord | None:
        recent = await self.get_recent_meals(limit=1)
        return recent[0] if recent else None

    async def update_meal(self, meal: MealRecord) -> MealRecord:
        async with self._lock:
            if meal.last_edit_event_key:
                existing = await self._thread(
                    self._find_by_event_key_sync, meal.last_edit_event_key
                )
                if existing is not None:
                    return existing
            rows = await self._thread(self._get_values_sync, "Meals!A2:N")
            for offset, row in enumerate(rows, start=2):
                if len(row) > 1 and str(row[1]) == meal.meal_id:
                    await self._thread(
                        self._update_values_sync,
                        f"Meals!A{offset}:N{offset}",
                        [self._meal_to_row(meal)],
                        "RAW",
                    )
                    return meal
        raise RepositoryError(f"meal not found: {meal.meal_id}")

    async def is_report_sent(self, report_key: str) -> bool:
        async with self._lock:
            record = await self._thread(self._find_report_sync, report_key)
        return record is not None

    async def claim_report(self, report_key: str, local_date: date) -> bool:
        async with self._lock:
            if await self._thread(self._find_report_sync, report_key) is not None:
                return False
            await self._thread(
                self._append_values_sync,
                "ReportLog!A:E",
                [[report_key, local_date.isoformat(), "sending", "", ""]],
            )
            return True

    async def mark_report_sent(
        self, report_key: str, sent_at: datetime, telegram_message_id: int
    ) -> None:
        async with self._lock:
            rows = await self._thread(self._get_values_sync, "ReportLog!A2:E")
            for offset, row in enumerate(rows, start=2):
                if row and str(row[0]) == report_key:
                    await self._thread(
                        self._update_values_sync,
                        f"ReportLog!C{offset}:E{offset}",
                        [["sent", sent_at.isoformat(), telegram_message_id]],
                        "RAW",
                    )
                    return
        raise RepositoryError(f"report claim not found: {report_key}")

    async def clear_personal_data(self) -> DeleteResult:
        async with self._lock:
            meals = await self._thread(self._get_values_sync, "Meals!A2:N")
            reports = await self._thread(self._get_values_sync, "ReportLog!A2:E")
            await self._thread(self._clear_values_sync, "Meals!A2:N")
            await self._thread(self._clear_values_sync, "ReportLog!A2:E")
        return DeleteResult(deleted_meals=len(meals), deleted_reports=len(reports))

    async def _thread(self, function: Callable[..., T], *args: Any) -> T:
        return await asyncio.to_thread(function, *args)

    def _bootstrap_sync(self) -> None:
        metadata = (
            self._service.spreadsheets()
            .get(spreadsheetId=self._spreadsheet_id, fields="properties,sheets.properties")
            .execute()
        )
        sheets = {entry["properties"]["title"] for entry in metadata.get("sheets", [])}
        missing = [name for name in ("Meals", "Dashboard", "ReportLog") if name not in sheets]
        requests: list[dict[str, Any]] = [
            {
                "updateSpreadsheetProperties": {
                    "properties": {"timeZone": self._timezone.key},
                    "fields": "timeZone",
                }
            }
        ]
        requests.extend({"addSheet": {"properties": {"title": name}}} for name in missing)
        if requests:
            (
                self._service.spreadsheets()
                .batchUpdate(
                    spreadsheetId=self._spreadsheet_id,
                    body={"requests": requests},
                )
                .execute()
            )
        self._ensure_row_sync("Meals!A1:N1", MEALS_HEADERS, "Meals")
        self._ensure_row_sync("ReportLog!A1:E1", REPORT_HEADERS, "ReportLog")
        self._ensure_row_sync("Dashboard!A1:E1", DASHBOARD_HEADERS, "Dashboard")
        self._ensure_row_sync("Dashboard!A2:E2", DASHBOARD_FORMULAS, "Dashboard formulas")
        self._validate_schema_sync()

    def _validate_schema_sync(self) -> None:
        metadata = (
            self._service.spreadsheets()
            .get(
                spreadsheetId=self._spreadsheet_id,
                fields="properties.timeZone,sheets.properties.title",
            )
            .execute()
        )
        timezone = metadata.get("properties", {}).get("timeZone")
        if timezone != self._timezone.key:
            raise SchemaError(
                f"spreadsheet timezone is {timezone!r}; expected {self._timezone.key!r}"
            )
        sheets = {entry["properties"]["title"] for entry in metadata.get("sheets", [])}
        required = {"Meals", "Dashboard", "ReportLog"}
        if not required.issubset(sheets):
            raise SchemaError(f"missing sheets: {sorted(required - sheets)}")
        self._expect_row_sync("Meals!A1:N1", MEALS_HEADERS, "Meals")
        self._expect_row_sync("ReportLog!A1:E1", REPORT_HEADERS, "ReportLog")
        self._expect_row_sync("Dashboard!A1:E1", DASHBOARD_HEADERS, "Dashboard")
        self._expect_row_sync("Dashboard!A2:E2", DASHBOARD_FORMULAS, "Dashboard formulas")

    def _ensure_row_sync(self, range_name: str, expected: list[str], label: str) -> None:
        current = self._get_values_sync(range_name, value_render_option="FORMULA")
        if not current or not current[0]:
            self._update_values_sync(range_name, [expected], "USER_ENTERED")
            return
        if [str(value) for value in current[0]] != expected:
            raise SchemaError(f"{label} schema is damaged; expected {expected}")

    def _expect_row_sync(self, range_name: str, expected: list[str], label: str) -> None:
        current = self._get_values_sync(range_name, value_render_option="FORMULA")
        row = [str(value) for value in current[0]] if current else []
        if row != expected:
            raise SchemaError(f"{label} schema is damaged; expected {expected}")

    def _read_meals_sync(self) -> list[MealRecord]:
        records: list[MealRecord] = []
        for row in self._get_values_sync("Meals!A2:N"):
            if row and str(row[0]).strip():
                records.append(self._row_to_meal(row))
        return records

    def _find_by_event_key_sync(self, event_key: str) -> MealRecord | None:
        for meal in self._read_meals_sync():
            if meal.event_key == event_key or meal.last_edit_event_key == event_key:
                return meal
        return None

    def _dashboard_summary_sync(self) -> DailySummary:
        rows = self._get_values_sync("Dashboard!A2:E2", value_render_option="UNFORMATTED_VALUE")
        if not rows or len(rows[0]) < 5:
            raise SchemaError("Dashboard has no calculated summary; check formulas")
        row = rows[0]
        try:
            return DailySummary(
                local_date=date.fromisoformat(str(row[0])),
                kcal=float(row[1]),
                protein_g=float(row[2]),
                fat_g=float(row[3]),
                carbs_g=float(row[4]),
            )
        except (TypeError, ValueError) as exc:
            raise SchemaError("Dashboard contains invalid calculated values") from exc

    def _find_report_sync(self, report_key: str) -> ReportRecord | None:
        for row in self._get_values_sync("ReportLog!A2:E"):
            if not row or str(row[0]) != report_key:
                continue
            sent_at = self._cell(row, 3)
            message_id = self._cell(row, 4)
            return ReportRecord(
                report_key=report_key,
                local_date=date.fromisoformat(str(self._cell(row, 1))),
                status=str(self._cell(row, 2)),
                sent_at=datetime.fromisoformat(str(sent_at)) if sent_at else None,
                telegram_message_id=(
                    int(message_id) if isinstance(message_id, (str, int, float)) else None
                ),
            )
        return None

    def _get_values_sync(
        self, range_name: str, *, value_render_option: str = "UNFORMATTED_VALUE"
    ) -> list[list[Any]]:
        result = (
            self._service.spreadsheets()
            .values()
            .get(
                spreadsheetId=self._spreadsheet_id,
                range=range_name,
                valueRenderOption=value_render_option,
            )
            .execute()
        )
        values = result.get("values", [])
        return values if isinstance(values, list) else []

    def _append_values_sync(self, range_name: str, values: list[list[Any]]) -> None:
        (
            self._service.spreadsheets()
            .values()
            .append(
                spreadsheetId=self._spreadsheet_id,
                range=range_name,
                valueInputOption="RAW",
                insertDataOption="INSERT_ROWS",
                body={"values": values},
            )
            .execute()
        )

    def _update_values_sync(
        self, range_name: str, values: list[list[Any]], input_option: str
    ) -> None:
        (
            self._service.spreadsheets()
            .values()
            .update(
                spreadsheetId=self._spreadsheet_id,
                range=range_name,
                valueInputOption=input_option,
                body={"values": values},
            )
            .execute()
        )

    def _clear_values_sync(self, range_name: str) -> None:
        (
            self._service.spreadsheets()
            .values()
            .clear(spreadsheetId=self._spreadsheet_id, range=range_name, body={})
            .execute()
        )

    def _meal_to_row(self, meal: MealRecord) -> list[Any]:
        return [
            meal.event_key,
            meal.meal_id,
            meal.logged_at.isoformat(),
            meal.local_date.isoformat(),
            meal.raw_text,
            meal.summary,
            meal.kcal,
            meal.protein_g,
            meal.fat_g,
            meal.carbs_g,
            json.dumps([item.model_dump(mode="json") for item in meal.items], ensure_ascii=False),
            json.dumps(meal.assumptions, ensure_ascii=False),
            meal.revision,
            meal.last_edit_event_key or "",
        ]

    def _row_to_meal(self, row: list[Any]) -> MealRecord:
        padded = [*row, *([""] * (14 - len(row)))]
        try:
            return MealRecord(
                event_key=str(padded[0]),
                meal_id=str(padded[1]),
                telegram_user_id=self._allowed_user_id,
                logged_at=datetime.fromisoformat(str(padded[2])),
                local_date=date.fromisoformat(str(padded[3])),
                raw_text=str(padded[4]),
                summary=str(padded[5]),
                kcal=float(padded[6]),
                protein_g=float(padded[7]),
                fat_g=float(padded[8]),
                carbs_g=float(padded[9]),
                items=[MealItem.model_validate(item) for item in json.loads(str(padded[10]))],
                assumptions=list(json.loads(str(padded[11]))),
                revision=int(padded[12]),
                last_edit_event_key=str(padded[13]) or None,
            )
        except (TypeError, ValueError, json.JSONDecodeError) as exc:
            key = str(padded[0]) or "<empty>"
            raise SchemaError(f"invalid Meals row for event_key {key}") from exc

    @staticmethod
    def _cell(row: list[Any], index: int) -> Any | None:
        return row[index] if index < len(row) else None
