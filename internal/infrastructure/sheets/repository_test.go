package sheets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OstKost/tg-food-tracker/internal/domain"
	"google.golang.org/api/option"
	sheetsapi "google.golang.org/api/sheets/v4"
)

func TestSheetsBootstrapIsIdempotent(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	spreadsheetID := "test-sheet-id"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// GET metadata
		if r.Method == http.MethodGet && r.URL.Path == "/v4/spreadsheets/"+spreadsheetID {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"properties": map[string]any{"timeZone": "Europe/Moscow"},
				"sheets": []any{
					map[string]any{"properties": map[string]any{"title": "Meals"}},
					map[string]any{"properties": map[string]any{"title": "Dashboard"}},
					map[string]any{"properties": map[string]any{"title": "ReportLog"}},
				},
			})
			return
		}
		// GET values
		if r.Method == http.MethodGet {
			// Range checks
			switch {
			case r.URL.Query().Get("valueRenderOption") == "FORMULA":
				// return expected headers
				if r.URL.Path == "/v4/spreadsheets/"+spreadsheetID+"/values/Meals!A1:N1" {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"values": [][]any{toStringInterfaceSlice(MealsHeaders)},
					})
					return
				}
				if r.URL.Path == "/v4/spreadsheets/"+spreadsheetID+"/values/ReportLog!A1:E1" {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"values": [][]any{toStringInterfaceSlice(ReportHeaders)},
					})
					return
				}
				if r.URL.Path == "/v4/spreadsheets/"+spreadsheetID+"/values/Dashboard!A1:E1" {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"values": [][]any{toStringInterfaceSlice(DashboardHeaders)},
					})
					return
				}
				if r.URL.Path == "/v4/spreadsheets/"+spreadsheetID+"/values/Dashboard!A2:E2" {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"values": [][]any{toStringInterfaceSlice(DashboardFormulas)},
					})
					return
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"values": [][]any{}})
			return
		}
		// BatchUpdate or values update
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	ctx := context.Background()
	svc, err := sheetsapi.NewService(ctx, option.WithoutAuthentication(), option.WithEndpoint(srv.URL), option.WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("failed to create sheets service: %v", err)
	}

	repo, err := NewGoogleSheetsRepository(ctx, spreadsheetID, "", 12345, loc, svc)
	if err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}

	if err := repo.Bootstrap(ctx); err != nil {
		t.Fatalf("unexpected error on bootstrap 1: %v", err)
	}
	if err := repo.Bootstrap(ctx); err != nil {
		t.Fatalf("unexpected error on bootstrap 2 (idempotent): %v", err)
	}
}

func TestAppendTimeoutIsReconciledByEventKey(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	spreadsheetID := "test-sheet-id"
	var appendCalls int32

	now := time.Now().In(loc)
	meal := &domain.MealRecord{
		EventKey:       "c:123",
		MealID:         "m:1",
		TelegramUserID: 12345,
		LoggedAt:       now,
		LocalDate:      now.Format("2006-01-02"),
		RawText:        "еда",
		Summary:        "еда",
		Kcal:           100,
		ProteinG:       10,
		FatG:           2,
		CarbsG:         10,
		Items: []domain.MealItem{
			{
				ItemID:     "i-1",
				Name:       "еда",
				AmountG:    100,
				Kcal:       100,
				ProteinG:   10,
				FatG:       2,
				CarbsG:     10,
				FoodGroups: []domain.FoodGroup{domain.FoodGroupProtein},
				Confidence: 0.9,
			},
		},
		Revision: 1,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// GET meals
		if r.Method == http.MethodGet {
			count := atomic.LoadInt32(&appendCalls)
			if count == 0 {
				// empty before append
				_ = json.NewEncoder(w).Encode(map[string]any{"values": [][]any{}})
			} else {
				// row was actually written to sheet before error
				itemsJSON, _ := json.Marshal(meal.Items)
				row := []any{
					meal.EventKey, meal.MealID, meal.LoggedAt.Format(time.RFC3339),
					meal.LocalDate, meal.RawText, meal.Summary, meal.Kcal, meal.ProteinG,
					meal.FatG, meal.CarbsG, string(itemsJSON), "[]", 1, "",
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"values": [][]any{row},
				})
			}
			return
		}
		// POST append
		if r.Method == http.MethodPost {
			atomic.AddInt32(&appendCalls, 1)
			// simulate network error / 504 Gateway Timeout
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
	}))
	defer srv.Close()

	ctx := context.Background()
	svc, err := sheetsapi.NewService(ctx, option.WithoutAuthentication(), option.WithEndpoint(srv.URL), option.WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("failed to create sheets service: %v", err)
	}

	repo, err := NewGoogleSheetsRepository(ctx, spreadsheetID, "", 12345, loc, svc)
	if err != nil {
		t.Fatalf("failed to create repo: %v", err)
	}

	stored, err := repo.AppendMeal(ctx, meal)
	if err != nil {
		t.Fatalf("expected reconciliation without error, got %v", err)
	}
	if stored.EventKey != "c:123" {
		t.Fatalf("expected event_key c:123, got %s", stored.EventKey)
	}
}
