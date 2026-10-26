package testutil

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/OstKost/tg-food-tracker/internal/domain"
)

type FakeAnalyzer struct {
	Mu                sync.Mutex
	AnalyzeCount      int
	AnalyzeFunc       func(ctx context.Context, text string) (*domain.MealAnalysis, error)
	AnalyzeCorrectionFunc func(ctx context.Context, text string, meal *domain.MealRecord) (*domain.CorrectionAnalysis, error)
}

func (f *FakeAnalyzer) Analyze(ctx context.Context, text string) (*domain.MealAnalysis, error) {
	f.Mu.Lock()
	f.AnalyzeCount++
	f.Mu.Unlock()
	if f.AnalyzeFunc != nil {
		return f.AnalyzeFunc(ctx, text)
	}
	return &domain.MealAnalysis{
		SchemaVersion: "1.0",
		Items: []domain.MealItem{
			{
				ItemID:     "item-1",
				Name:       text,
				AmountG:    100,
				Kcal:       100,
				ProteinG:   10,
				FatG:       2,
				CarbsG:     20,
				FoodGroups: []domain.FoodGroup{domain.FoodGroupProtein},
				Confidence: 0.9,
			},
		},
		Confidence: 0.9,
	}, nil
}

func (f *FakeAnalyzer) AnalyzeCorrection(ctx context.Context, text string, meal *domain.MealRecord) (*domain.CorrectionAnalysis, error) {
	if f.AnalyzeCorrectionFunc != nil {
		return f.AnalyzeCorrectionFunc(ctx, text, meal)
	}
	return &domain.CorrectionAnalysis{
		SchemaVersion: "1.0",
		ItemID:        meal.Items[0].ItemID,
		NewAmountG:   200,
		Confidence:    0.9,
	}, nil
}

func (f *FakeAnalyzer) Close() error {
	return nil
}

type FakeSheetsRepository struct {
	Mu          sync.Mutex
	Meals       []*domain.MealRecord
	Reports     []*domain.ReportRecord
	AppendError error
}

func NewFakeSheetsRepository() *FakeSheetsRepository {
	return &FakeSheetsRepository{
		Meals:   make([]*domain.MealRecord, 0),
		Reports: make([]*domain.ReportRecord, 0),
	}
}

func (f *FakeSheetsRepository) Bootstrap(ctx context.Context) error {
	return nil
}

func (f *FakeSheetsRepository) ValidateSchema(ctx context.Context) error {
	return nil
}

func (f *FakeSheetsRepository) FindByEventKey(ctx context.Context, eventKey string) (*domain.MealRecord, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	for _, m := range f.Meals {
		if m.EventKey == eventKey || (m.LastEditEventKey != nil && *m.LastEditEventKey == eventKey) {
			copy := *m
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *FakeSheetsRepository) AppendMeal(ctx context.Context, meal *domain.MealRecord) (*domain.MealRecord, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if f.AppendError != nil {
		return nil, f.AppendError
	}
	for _, m := range f.Meals {
		if m.EventKey == meal.EventKey {
			copy := *m
			return &copy, nil
		}
	}
	copy := *meal
	f.Meals = append(f.Meals, &copy)
	return &copy, nil
}

func (f *FakeSheetsRepository) GetTodaySummary(ctx context.Context, localDate string) (*domain.DailySummary, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	var kcal, protein, fat, carbs float64
	for _, m := range f.Meals {
		if m.LocalDate == localDate {
			kcal += m.Kcal
			protein += m.ProteinG
			fat += m.FatG
			carbs += m.CarbsG
		}
	}
	return &domain.DailySummary{
		LocalDate: localDate,
		Kcal:      kcal,
		ProteinG:  protein,
		FatG:      fat,
		CarbsG:    carbs,
	}, nil
}

func (f *FakeSheetsRepository) GetRecentMeals(ctx context.Context, limit int) ([]*domain.MealRecord, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	sorted := make([]*domain.MealRecord, len(f.Meals))
	copy(sorted, f.Meals)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].LoggedAt.After(sorted[j].LoggedAt)
	})
	if limit > len(sorted) {
		limit = len(sorted)
	}
	res := make([]*domain.MealRecord, limit)
	for i := 0; i < limit; i++ {
		c := *sorted[i]
		res[i] = &c
	}
	return res, nil
}

func (f *FakeSheetsRepository) GetLastMeal(ctx context.Context) (*domain.MealRecord, error) {
	meals, err := f.GetRecentMeals(ctx, 1)
	if err != nil || len(meals) == 0 {
		return nil, err
	}
	return meals[0], nil
}

func (f *FakeSheetsRepository) UpdateMeal(ctx context.Context, meal *domain.MealRecord) (*domain.MealRecord, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	for i, m := range f.Meals {
		if m.MealID == meal.MealID {
			copy := *meal
			f.Meals[i] = &copy
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("%w: meal not found %s", domain.ErrRepository, meal.MealID)
}

func (f *FakeSheetsRepository) IsReportSent(ctx context.Context, reportKey string) (bool, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	for _, r := range f.Reports {
		if r.ReportKey == reportKey && r.Status == "sent" {
			return true, nil
		}
	}
	return false, nil
}

func (f *FakeSheetsRepository) ClaimReport(ctx context.Context, reportKey string, localDate string) (bool, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	for _, r := range f.Reports {
		if r.ReportKey == reportKey {
			return false, nil
		}
	}
	f.Reports = append(f.Reports, &domain.ReportRecord{
		ReportKey: reportKey,
		LocalDate: localDate,
		Status:    "sending",
	})
	return true, nil
}

func (f *FakeSheetsRepository) MarkReportSent(ctx context.Context, reportKey string, sentAt time.Time, telegramMessageID int) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	for _, r := range f.Reports {
		if r.ReportKey == reportKey {
			r.Status = "sent"
			r.SentAt = &sentAt
			r.TelegramMessageID = &telegramMessageID
			return nil
		}
	}
	return fmt.Errorf("%w: report claim not found %s", domain.ErrRepository, reportKey)
}

func (f *FakeSheetsRepository) ClearPersonalData(ctx context.Context) (*domain.DeleteResult, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	mealsCount := len(f.Meals)
	reportsCount := len(f.Reports)
	f.Meals = make([]*domain.MealRecord, 0)
	f.Reports = make([]*domain.ReportRecord, 0)
	return &domain.DeleteResult{
		DeletedMeals:   mealsCount,
		DeletedReports: reportsCount,
	}, nil
}

type FakeTelegramTransport struct {
	Mu       sync.Mutex
	Messages []struct {
		ChatID int64
		Text   string
	}
	NextMessageID int
}

func (t *FakeTelegramTransport) SendMessage(ctx context.Context, chatID int64, text string) (int, error) {
	t.Mu.Lock()
	defer t.Mu.Unlock()
	t.Messages = append(t.Messages, struct {
		ChatID int64
		Text   string
	}{ChatID: chatID, Text: text})
	t.NextMessageID++
	return t.NextMessageID, nil
}
