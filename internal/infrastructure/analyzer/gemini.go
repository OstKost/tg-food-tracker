package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/OstKost/tg-food-tracker/internal/domain"
)

type GeminiFoodAnalyzer struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

func NewGeminiFoodAnalyzer(
	apiKey, model, baseURL string,
	timeout time.Duration,
	client *http.Client,
) *GeminiFoodAnalyzer {
	if client == nil {
		client = &http.Client{
			Timeout: timeout,
		}
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &GeminiFoodAnalyzer{
		apiKey:     apiKey,
		model:      model,
		baseURL:    baseURL,
		httpClient: client,
	}
}

func (a *GeminiFoodAnalyzer) Analyze(ctx context.Context, text string) (*domain.MealAnalysis, error) {
	prompt := fmt.Sprintf("%s\nСообщение пользователя: %s", SystemPrompt, text)
	rawJSON, err := a.request(ctx, MealAnalysisSchema(), prompt)
	if err != nil {
		return nil, err
	}

	var res domain.MealAnalysis
	if err := json.Unmarshal([]byte(rawJSON), &res); err != nil {
		return nil, fmt.Errorf("%w: Gemini returned invalid JSON (%w)", domain.ErrAnalysis, err)
	}

	if err := res.Validate(); err != nil {
		return nil, fmt.Errorf("%w: Gemini returned data outside meal contract (%w)", domain.ErrAnalysis, err)
	}

	return &res, nil
}

func (a *GeminiFoodAnalyzer) AnalyzeCorrection(ctx context.Context, text string, meal *domain.MealRecord) (*domain.CorrectionAnalysis, error) {
	type itemInfo struct {
		ItemID  string  `json:"item_id"`
		Name    string  `json:"name"`
		AmountG float64 `json:"amount_g"`
	}
	items := make([]itemInfo, 0, len(meal.Items))
	for _, it := range meal.Items {
		items = append(items, itemInfo{ItemID: it.ItemID, Name: it.Name, AmountG: it.AmountG})
	}
	itemsJSON, _ := json.Marshal(items)

	prompt := fmt.Sprintf(
		"Определи, какую позицию пользователь исправляет, и её новую массу. "+
			"Выбери item_id только из переданного списка. Если неоднозначно, запроси уточнение. "+
			"Верни все поля JSON Schema. Список позиций: %s\nИсправление пользователя: %s",
		string(itemsJSON),
		text,
	)

	rawJSON, err := a.request(ctx, CorrectionSchema(), prompt)
	if err != nil {
		return nil, err
	}

	var res domain.CorrectionAnalysis
	if err := json.Unmarshal([]byte(rawJSON), &res); err != nil {
		return nil, fmt.Errorf("%w: Gemini returned invalid correction JSON (%w)", domain.ErrAnalysis, err)
	}

	if err := res.Validate(); err != nil {
		return nil, fmt.Errorf("%w: Gemini returned data outside correction contract (%w)", domain.ErrAnalysis, err)
	}

	return &res, nil
}

func (a *GeminiFoodAnalyzer) Close() error {
	return nil
}

func (a *GeminiFoodAnalyzer) request(ctx context.Context, schema map[string]any, prompt string) (string, error) {
	adaptedSchema := adaptGeminiSchema(schema)
	body := map[string]any{
		"model": a.model,
		"input": prompt,
		"response_format": map[string]any{
			"type":      "text",
			"mime_type": "application/json",
			"schema":    adaptedSchema,
		},
	}

	bodyBytes, _ := json.Marshal(body)
	endpoint := a.baseURL + "/interactions"

	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return "", err
		}
		req.Header.Set("x-goog-api-key", a.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := a.httpClient.Do(req)
		if err != nil {
			if attempt == 0 {
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(250 * time.Millisecond):
					continue
				}
			}
			return "", fmt.Errorf("%w: Gemini request failed after one retry (%w)", domain.ErrAnalysis, err)
		}

		respBytes, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode == 408 || resp.StatusCode == 429 || resp.StatusCode >= 500 {
			if attempt == 0 {
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(250 * time.Millisecond):
					continue
				}
			}
			return "", fmt.Errorf("%w: Gemini request failed with HTTP %d", domain.ErrAnalysis, resp.StatusCode)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("%w: Gemini request failed with HTTP %d", domain.ErrAnalysis, resp.StatusCode)
		}

		if readErr != nil {
			return "", fmt.Errorf("%w: failed to read Gemini response (%w)", domain.ErrAnalysis, readErr)
		}

		var parsed map[string]any
		if err := json.Unmarshal(respBytes, &parsed); err != nil {
			return "", fmt.Errorf("%w: Gemini returned unreadable response (%w)", domain.ErrAnalysis, err)
		}

		extracted, err := extractGeminiOutputText(parsed)
		if err != nil {
			return "", err
		}
		return extracted, nil
	}

	return "", fmt.Errorf("%w: Gemini request failed after one retry", domain.ErrAnalysis)
}

func adaptGeminiSchema(node any) any {
	switch v := node.(type) {
	case map[string]any:
		supported := map[string]bool{
			"type": true, "enum": true, "items": true, "properties": true,
			"required": true, "anyOf": true, "const": true,
		}
		converted := make(map[string]any)
		for key, val := range v {
			if !supported[key] {
				continue
			}
			if key == "properties" {
				if props, ok := val.(map[string]any); ok {
					subProps := make(map[string]any)
					for pk, pv := range props {
						subProps[pk] = adaptGeminiSchema(pv)
					}
					converted[key] = subProps
				}
			} else {
				converted[key] = adaptGeminiSchema(val)
			}
		}

		if c, ok := converted["const"]; ok {
			delete(converted, "const")
			converted["enum"] = []any{c}
		}

		// Check anyOf
		if anyOf, ok := converted["anyOf"].([]any); ok && len(anyOf) == 2 {
			var types []string
			for _, variant := range anyOf {
				if vm, ok := variant.(map[string]any); ok {
					if ts, ok := vm["type"].(string); ok {
						types = append(types, ts)
					}
				}
			}
			if len(types) == 2 {
				delete(converted, "anyOf")
				if (types[0] == "string" && types[1] == "null") || (types[0] == "null" && types[1] == "string") {
					converted["type"] = "string"
				} else {
					converted["type"] = types
				}
			}
		}

		return converted
	case []any:
		res := make([]any, len(v))
		for i, item := range v {
			res[i] = adaptGeminiSchema(item)
		}
		return res
	default:
		return node
	}
}

func extractGeminiOutputText(data map[string]any) (string, error) {
	if direct, ok := data["output_text"].(string); ok && direct != "" {
		return direct, nil
	}
	if steps, ok := data["steps"].([]any); ok {
		for i := len(steps) - 1; i >= 0; i-- {
			step, ok := steps[i].(map[string]any)
			if !ok || step["type"] != "model_output" {
				continue
			}
			content, ok := step["content"].([]any)
			if !ok {
				continue
			}
			for _, part := range content {
				if pm, ok := part.(map[string]any); ok && pm["type"] == "text" {
					if text, ok := pm["text"].(string); ok && text != "" {
						return text, nil
					}
				}
			}
		}
	}
	return "", fmt.Errorf("%w: Gemini response has no JSON text", domain.ErrAnalysis)
}
