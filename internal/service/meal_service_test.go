package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/OstKost/tg-food-tracker/internal/domain"
	"github.com/OstKost/tg-food-tracker/internal/testutil"
)

func TestRecordsAndCalculatesDailySummary(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	fixedNow := time.Date(2026, 10, 26, 12, 0, 0, 0, loc)
	clock := func() time.Time { return fixedNow }

	repo := testutil.NewFakeSheetsRepository()
	analyzer := &testutil.FakeAnalyzer{}
	svc := NewMealService(repo, analyzer, loc, clock)

	ctx := context.Background()
	res, err := svc.RecordMeal(ctx, 12345, "chat:1", "гречка 100г")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != domain.StatusStored {
		t.Fatalf("expected stored status, got %s", res.Status)
	}
	if res.Meal == nil || res.Daily == nil {
		t.Fatalf("expected meal and daily in result")
	}
	if res.Meal.LocalDate != "2026-10-26" {
		t.Fatalf("expected local_date 2026-10-26, got %s", res.Meal.LocalDate)
	}
	if res.Daily.Kcal != 100 {
		t.Fatalf("expected 100 kcal, got %f", res.Daily.Kcal)
	}
}

func TestDuplicateUpdateDoesNotCallAnalyzerTwice(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	repo := testutil.NewFakeSheetsRepository()
	analyzer := &testutil.FakeAnalyzer{}
	svc := NewMealService(repo, analyzer, loc, nil)

	ctx := context.Background()
	res1, err := svc.RecordMeal(ctx, 12345, "chat:1", "гречка 100г")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res1.Status != domain.StatusStored {
		t.Fatalf("expected stored, got %s", res1.Status)
	}

	res2, err := svc.RecordMeal(ctx, 12345, "chat:1", "гречка 100г")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res2.Status != domain.StatusDuplicate {
		t.Fatalf("expected duplicate status, got %s", res2.Status)
	}
	if analyzer.AnalyzeCount != 1 {
		t.Fatalf("expected 1 analyzer call, got %d", analyzer.AnalyzeCount)
	}
}

func TestParallelDuplicateJoinsInflightAnalysis(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	repo := testutil.NewFakeSheetsRepository()
	started := make(chan struct{})
	continueAnalysis := make(chan struct{})

	analyzer := &testutil.FakeAnalyzer{
		AnalyzeFunc: func(ctx context.Context, text string) (*domain.MealAnalysis, error) {
			close(started)
			<-continueAnalysis
			return &domain.MealAnalysis{
				SchemaVersion: "1.0",
				Items: []domain.MealItem{
					{
						ItemID:     "item-1",
						Name:       text,
						AmountG:    100,
						Kcal:       150,
						ProteinG:   5,
						FatG:       1,
						CarbsG:     30,
						Confidence: 0.9,
					},
				},
				Confidence: 0.9,
			}, nil
		},
	}
	svc := NewMealService(repo, analyzer, loc, nil)

	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Add(2)

	var res1, res2 *domain.RecordMealResult
	var err1, err2 error

	go func() {
		defer wg.Done()
		res1, err1 = svc.RecordMeal(ctx, 12345, "chat:1", "рис 100г")
	}()

	<-started

	go func() {
		defer wg.Done()
		res2, err2 = svc.RecordMeal(ctx, 12345, "chat:1", "рис 100г")
	}()

	time.Sleep(50 * time.Millisecond)
	close(continueAnalysis)
	wg.Wait()

	if err1 != nil || err2 != nil {
		t.Fatalf("errors: err1=%v err2=%v", err1, err2)
	}
	if res1.Status != domain.StatusStored || res2.Status != domain.StatusStored {
		t.Fatalf("expected stored status for both: %s, %s", res1.Status, res2.Status)
	}
	if analyzer.AnalyzeCount != 1 {
		t.Fatalf("expected 1 analyze call, got %d", analyzer.AnalyzeCount)
	}
}

func TestRequiresConfirmationBetween5000And10000(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	repo := testutil.NewFakeSheetsRepository()
	reason := "огромный торт на праздник"
	analyzer := &testutil.FakeAnalyzer{
		AnalyzeFunc: func(ctx context.Context, text string) (*domain.MealAnalysis, error) {
			return &domain.MealAnalysis{
				SchemaVersion: "1.0",
				Items: []domain.MealItem{
					{
						ItemID:     "item-1",
						Name:       "Торт",
						AmountG:    2000,
						Kcal:       6000,
						ProteinG:   50,
						FatG:       300,
						CarbsG:     800,
						Confidence: 0.9,
					},
				},
				Confidence:        0.9,
				HighCalorieReason: &reason,
			}, nil
		},
	}
	svc := NewMealService(repo, analyzer, loc, nil)

	ctx := context.Background()
	res, err := svc.RecordMeal(ctx, 12345, "chat:1", "целый торт")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != domain.StatusNeedsConfirmation {
		t.Fatalf("expected needs_confirmation, got %s", res.Status)
	}
	if res.ConfirmationToken == "" {
		t.Fatalf("expected confirmation token")
	}

	// Confirm
	resConf, err := svc.ConfirmMeal(ctx, 12345, res.ConfirmationToken)
	if err != nil {
		t.Fatalf("unexpected confirm error: %v", err)
	}
	if resConf.Status != domain.StatusStored {
		t.Fatalf("expected stored, got %s", resConf.Status)
	}
	if resConf.Meal.Kcal != 6000 {
		t.Fatalf("expected 6000 kcal, got %f", resConf.Meal.Kcal)
	}
}

func TestRejectsTotalOver10000(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	repo := testutil.NewFakeSheetsRepository()
	analyzer := &testutil.FakeAnalyzer{
		AnalyzeFunc: func(ctx context.Context, text string) (*domain.MealAnalysis, error) {
			return &domain.MealAnalysis{
				SchemaVersion: "1.0",
				Items: []domain.MealItem{
					{
						ItemID:     "item-1",
						Name:       "Еда",
						AmountG:    5000,
						Kcal:       12000,
						ProteinG:   100,
						FatG:       500,
						CarbsG:     1500,
						Confidence: 0.9,
					},
				},
				Confidence: 0.9,
			}, nil
		},
	}
	svc := NewMealService(repo, analyzer, loc, nil)

	ctx := context.Background()
	_, err := svc.RecordMeal(ctx, 12345, "chat:1", "10 пицц")
	if err == nil || !errors.Is(err, domain.ErrMealValidation) {
		t.Fatalf("expected ErrMealValidation for >10000 kcal, got %v", err)
	}
}

func TestEditLastScalesSelectedItemOnly(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	repo := testutil.NewFakeSheetsRepository()
	analyzer := &testutil.FakeAnalyzer{
		AnalyzeFunc: func(ctx context.Context, text string) (*domain.MealAnalysis, error) {
			return &domain.MealAnalysis{
				SchemaVersion: "1.0",
				Items: []domain.MealItem{
					{
						ItemID:     "item-1",
						Name:       "Гречка",
						AmountG:    200,
						Kcal:       200,
						ProteinG:   6,
						FatG:       2,
						CarbsG:     40,
						Confidence: 0.9,
					},
					{
						ItemID:     "item-2",
						Name:       "Курица",
						AmountG:    100,
						Kcal:       150,
						ProteinG:   30,
						FatG:       3,
						CarbsG:     0,
						Confidence: 0.9,
					},
				},
				Confidence: 0.9,
			}, nil
		},
		AnalyzeCorrectionFunc: func(ctx context.Context, text string, meal *domain.MealRecord) (*domain.CorrectionAnalysis, error) {
			return &domain.CorrectionAnalysis{
				SchemaVersion: "1.0",
				ItemID:        "item-1",
				NewAmountG:   100, // half the buckwheat
				Confidence:    0.9,
			}, nil
		},
	}
	svc := NewMealService(repo, analyzer, loc, nil)

	ctx := context.Background()
	_, err := svc.RecordMeal(ctx, 12345, "chat:1", "гречка 200г и курица 100г")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	meal, err := svc.BeginEdit(ctx, 12345)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(meal.Items) != 2 {
		t.Fatalf("expected 2 items")
	}

	resEdit, err := svc.CorrectLast(ctx, 12345, "chat:2", "гречки было 100г")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resEdit.Meal.Revision != 2 {
		t.Fatalf("expected revision 2, got %d", resEdit.Meal.Revision)
	}
	// Buckwheat halved (100 kcal instead of 200), Chicken unchanged (150 kcal) => total 250 kcal
	if resEdit.Meal.Kcal != 250 {
		t.Fatalf("expected 250 kcal, got %f", resEdit.Meal.Kcal)
	}
	if resEdit.Meal.Items[0].AmountG != 100 || resEdit.Meal.Items[1].AmountG != 100 {
		t.Fatalf("unexpected amounts: item-1=%f item-2=%f", resEdit.Meal.Items[0].AmountG, resEdit.Meal.Items[1].AmountG)
	}
}

func TestEditSessionExpiresAfter15Minutes(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	currTime := time.Date(2026, 10, 26, 12, 0, 0, 0, loc)
	clock := func() time.Time { return currTime }

	repo := testutil.NewFakeSheetsRepository()
	analyzer := &testutil.FakeAnalyzer{}
	svc := NewMealService(repo, analyzer, loc, clock)

	ctx := context.Background()
	_, _ = svc.RecordMeal(ctx, 12345, "chat:1", "еда")
	_, err := svc.BeginEdit(ctx, 12345)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Advance time by 16 minutes
	currTime = currTime.Add(16 * time.Minute)

	_, err = svc.CorrectLast(ctx, 12345, "chat:2", "исправление")
	if err == nil || !errors.Is(err, domain.ErrEditSessionExpired) {
		t.Fatalf("expected ErrEditSessionExpired, got %v", err)
	}
}

func TestDuplicateCorrectionIsSafe(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	repo := testutil.NewFakeSheetsRepository()
	analyzer := &testutil.FakeAnalyzer{}
	svc := NewMealService(repo, analyzer, loc, nil)

	ctx := context.Background()
	_, _ = svc.RecordMeal(ctx, 12345, "chat:1", "еда")
	_, _ = svc.BeginEdit(ctx, 12345)

	res1, err := svc.CorrectLast(ctx, 12345, "chat:2", "исправление")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res1.Status != domain.StatusStored {
		t.Fatalf("expected stored, got %s", res1.Status)
	}

	// Re-submitting the same edit event key
	res2, err := svc.CorrectLast(ctx, 12345, "chat:2", "исправление")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res2.Status != domain.StatusDuplicate {
		t.Fatalf("expected duplicate, got %s", res2.Status)
	}
}

func TestClearDataIsRepeatable(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	repo := testutil.NewFakeSheetsRepository()
	analyzer := &testutil.FakeAnalyzer{}
	svc := NewMealService(repo, analyzer, loc, nil)

	ctx := context.Background()
	_, _ = svc.RecordMeal(ctx, 12345, "chat:1", "еда")
	del1, err := repo.ClearPersonalData(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if del1.DeletedMeals != 1 {
		t.Fatalf("expected 1 deleted meal, got %d", del1.DeletedMeals)
	}

	del2, err := repo.ClearPersonalData(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if del2.DeletedMeals != 0 {
		t.Fatalf("expected 0 deleted meals, got %d", del2.DeletedMeals)
	}
}
