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

func TestGeminiInteractionsRequestUsesJSONSchema(t *testing.T) {
	var requestedPath string
	var capturedHeader string
	var capturedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		capturedHeader = r.Header.Get("x-goog-api-key")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output_text": validMealAnalysisJSON(),
		})
	}))
	defer srv.Close()

	client := NewGeminiFoodAnalyzer("gemini-test-key", "gemini-model", srv.URL, 5*time.Second, srv.Client())
	res, err := client.Analyze(context.Background(), "гречка 200г")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if requestedPath != "/interactions" {
		t.Fatalf("expected /interactions, got %s", requestedPath)
	}
	if capturedHeader != "gemini-test-key" {
		t.Fatalf("expected header x-goog-api-key: gemini-test-key, got %s", capturedHeader)
	}
	if len(res.Items) != 1 || res.Items[0].Name != "Гречка" {
		t.Fatalf("unexpected items: %+v", res.Items)
	}
}

func TestGeminiRetriesOneTransientFailure(t *testing.T) {
	var attempts int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		val := atomic.AddInt32(&attempts, 1)
		if val == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"steps": []any{
				map[string]any{
					"type": "model_output",
					"content": []any{
						map[string]any{
							"type": "text",
							"text": validMealAnalysisJSON(),
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	client := NewGeminiFoodAnalyzer("gemini-test-key", "gemini-model", srv.URL, 5*time.Second, srv.Client())
	res, err := client.Analyze(context.Background(), "гречка 200г")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if res.Items[0].AmountG != 200 {
		t.Fatalf("expected 200g, got %f", res.Items[0].AmountG)
	}
}
