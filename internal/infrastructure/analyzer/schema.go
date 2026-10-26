package analyzer

const SystemPrompt = `Ты извлекаешь еду из короткого сообщения на русском языке.
Верни только данные по JSON Schema. Оцени массу и КБЖУ приблизительно и консервативно.
Каждой позиции дай стабильный внутри ответа item_id: item-1, item-2 и т.д.
Если порция или продукт настолько неоднозначны, что оценка ненадёжна, установи
needs_clarification=true и задай один короткий вопрос. Не давай медицинских советов.
Если сумма превышает 5000 ккал, объясни правдоподобную причину в high_calorie_reason;
иначе верни null. Все списки должны присутствовать, даже если они пустые.`

func MealAnalysisSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"schema_version": map[string]any{"type": "string", "enum": []string{"1.0"}},
			"items": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"item_id":   map[string]any{"type": "string"},
						"name":      map[string]any{"type": "string"},
						"amount_g":  map[string]any{"type": "number"},
						"kcal":      map[string]any{"type": "number"},
						"protein_g": map[string]any{"type": "number"},
						"fat_g":     map[string]any{"type": "number"},
						"carbs_g":   map[string]any{"type": "number"},
						"food_groups": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "string",
								"enum": []string{
									"protein", "vegetable", "fruit", "grain",
									"dairy", "fat", "sweet", "drink", "other",
								},
							},
						},
						"confidence": map[string]any{"type": "number"},
						"assumptions": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
					},
					"required": []string{
						"item_id", "name", "amount_g", "kcal", "protein_g", "fat_g",
						"carbs_g", "food_groups", "confidence", "assumptions",
					},
					"additionalProperties": false,
				},
			},
			"assumptions": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"confidence":             map[string]any{"type": "number"},
			"needs_clarification":    map[string]any{"type": "boolean"},
			"clarification_question": map[string]any{"type": []any{"string", "null"}},
			"high_calorie_reason":     map[string]any{"type": []any{"string", "null"}},
		},
		"required": []string{
			"schema_version", "items", "assumptions", "confidence",
			"needs_clarification", "clarification_question", "high_calorie_reason",
		},
		"additionalProperties": false,
	}
}

func CorrectionSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"schema_version": map[string]any{"type": "string", "enum": []string{"1.0"}},
			"item_id":        map[string]any{"type": "string"},
			"new_amount_g":   map[string]any{"type": "number"},
			"confidence":     map[string]any{"type": "number"},
			"needs_clarification": map[string]any{
				"type": "boolean",
			},
			"clarification_question": map[string]any{
				"type": []any{"string", "null"},
			},
		},
		"required": []string{
			"schema_version", "item_id", "new_amount_g", "confidence",
			"needs_clarification", "clarification_question",
		},
		"additionalProperties": false,
	}
}
