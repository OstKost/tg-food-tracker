from __future__ import annotations

import asyncio
import json
import logging
from typing import Any

import httpx
from pydantic import ValidationError

from food_tracker.domain.errors import AnalysisError
from food_tracker.domain.models import CorrectionAnalysis, MealAnalysis, MealItem, MealRecord
from food_tracker.infrastructure.analysis_schema import (
    SYSTEM_PROMPT,
    ProviderCorrection,
    ProviderMealAnalysis,
)

logger = logging.getLogger(__name__)


class GeminiFoodAnalyzer:
    """Gemini Interactions API adapter with structured JSON and local validation."""

    def __init__(
        self,
        api_key: str,
        model: str,
        *,
        base_url: str = "https://generativelanguage.googleapis.com/v1beta",
        timeout_seconds: float = 30,
        client: httpx.AsyncClient | None = None,
    ) -> None:
        self._model = model
        self._api_key = api_key
        self._owns_client = client is None
        self._client = client or httpx.AsyncClient(
            base_url=base_url.rstrip("/"),
            timeout=httpx.Timeout(timeout_seconds),
            headers={"x-goog-api-key": api_key},
        )

    async def analyze(self, text: str) -> MealAnalysis:
        payload = await self._request(
            schema=ProviderMealAnalysis.model_json_schema(),
            prompt=f"{SYSTEM_PROMPT}\nСообщение пользователя: {text}",
        )
        try:
            provider = ProviderMealAnalysis.model_validate_json(payload)
            return MealAnalysis(
                schema_version=provider.schema_version,
                items=[MealItem.model_validate(item.model_dump()) for item in provider.items],
                assumptions=provider.assumptions,
                confidence=provider.confidence,
                needs_clarification=provider.needs_clarification,
                clarification_question=provider.clarification_question or None,
                high_calorie_reason=provider.high_calorie_reason or None,
            )
        except ValidationError as exc:
            raise AnalysisError("Gemini returned data outside the meal contract") from exc

    async def analyze_correction(self, text: str, meal: MealRecord) -> CorrectionAnalysis:
        items = [
            {"item_id": item.item_id, "name": item.name, "amount_g": item.amount_g}
            for item in meal.items
        ]
        prompt = (
            "Определи, какую позицию пользователь исправляет, и её новую массу. "
            "Выбери item_id только из переданного списка. Если неоднозначно, запроси уточнение. "
            "Верни все поля JSON Schema. Список позиций: "
            + json.dumps(items, ensure_ascii=False)
            + f"\nИсправление пользователя: {text}"
        )
        payload = await self._request(
            schema=ProviderCorrection.model_json_schema(),
            prompt=prompt,
        )
        try:
            return CorrectionAnalysis.model_validate_json(payload)
        except ValidationError as exc:
            raise AnalysisError("Gemini returned data outside the correction contract") from exc

    async def aclose(self) -> None:
        if self._owns_client:
            await self._client.aclose()

    async def _request(self, *, schema: dict[str, Any], prompt: str) -> str:
        body = {
            "model": self._model,
            "input": prompt,
            "response_format": {
                "type": "text",
                "mime_type": "application/json",
                "schema": _gemini_schema(schema),
            },
        }
        if logger.isEnabledFor(logging.DEBUG):
            logger.debug(
                "Gemini request model=%s input=%r schema=%s",
                self._model,
                prompt,
                _schema_summary(body["response_format"]),
            )
            logger.debug(
                "Gemini response_format schema=%s",
                json.dumps(body["response_format"], ensure_ascii=False, sort_keys=True),
            )
        for attempt in range(2):
            try:
                response = await self._client.post(
                    "/interactions",
                    json=body,
                    headers={"x-goog-api-key": self._api_key},
                )
                if response.status_code in {408, 429} or response.status_code >= 500:
                    if attempt == 0:
                        await asyncio.sleep(0.25)
                        continue
                if response.is_error:
                    error_info = _provider_error_info(response)
                    logger.error(
                        "Gemini HTTP error status=%s model=%s path=/interactions "
                        "provider_code=%s provider_status=%s message=%s "
                        "response_format=%s",
                        response.status_code,
                        self._model,
                        error_info["code"],
                        error_info["status"],
                        error_info["message"],
                        _schema_summary(body["response_format"]),
                    )
                    detail = _provider_error_detail(response)
                    suffix = f": {detail}" if detail else ""
                    raise AnalysisError(
                        f"Gemini request failed with HTTP {response.status_code}{suffix}"
                    )
                data = response.json()
                if logger.isEnabledFor(logging.DEBUG):
                    logger.debug(
                        "Gemini response status=%s %s",
                        response.status_code,
                        _response_summary(data),
                    )
                if not isinstance(data, dict):
                    raise AnalysisError("Gemini returned an unreadable response")
                return self._extract_output_text(data)
            except (httpx.TimeoutException, httpx.TransportError) as exc:
                if attempt == 0:
                    await asyncio.sleep(0.25)
                    continue
                raise AnalysisError("Gemini request failed after one retry") from exc
            except (ValueError, KeyError, TypeError) as exc:
                raise AnalysisError("Gemini returned an unreadable response") from exc
        raise AnalysisError("Gemini request failed after one retry")

    @staticmethod
    def _extract_output_text(data: dict[str, Any]) -> str:
        direct = data.get("output_text")
        if isinstance(direct, str) and direct:
            return direct
        steps = data.get("steps")
        if not isinstance(steps, list):
            raise AnalysisError("Gemini response has no output")
        for step in reversed(steps):
            if not isinstance(step, dict) or step.get("type") != "model_output":
                continue
            content = step.get("content")
            if not isinstance(content, list):
                continue
            for part in content:
                if isinstance(part, dict) and part.get("type") == "text":
                    text = part.get("text")
                    if isinstance(text, str) and text:
                        return text
        raise AnalysisError("Gemini response has no JSON text")


def _gemini_schema(value: Any) -> Any:
    """Adapt Pydantic's portable JSON Schema to Gemini's documented subset."""

    definitions = value.get("$defs", {}) if isinstance(value, dict) else {}

    def convert(node: Any) -> Any:
        if isinstance(node, list):
            return [convert(item) for item in node]
        if not isinstance(node, dict):
            return node

        reference = node.get("$ref")
        if isinstance(reference, str) and reference.startswith("#/$defs/"):
            definition = definitions.get(reference.removeprefix("#/$defs/"))
            if definition is not None:
                return convert(definition)

        # Gemini's schema dialect is deliberately smaller than JSON Schema.
        # Domain bounds remain enforced by Pydantic after the response arrives.
        supported = {
            "type", "enum", "items", "properties", "required", "anyOf", "const"
        }
        converted: dict[str, Any] = {}
        for key, item in node.items():
            if key not in supported:
                continue
            if key == "properties" and isinstance(item, dict):
                converted[key] = {name: convert(schema) for name, schema in item.items()}
            else:
                converted[key] = convert(item)
        variants = converted.get("anyOf")
        if isinstance(variants, list) and len(variants) == 2:
            types = [variant.get("type") for variant in variants if isinstance(variant, dict)]
            if len(types) == 2 and all(isinstance(item, str) for item in types):
                converted.pop("anyOf")
                if set(types) == {"string", "null"}:
                    converted["type"] = "string"
                else:
                    converted["type"] = types
        constant = converted.pop("const", None)
        if constant is not None:
            converted["enum"] = [constant]
        return converted

    return convert(value)


def _provider_error_detail(response: httpx.Response) -> str | None:
    """Extract Google's short diagnostic without exposing the raw response."""

    try:
        data = response.json()
    except (ValueError, TypeError):
        return None
    if not isinstance(data, dict):
        return None
    error = data.get("error")
    if not isinstance(error, dict):
        return None
    message = error.get("message")
    return message.strip() if isinstance(message, str) and message.strip() else None


def _provider_error_info(response: httpx.Response) -> dict[str, str | None]:
    try:
        data = response.json()
    except (ValueError, TypeError):
        return {"code": None, "status": None, "message": None}
    error = data.get("error") if isinstance(data, dict) else None
    if not isinstance(error, dict):
        return {"code": None, "status": None, "message": None}
    return {
        "code": str(error["code"]) if "code" in error else None,
        "status": str(error["status"]) if "status" in error else None,
        "message": error.get("message") if isinstance(error.get("message"), str) else None,
    }


def _schema_summary(response_format: Any) -> str:
    if not isinstance(response_format, dict):
        return "invalid"
    schema = response_format.get("schema")
    if not isinstance(schema, dict):
        return "missing-schema"
    properties = schema.get("properties")
    names = sorted(properties) if isinstance(properties, dict) else []
    return f"type={response_format.get('type')!r} properties={names!r}"


def _response_summary(data: Any) -> str:
    """Describe a provider response without logging its generated text."""

    if not isinstance(data, dict):
        return f"body_type={type(data).__name__}"
    steps = data.get("steps")
    step_types = [
        step.get("type")
        for step in steps
        if isinstance(step, dict) and isinstance(step.get("type"), str)
    ] if isinstance(steps, list) else []
    return (
        f"keys={sorted(data)} output_text_present={isinstance(data.get('output_text'), str)} "
        f"step_types={step_types!r}"
    )
