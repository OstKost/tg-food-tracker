package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/OstKost/tg-food-tracker/internal/domain"
	"github.com/OstKost/tg-food-tracker/internal/testutil"
)

func TestReportBefore21IsSkipped(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	curr := time.Date(2026, 10, 26, 20, 59, 0, 0, loc)
	repo := testutil.NewFakeSheetsRepository()
	transport := &testutil.FakeTelegramTransport{}

	sched := NewDailyReportScheduler(repo, transport, 12345, loc, 21, 0, func() time.Time { return curr })
	ctx := context.Background()

	sent, err := sched.RunOnce(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sent {
		t.Fatalf("expected report not to be sent before 21:00")
	}
	if len(transport.Messages) != 0 {
		t.Fatalf("expected no messages sent")
	}
}

func TestEmptyDayIsSkipped(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	curr := time.Date(2026, 10, 26, 21, 5, 0, 0, loc)
	repo := testutil.NewFakeSheetsRepository()
	transport := &testutil.FakeTelegramTransport{}

	sched := NewDailyReportScheduler(repo, transport, 12345, loc, 21, 0, func() time.Time { return curr })
	ctx := context.Background()

	sent, err := sched.RunOnce(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sent {
		t.Fatalf("expected report not to be sent when day has no food")
	}
	if len(transport.Messages) != 0 {
		t.Fatalf("expected no messages sent")
	}
}

func TestReportIsSentOnce(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	curr := time.Date(2026, 10, 26, 21, 5, 0, 0, loc)
	repo := testutil.NewFakeSheetsRepository()
	transport := &testutil.FakeTelegramTransport{}

	// Add a meal
	_, _ = repo.AppendMeal(context.Background(), &domain.MealRecord{
		EventKey:       "c:1",
		MealID:         "m-1",
		TelegramUserID: 12345,
		LoggedAt:       curr.Add(-2 * time.Hour),
		LocalDate:      "2026-10-26",
		RawText:        "еда",
		Summary:        "еда",
		Kcal:           500,
		ProteinG:       25,
		FatG:           10,
		CarbsG:         75,
		Items: []domain.MealItem{
			{
				ItemID:     "i-1",
				Name:       "еда",
				AmountG:    200,
				Kcal:       500,
				ProteinG:   25,
				FatG:       10,
				CarbsG:     75,
				FoodGroups: []domain.FoodGroup{domain.FoodGroupProtein, domain.FoodGroupVegetable},
				Confidence: 0.9,
			},
		},
		Revision: 1,
	})

	sched := NewDailyReportScheduler(repo, transport, 12345, loc, 21, 0, func() time.Time { return curr })
	ctx := context.Background()

	sent, err := sched.RunOnce(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sent {
		t.Fatalf("expected report to be sent")
	}
	if len(transport.Messages) != 1 {
		t.Fatalf("expected exactly 1 message sent")
	}

	// Second run same day
	sent2, err := sched.RunOnce(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error on second run: %v", err)
	}
	if sent2 {
		t.Fatalf("expected second run not to send duplicate report")
	}
	if len(transport.Messages) != 1 {
		t.Fatalf("expected still exactly 1 message sent")
	}
}

func TestNextLocalDayGetsANewReport(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	day1 := time.Date(2026, 10, 26, 21, 5, 0, 0, loc)
	repo := testutil.NewFakeSheetsRepository()
	transport := &testutil.FakeTelegramTransport{}

	ctx := context.Background()
	_, _ = repo.AppendMeal(ctx, &domain.MealRecord{
		EventKey:       "c:1",
		MealID:         "m-1",
		TelegramUserID: 12345,
		LoggedAt:       day1,
		LocalDate:      "2026-10-26",
		RawText:        "еда",
		Summary:        "еда",
		Kcal:           300,
		ProteinG:       15,
		FatG:           5,
		CarbsG:         50,
		Items: []domain.MealItem{
			{
				ItemID:     "i-1",
				Name:       "еда",
				AmountG:    100,
				Kcal:       300,
				ProteinG:   15,
				FatG:       5,
				CarbsG:     50,
				FoodGroups: []domain.FoodGroup{domain.FoodGroupFruit},
				Confidence: 0.9,
			},
		},
		Revision: 1,
	})

	currTime := day1
	sched := NewDailyReportScheduler(repo, transport, 12345, loc, 21, 0, func() time.Time { return currTime })

	sent1, err := sched.RunOnce(ctx, nil)
	if err != nil || !sent1 {
		t.Fatalf("expected day 1 report to send: sent=%v err=%v", sent1, err)
	}

	// Move to next day
	day2 := time.Date(2026, 10, 27, 21, 10, 0, 0, loc)
	currTime = day2
	_, _ = repo.AppendMeal(ctx, &domain.MealRecord{
		EventKey:       "c:2",
		MealID:         "m-2",
		TelegramUserID: 12345,
		LoggedAt:       day2,
		LocalDate:      "2026-10-27",
		RawText:        "еда день 2",
		Summary:        "еда день 2",
		Kcal:           400,
		ProteinG:       20,
		FatG:           10,
		CarbsG:         60,
		Items: []domain.MealItem{
			{
				ItemID:     "i-2",
				Name:       "еда день 2",
				AmountG:    150,
				Kcal:       400,
				ProteinG:   20,
				FatG:       10,
				CarbsG:     60,
				FoodGroups: []domain.FoodGroup{domain.FoodGroupVegetable},
				Confidence: 0.9,
			},
		},
		Revision: 1,
	})

	sent2, err := sched.RunOnce(ctx, nil)
	if err != nil || !sent2 {
		t.Fatalf("expected day 2 report to send: sent=%v err=%v", sent2, err)
	}
	if len(transport.Messages) != 2 {
		t.Fatalf("expected 2 messages sent, got %d", len(transport.Messages))
	}
}
