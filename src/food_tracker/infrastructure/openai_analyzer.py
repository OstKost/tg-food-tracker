from __future__ import annotations

import asyncio
import json
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


class OpenAIFoodAnalyzer:
    """OpenAI Responses and Chat Completions API adapter using strict JSON Schema."""

    def __init__(
        self,
        api_key: str,
        model: str,
        *,
        base_url: str = "https://api.openai.com/v1",
        timeout_seconds: float = 30,
        client: httpx.AsyncClient | None = None,
    ) -> None:
        self._model = model
        self._owns_client = client is None
        self._client = client or httpx.AsyncClient(
            base_url=base_url.rstrip("/"),
            timeout=httpx.Timeout(timeout_seconds),
            headers={"Authorization": f"Bearer {api_key}"},
        )
        self._use_chat_completions = False

    async def analyze(self, text: str) -> MealAnalysis:
        payload = await self._request(
            name="meal_analysis",
            schema=ProviderMealAnalysis.model_json_schema(),
            instructions=SYSTEM_PROMPT,
            user_text=text,
        )
        try:
            provider = ProviderMealAnalysis.model_validate_json(payload)
            return MealAnalysis(
                schema_version=provider.schema_version,
                items=[MealItem.model_validate(item.model_dump()) for item in provider.items],
                assumptions=provider.assumptions,
                confidence=provider.confidence,
                needs_clarification=provider.needs_clarification,
                clarification_question=provider.clarification_question,
                high_calorie_reason=provider.high_calorie_reason,
            )
        except ValidationError as exc:
            raise AnalysisError("OpenAI returned data outside the meal contract") from exc

    async def analyze_correction(self, text: str, meal: MealRecord) -> CorrectionAnalysis:
        items = [
            {"item_id": item.item_id, "name": item.name, "amount_g": item.amount_g}
            for item in meal.items
        ]
        instructions = (
            "Определи, какую позицию пользователь исправляет, и её новую массу. "
            "Выбери item_id только из переданного списка. Если неоднозначно, запроси уточнение. "
            "Верни все поля JSON Schema. Список позиций: "
            + json.dumps(items, ensure_ascii=False)
        )
        payload = await self._request(
            name="meal_correction",
            schema=ProviderCorrection.model_json_schema(),
            instructions=instructions,
            user_text=text,
        )
        try:
            return CorrectionAnalysis.model_validate_json(payload)
        except ValidationError as exc:
            raise AnalysisError("OpenAI returned data outside the correction contract") from exc

    async def aclose(self) -> None:
        if self._owns_client:
            await self._client.aclose()

    async def _request(
        self, *, name: str, schema: dict[str, Any], instructions: str, user_text: str
    ) -> str:
        responses_body = {
            "model": self._model,
            "store": False,
            "instructions": instructions,
            "input": [{"role": "user", "content": user_text}],
            "text": {
                "format": {
                    "type": "json_schema",
                    "name": name,
                    "strict": True,
                    "schema": schema,
                }
            },
            "max_output_tokens": 1800,
        }
        chat_body = {
            "model": self._model,
            "messages": [
                {"role": "system", "content": instructions},
                {"role": "user", "content": user_text},
            ],
            "response_format": {
                "type": "json_schema",
                "json_schema": {
                    "name": name,
                    "strict": True,
                    "schema": schema,
                },
            },
            "max_tokens": 1800,
        }
        for attempt in range(2):
            try:
                if not self._use_chat_completions:
                    response = await self._client.post("/responses", json=responses_body)
                    if response.status_code == 404:
                        self._use_chat_completions = True
                        response = await self._client.post("/chat/completions", json=chat_body)
                else:
                    response = await self._client.post("/chat/completions", json=chat_body)

                if response.status_code in {408, 429} or response.status_code >= 500:
                    if attempt == 0:
                        await asyncio.sleep(0.25)
                        continue
                if response.is_error:
                    raise AnalysisError(f"OpenAI request failed with HTTP {response.status_code}")
                data = response.json()
                if not isinstance(data, dict):
                    raise AnalysisError("OpenAI returned an unreadable response")
                return self._extract_output_text(data)
            except (httpx.TimeoutException, httpx.TransportError) as exc:
                if attempt == 0:
                    await asyncio.sleep(0.25)
                    continue
                raise AnalysisError("OpenAI request failed after one retry") from exc
            except (ValueError, KeyError, TypeError) as exc:
                raise AnalysisError("OpenAI returned an unreadable response") from exc
        raise AnalysisError("OpenAI request failed after one retry")

    @staticmethod
    def _extract_output_text(data: dict[str, Any]) -> str:
        direct = data.get("output_text")
        if isinstance(direct, str) and direct:
            return direct
        choices = data.get("choices")
        if isinstance(choices, list) and choices:
            message = choices[0].get("message")
            if isinstance(message, dict):
                content = message.get("content")
                if isinstance(content, str) and content:
                    return content
        output = data.get("output")
        if isinstance(output, list):
            for message in output:
                if not isinstance(message, dict) or message.get("type") != "message":
                    continue
                content = message.get("content")
                if not isinstance(content, list):
                    continue
                for part in content:
                    if isinstance(part, dict) and part.get("type") == "output_text":
                        text = part.get("text")
                        if isinstance(text, str) and text:
                            return text
        raise AnalysisError("OpenAI response has no JSON text")
