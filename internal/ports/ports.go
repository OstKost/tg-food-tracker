package ports

import (
	"context"
	"time"

	"github.com/OstKost/tg-food-tracker/internal/domain"
)

type FoodAnalyzer interface {
	Analyze(ctx context.Context, text string) (*domain.MealAnalysis, error)
	AnalyzeCorrection(ctx context.Context, text string, meal *domain.MealRecord) (*domain.CorrectionAnalysis, error)
	Close() error
}

type SheetsRepository interface {
	Bootstrap(ctx context.Context) error
	ValidateSchema(ctx context.Context) error
	FindByEventKey(ctx context.Context, eventKey string) (*domain.MealRecord, error)
	AppendMeal(ctx context.Context, meal *domain.MealRecord) (*domain.MealRecord, error)
	GetTodaySummary(ctx context.Context, localDate string) (*domain.DailySummary, error)
	GetRecentMeals(ctx context.Context, limit int) ([]*domain.MealRecord, error)
	GetLastMeal(ctx context.Context) (*domain.MealRecord, error)
	UpdateMeal(ctx context.Context, meal *domain.MealRecord) (*domain.MealRecord, error)
	IsReportSent(ctx context.Context, reportKey string) (bool, error)
	ClaimReport(ctx context.Context, reportKey string, localDate string) (bool, error)
	MarkReportSent(ctx context.Context, reportKey string, sentAt time.Time, telegramMessageID int) error
	ClearPersonalData(ctx context.Context) (*domain.DeleteResult, error)
}

type TelegramTransport interface {
	SendMessage(ctx context.Context, chatID int64, text string) (int, error)
}
