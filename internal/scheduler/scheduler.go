package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/OstKost/tg-food-tracker/internal/domain"
	"github.com/OstKost/tg-food-tracker/internal/ports"
	"github.com/OstKost/tg-food-tracker/internal/recommendation"
)

type DailyReportScheduler struct {
	repo           ports.SheetsRepository
	transport      ports.TelegramTransport
	telegramUserID int64
	location       *time.Location
	reportHour     int
	reportMinute   int
	clock          func() time.Time
	lastSentDate   string
}

func NewDailyReportScheduler(
	repo ports.SheetsRepository,
	transport ports.TelegramTransport,
	telegramUserID int64,
	location *time.Location,
	reportHour, reportMinute int,
	clock func() time.Time,
) *DailyReportScheduler {
	if clock == nil {
		clock = func() time.Time {
			return time.Now().In(location)
		}
	}
	return &DailyReportScheduler{
		repo:           repo,
		transport:      transport,
		telegramUserID: telegramUserID,
		location:       location,
		reportHour:     reportHour,
		reportMinute:   reportMinute,
		clock:          clock,
	}
}

func (s *DailyReportScheduler) RunOnce(ctx context.Context, customNow *time.Time) (bool, error) {
	now := s.clock()
	if customNow != nil {
		now = *customNow
	}
	current := now.In(s.location)

	if current.Hour() < s.reportHour || (current.Hour() == s.reportHour && current.Minute() < s.reportMinute) {
		return false, nil
	}

	localDate := current.Format("2006-01-02")
	if s.lastSentDate == localDate {
		return false, nil
	}

	reportKey := fmt.Sprintf("daily:%d:%s", s.telegramUserID, localDate)
	sent, err := s.repo.IsReportSent(ctx, reportKey)
	if err != nil {
		return false, err
	}
	if sent {
		s.lastSentDate = localDate
		return false, nil
	}

	summary, err := s.repo.GetTodaySummary(ctx, localDate)
	if err != nil {
		return false, err
	}
	if summary.Kcal == 0 && summary.ProteinG == 0 && summary.FatG == 0 && summary.CarbsG == 0 {
		return false, nil
	}

	claimed, err := s.repo.ClaimReport(ctx, reportKey, localDate)
	if err != nil {
		return false, err
	}
	if !claimed {
		s.lastSentDate = localDate
		return false, nil
	}

	recent, err := s.repo.GetRecentMeals(ctx, 100)
	if err != nil {
		return false, err
	}
	todayMeals := make([]*domain.MealRecord, 0)
	for _, m := range recent {
		if m.LocalDate == localDate {
			todayMeals = append(todayMeals, m)
		}
	}

	rec := recommendation.DailyRecommendation(summary, todayMeals)
	parsedDate, _ := time.Parse("2006-01-02", localDate)
	formattedDate := parsedDate.Format("02.01.2006")

	text := fmt.Sprintf(
		"Итог за %s\nКалории: %.0f ккал\nБ/Ж/У: %.1f / %.1f / %.1f г\n\n%s",
		formattedDate,
		summary.Kcal,
		summary.ProteinG,
		summary.FatG,
		summary.CarbsG,
		rec,
	)

	msgID, err := s.transport.SendMessage(ctx, s.telegramUserID, text)
	if err != nil {
		return false, err
	}

	if err := s.repo.MarkReportSent(ctx, reportKey, current, msgID); err != nil {
		return false, err
	}

	s.lastSentDate = localDate
	return true, nil
}

func (s *DailyReportScheduler) RunForever(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if _, err := s.RunOnce(ctx, nil); err != nil {
			slog.Error("failed to run daily report scheduler iteration", "err", err)
		}

		current := s.clock().In(s.location)
		var sleepDuration time.Duration
		if s.lastSentDate == current.Format("2006-01-02") {
			sleepDuration = 5 * time.Minute
		} else {
			sleepDuration = 1 * time.Minute
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(sleepDuration):
		}
	}
}
