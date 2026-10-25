import json

import httpx
import pytest

from food_tracker.infrastructure.openai_analyzer import OpenAIFoodAnalyzer


def response_body() -> dict[str, object]:
    payload = {
        "schema_version": "1.0",
        "items": [
            {
                "item_id": "item-1",
                "name": "Яблоко",
                "amount_g": 150,
                "kcal": 78,
                "protein_g": 0.4,
                "fat_g": 0.3,
                "carbs_g": 21,
                "food_groups": ["fruit"],
                "confidence": 0.9,
                "assumptions": [],
            }
        ],
        "assumptions": [],
        "confidence": 0.9,
        "needs_clarification": False,
        "clarification_question": None,
        "high_calorie_reason": None,
    }
    return {
        "output": [
            {
                "type": "message",
                "content": [{"type": "output_text", "text": json.dumps(payload)}],
            }
        ]
    }


@pytest.mark.asyncio
async def test_openai_request_uses_schema_and_store_false() -> None:
    captured: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured.update(json.loads(request.content))
        return httpx.Response(200, json=response_body())

    client = httpx.AsyncClient(
        transport=httpx.MockTransport(handler), base_url="https://api.openai.test/v1"
    )
    analyzer = OpenAIFoodAnalyzer("secret", "test-model", client=client)

    result = await analyzer.analyze("яблоко")

    assert result.kcal == 78
    assert captured["store"] is False
    assert captured["model"] == "test-model"
    text_config = captured["text"]
    assert isinstance(text_config, dict)
    format_config = text_config["format"]
    assert isinstance(format_config, dict)
    assert format_config["strict"] is True
    await client.aclose()


@pytest.mark.asyncio
async def test_openai_retries_one_transient_failure() -> None:
    calls = 0

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal calls
        del request
        calls += 1
        if calls == 1:
            return httpx.Response(500)
        return httpx.Response(200, json=response_body())

    client = httpx.AsyncClient(
        transport=httpx.MockTransport(handler), base_url="https://api.openai.test/v1"
    )
    analyzer = OpenAIFoodAnalyzer("secret", "test-model", client=client)

    await analyzer.analyze("яблоко")

    assert calls == 2
    await client.aclose()


@pytest.mark.asyncio
async def test_openai_retries_one_timeout() -> None:
    calls = 0

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal calls
        calls += 1
        if calls == 1:
            raise httpx.ReadTimeout("timed out", request=request)
        return httpx.Response(200, json=response_body())

    client = httpx.AsyncClient(
        transport=httpx.MockTransport(handler), base_url="https://api.openai.test/v1"
    )
    analyzer = OpenAIFoodAnalyzer("secret", "test-model", client=client)

    await analyzer.analyze("яблоко")

    assert calls == 2
    await client.aclose()
