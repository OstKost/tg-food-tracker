package analyzer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func validMealAnalysisJSON() string {
	return `{
		"schema_version": "1.0",
		"items": [
			{
				"item_id": "item-1",
				"name": "Гречка",
				"amount_g": 200,
				"kcal": 220,
				"protein_g": 8,
				"fat_g": 2,
				"carbs_g": 40,
				"food_groups": ["grain"],
				"confidence": 0.95,
				"assumptions": ["вареная"]
			}
		],
		"assumptions": ["порция стандартная"],
		"confidence": 0.95,
		"needs_clarification": false,
		"clarification_question": null,
		"high_calorie_reason": null
	}`
}

func TestOpenAIRequestUsesSchemaAndStoreFalse(t *testing.T) {
	var requestedPath string
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output_text": validMealAnalysisJSON(),
		})
	}))
	defer srv.Close()

	client := NewOpenAIFoodAnalyzer("sk-test", "gpt-test", srv.URL, 5*time.Second, srv.Client())
	res, err := client.Analyze(context.Background(), "гречка 200г")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if requestedPath != "/responses" {
		t.Fatalf("expected /responses, got %s", requestedPath)
	}
	if capturedBody["store"] != false {
		t.Fatalf("expected store: false, got %v", capturedBody["store"])
	}
	if len(res.Items) != 1 || res.Items[0].Name != "Гречка" {
		t.Fatalf("unexpected items: %+v", res.Items)
	}
}

func TestOpenAIRetriesOneTransientFailure(t *testing.T) {
	var attempts int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		val := atomic.AddInt32(&attempts, 1)
		if val == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output_text": validMealAnalysisJSON(),
		})
	}))
	defer srv.Close()

	client := NewOpenAIFoodAnalyzer("sk-test", "gpt-test", srv.URL, 5*time.Second, srv.Client())
	res, err := client.Analyze(context.Background(), "гречка 200г")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if res.Items[0].Kcal != 220 {
		t.Fatalf("expected 220 kcal, got %f", res.Items[0].Kcal)
	}
}

func TestOpenAIFallbackToChatCompletions(t *testing.T) {
	var responsesCalled, chatCalled int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/responses" {
			atomic.AddInt32(&responsesCalled, 1)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path == "/chat/completions" {
			atomic.AddInt32(&chatCalled, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{
					map[string]any{
						"message": map[string]any{
							"content": validMealAnalysisJSON(),
						},
					},
				},
			})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewOpenAIFoodAnalyzer("sk-test", "gpt-test", srv.URL, 5*time.Second, srv.Client())
	res, err := client.Analyze(context.Background(), "гречка 200г")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if atomic.LoadInt32(&responsesCalled) != 1 {
		t.Fatalf("expected 1 /responses call, got %d", responsesCalled)
	}
	if atomic.LoadInt32(&chatCalled) != 1 {
		t.Fatalf("expected 1 /chat/completions call, got %d", chatCalled)
	}
	if res.Items[0].Name != "Гречка" {
		t.Fatalf("expected Greek, got %s", res.Items[0].Name)
	}

	// Subsequent call should go straight to chat/completions
	_, err = client.Analyze(context.Background(), "гречка 200г")
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if atomic.LoadInt32(&responsesCalled) != 1 {
		t.Fatalf("expected /responses not to be called again, count: %d", responsesCalled)
	}
	if atomic.LoadInt32(&chatCalled) != 2 {
		t.Fatalf("expected 2 /chat/completions calls, got %d", chatCalled)
	}
}
