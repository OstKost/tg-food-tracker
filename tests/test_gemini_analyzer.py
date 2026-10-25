import json

import httpx
import pytest

from food_tracker.infrastructure.gemini_analyzer import GeminiFoodAnalyzer


def interaction_response() -> dict[str, object]:
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
        "steps": [
            {
                "type": "model_output",
                "content": [{"type": "text", "text": json.dumps(payload)}],
            }
        ]
    }


@pytest.mark.asyncio
async def test_gemini_interactions_request_uses_json_schema() -> None:
    captured: dict[str, object] = {}
    headers: dict[str, str] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured.update(json.loads(request.content))
        headers.update(request.headers)
        return httpx.Response(200, json=interaction_response())

    client = httpx.AsyncClient(
        transport=httpx.MockTransport(handler),
        base_url="https://gemini.test/v1beta",
    )
    analyzer = GeminiFoodAnalyzer("gemini-secret", "gemini-test", client=client)

    result = await analyzer.analyze("яблоко")

    assert result.kcal == 78
    assert headers["x-goog-api-key"] == "gemini-secret"
    assert captured["model"] == "gemini-test"
    response_format = captured["response_format"]
    assert isinstance(response_format, dict)
    assert response_format["mime_type"] == "application/json"
    assert response_format["type"] == "text"
    schema = response_format["schema"]
    assert isinstance(schema, dict)
    assert "$defs" not in json.dumps(schema)
    assert "$ref" not in json.dumps(schema)
    properties = schema["properties"]
    assert isinstance(properties, dict)
    assert properties["clarification_question"]["type"] == "string"
    assert set(properties["clarification_question"]) == {"type"}
    assert set(schema) == {"type", "properties", "required"}
    await client.aclose()


@pytest.mark.asyncio
async def test_gemini_retries_one_transient_failure() -> None:
    calls = 0

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal calls
        del request
        calls += 1
        if calls == 1:
            return httpx.Response(503)
        return httpx.Response(200, json=interaction_response())

    client = httpx.AsyncClient(
        transport=httpx.MockTransport(handler),
        base_url="https://gemini.test/v1beta",
    )
    analyzer = GeminiFoodAnalyzer("gemini-secret", "gemini-test", client=client)

    await analyzer.analyze("яблоко")

    assert calls == 2
    await client.aclose()
