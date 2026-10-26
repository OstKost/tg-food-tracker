package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/OstKost/tg-food-tracker/internal/domain"
)

type OpenAIFoodAnalyzer struct {
	apiKey             string
	model              string
	baseURL            string
	httpClient         *http.Client
	mu                 sync.Mutex
	useChatCompletions bool
}

func NewOpenAIFoodAnalyzer(
	apiKey, model, baseURL string,
	timeout time.Duration,
	client *http.Client,
) *OpenAIFoodAnalyzer {
	if client == nil {
		client = &http.Client{
			Timeout: timeout,
		}
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &OpenAIFoodAnalyzer{
		apiKey:     apiKey,
		model:      model,
		baseURL:    baseURL,
		httpClient: client,
	}
}

func (a *OpenAIFoodAnalyzer) Analyze(ctx context.Context, text string) (*domain.MealAnalysis, error) {
	rawJSON, err := a.request(ctx, "meal_analysis", MealAnalysisSchema(), SystemPrompt, text)
	if err != nil {
		return nil, err
	}

	var res domain.MealAnalysis
	if err := json.Unmarshal([]byte(rawJSON), &res); err != nil {
		return nil, fmt.Errorf("%w: OpenAI returned invalid JSON (%w)", domain.ErrAnalysis, err)
	}

	if err := res.Validate(); err != nil {
		return nil, fmt.Errorf("%w: OpenAI returned data outside meal contract (%w)", domain.ErrAnalysis, err)
	}

	return &res, nil
}

func (a *OpenAIFoodAnalyzer) AnalyzeCorrection(ctx context.Context, text string, meal *domain.MealRecord) (*domain.CorrectionAnalysis, error) {
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

	instructions := fmt.Sprintf(
		"Определи, какую позицию пользователь исправляет, и её новую массу. "+
			"Выбери item_id только из переданного списка. Если неоднозначно, запроси уточнение. "+
			"Верни все поля JSON Schema. Список позиций: %s",
		string(itemsJSON),
	)

	rawJSON, err := a.request(ctx, "meal_correction", CorrectionSchema(), instructions, text)
	if err != nil {
		return nil, err
	}

	var res domain.CorrectionAnalysis
	if err := json.Unmarshal([]byte(rawJSON), &res); err != nil {
		return nil, fmt.Errorf("%w: OpenAI returned invalid correction JSON (%w)", domain.ErrAnalysis, err)
	}

	if err := res.Validate(); err != nil {
		return nil, fmt.Errorf("%w: OpenAI returned data outside correction contract (%w)", domain.ErrAnalysis, err)
	}

	return &res, nil
}

func (a *OpenAIFoodAnalyzer) Close() error {
	return nil
}

func (a *OpenAIFoodAnalyzer) request(ctx context.Context, name string, schema map[string]any, instructions, userText string) (string, error) {
	responsesBody := map[string]any{
		"model":        a.model,
		"store":        false,
		"instructions": instructions,
		"input": []map[string]any{
			{"role": "user", "content": userText},
		},
		"text": map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"name":   name,
				"strict": true,
				"schema": schema,
			},
		},
		"max_output_tokens": 1800,
	}

	chatBody := map[string]any{
		"model": a.model,
		"messages": []map[string]any{
			{"role": "system", "content": instructions},
			{"role": "user", "content": userText},
		},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   name,
				"strict": true,
				"schema": schema,
			},
		},
		"max_tokens": 1800,
	}

	for attempt := 0; attempt < 2; attempt++ {
		a.mu.Lock()
		useChat := a.useChatCompletions
		a.mu.Unlock()

		var endpoint string
		var bodyObj any
		if !useChat {
			endpoint = a.baseURL + "/responses"
			bodyObj = responsesBody
		} else {
			endpoint = a.baseURL + "/chat/completions"
			bodyObj = chatBody
		}

		bodyBytes, _ := json.Marshal(bodyObj)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
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
			return "", fmt.Errorf("%w: OpenAI request failed after retry (%w)", domain.ErrAnalysis, err)
		}

		if resp.StatusCode == http.StatusNotFound && !useChat {
			_ = resp.Body.Close()
			a.mu.Lock()
			a.useChatCompletions = true
			a.mu.Unlock()

			// Retry immediately with chat completions
			chatBytes, _ := json.Marshal(chatBody)
			reqChat, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/chat/completions", bytes.NewReader(chatBytes))
			if err != nil {
				return "", err
			}
			reqChat.Header.Set("Authorization", "Bearer "+a.apiKey)
			reqChat.Header.Set("Content-Type", "application/json")
			resp, err = a.httpClient.Do(reqChat)
			if err != nil {
				return "", fmt.Errorf("%w: OpenAI fallback to chat completions failed (%w)", domain.ErrAnalysis, err)
			}
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
			return "", fmt.Errorf("%w: OpenAI request failed with HTTP %d", domain.ErrAnalysis, resp.StatusCode)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("%w: OpenAI request failed with HTTP %d", domain.ErrAnalysis, resp.StatusCode)
		}

		if readErr != nil {
			return "", fmt.Errorf("%w: failed to read OpenAI response (%w)", domain.ErrAnalysis, readErr)
		}

		var parsed map[string]any
		if err := json.Unmarshal(respBytes, &parsed); err != nil {
			return "", fmt.Errorf("%w: OpenAI returned unreadable JSON (%w)", domain.ErrAnalysis, err)
		}

		extracted, err := extractOpenAIOutputText(parsed)
		if err != nil {
			return "", err
		}
		return extracted, nil
	}

	return "", fmt.Errorf("%w: OpenAI request failed after one retry", domain.ErrAnalysis)
}

func extractOpenAIOutputText(data map[string]any) (string, error) {
	if direct, ok := data["output_text"].(string); ok && direct != "" {
		return direct, nil
	}
	if choices, ok := data["choices"].([]any); ok && len(choices) > 0 {
		if firstChoice, ok := choices[0].(map[string]any); ok {
			if msg, ok := firstChoice["message"].(map[string]any); ok {
				if content, ok := msg["content"].(string); ok && content != "" {
					return content, nil
				}
			}
		}
	}
	if output, ok := data["output"].([]any); ok {
		for _, item := range output {
			if m, ok := item.(map[string]any); ok && m["type"] == "message" {
				if content, ok := m["content"].([]any); ok {
					for _, part := range content {
						if p, ok := part.(map[string]any); ok && p["type"] == "output_text" {
							if text, ok := p["text"].(string); ok && text != "" {
								return text, nil
							}
						}
					}
				}
			}
		}
	}
	return "", fmt.Errorf("%w: OpenAI response has no JSON text", domain.ErrAnalysis)
}
