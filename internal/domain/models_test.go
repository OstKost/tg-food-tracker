package domain

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestRejectsNonFiniteNumber(t *testing.T) {
	item := MealItem{
		ItemID:     "item-1",
		Name:       "Гречка",
		AmountG:    math.NaN(),
		Kcal:       100,
		ProteinG:   10,
		FatG:       2,
		CarbsG:     20,
		FoodGroups: []FoodGroup{FoodGroupGrain},
		Confidence: 0.9,
	}
	if err := item.Validate(); !errors.Is(err, ErrMealValidation) {
		t.Fatalf("expected ErrMealValidation for NaN amount_g, got %v", err)
	}

	item.AmountG = 100
	item.Kcal = math.Inf(1)
	if err := item.Validate(); !errors.Is(err, ErrMealValidation) {
		t.Fatalf("expected ErrMealValidation for Inf kcal, got %v", err)
	}
}

func TestLimitsItemCount(t *testing.T) {
	items := make([]MealItem, 21)
	for i := 0; i < 21; i++ {
		items[i] = MealItem{
			ItemID:     string(rune('a' + i)),
			Name:       "Item",
			AmountG:    100,
			Kcal:       100,
			ProteinG:   10,
			FatG:       2,
			CarbsG:     20,
			Confidence: 0.9,
		}
	}
	analysis := MealAnalysis{
		SchemaVersion: "1.0",
		Items:         items,
		Confidence:    0.9,
	}
	if err := analysis.Validate(); !errors.Is(err, ErrMealValidation) {
		t.Fatalf("expected ErrMealValidation for >20 items, got %v", err)
	}
}

func TestClarificationRequiresQuestion(t *testing.T) {
	analysis := MealAnalysis{
		SchemaVersion:      "1.0",
		Items:              []MealItem{{ItemID: "1", Name: "Яблоко", AmountG: 100, Kcal: 52, ProteinG: 0.3, FatG: 0.2, CarbsG: 14, Confidence: 0.5}},
		Confidence:         0.5,
		NeedsClarification: true,
	}
	if err := analysis.Validate(); !errors.Is(err, ErrMealValidation) {
		t.Fatalf("expected ErrMealValidation when clarification question is missing, got %v", err)
	}

	q := "Какого размера яблоко?"
	analysis.ClarificationQuestion = &q
	if err := analysis.Validate(); err != nil {
		t.Fatalf("expected valid analysis, got %v", err)
	}
}

func TestMealRecordTotalsValidation(t *testing.T) {
	now := time.Now()
	items := []MealItem{
		{
			ItemID:     "1",
			Name:       "Грудка куриная",
			AmountG:    150,
			Kcal:       247.5,
			ProteinG:   46.5,
			FatG:       5.4,
			CarbsG:     0.0,
			Confidence: 0.9,
		},
	}
	record := MealRecord{
		EventKey:       "123:456",
		MealID:         "meal-uuid",
		TelegramUserID: 1234567,
		LoggedAt:       now,
		LocalDate:      "2026-10-26",
		RawText:        "куриная грудка 150г",
		Summary:        "Грудка куриная",
		Kcal:           247.5,
		ProteinG:       46.5,
		FatG:           5.4,
		CarbsG:         0.0,
		Items:          items,
		Revision:       1,
	}

	if err := record.Validate(); err != nil {
		t.Fatalf("expected valid record, got %v", err)
	}

	// Mismatched total
	record.Kcal = 500.0
	if err := record.Validate(); !errors.Is(err, ErrMealValidation) {
		t.Fatalf("expected ErrMealValidation for mismatched totals, got %v", err)
	}
}
