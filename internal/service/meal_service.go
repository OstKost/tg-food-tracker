package service

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/OstKost/tg-food-tracker/internal/domain"
	"github.com/OstKost/tg-food-tracker/internal/ports"
	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"
)

type pendingMeal struct {
	eventKey  string
	userID    int64
	rawText   string
	analysis  *domain.MealAnalysis
	expiresAt time.Time
}

type clarification struct {
	eventKey  string
	userID    int64
	rawText   string
	expiresAt time.Time
}

type editSession struct {
	mealID    string
	expiresAt time.Time
}

type MealService struct {
	repo     ports.SheetsRepository
	analyzer ports.FoodAnalyzer
	location *time.Location
	clock    func() time.Time

	sf singleflight.Group

	mu             sync.Mutex
	pending        map[string]*pendingMeal
	clarifications map[int64]*clarification
	edits          map[int64]*editSession
}

func NewMealService(
	repo ports.SheetsRepository,
	analyzer ports.FoodAnalyzer,
	location *time.Location,
	clock func() time.Time,
) *MealService {
	if clock == nil {
		clock = func() time.Time {
			return time.Now().In(location)
		}
	}
	return &MealService{
		repo:           repo,
		analyzer:       analyzer,
		location:       location,
		clock:          clock,
		pending:        make(map[string]*pendingMeal),
		clarifications: make(map[int64]*clarification),
		edits:          make(map[int64]*editSession),
	}
}

func (s *MealService) now() time.Time {
	return s.clock().In(s.location)
}

func (s *MealService) RecordMeal(ctx context.Context, userID int64, eventKey, text string) (*domain.RecordMealResult, error) {
	existing, err := s.repo.FindByEventKey(ctx, eventKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return s.duplicateResult(ctx, existing)
	}

	res, err, _ := s.sf.Do(eventKey, func() (any, error) {
		return s.recordNew(ctx, userID, eventKey, text)
	})
	if err != nil {
		return nil, err
	}
	return res.(*domain.RecordMealResult), nil
}

func (s *MealService) recordNew(ctx context.Context, userID int64, eventKey, text string) (*domain.RecordMealResult, error) {
	analysis, err := s.analyzer.Analyze(ctx, text)
	if err != nil {
		return nil, err
	}

	if analysis.NeedsClarification {
		q := ""
		if analysis.ClarificationQuestion != nil {
			q = *analysis.ClarificationQuestion
		}
		s.mu.Lock()
		s.clarifications[userID] = &clarification{
			eventKey:  eventKey,
			userID:    userID,
			rawText:   text,
			expiresAt: s.now().Add(15 * time.Minute),
		}
		s.mu.Unlock()
		return &domain.RecordMealResult{
			Status: domain.StatusNeedsClarification,
			Prompt: q,
		}, nil
	}

	kcal := analysis.Kcal()
	if kcal > 10000 {
		return nil, fmt.Errorf("%w: Получилось больше 10 000 ккал. Уточните продукты и размеры порций.", domain.ErrMealValidation)
	}

	if kcal > 5000 {
		if analysis.HighCalorieReason == nil || *analysis.HighCalorieReason == "" {
			return nil, fmt.Errorf("%w: Получилось больше 5 000 ккал, но причина неясна. Уточните порцию.", domain.ErrMealValidation)
		}
		token := uuid.NewString()
		s.mu.Lock()
		s.pending[token] = &pendingMeal{
			eventKey:  eventKey,
			userID:    userID,
			rawText:   text,
			analysis:  analysis,
			expiresAt: s.now().Add(15 * time.Minute),
		}
		s.mu.Unlock()

		prompt := fmt.Sprintf("Получилось %.0f ккал. Причина: %s. Записать?", kcal, *analysis.HighCalorieReason)
		return &domain.RecordMealResult{
			Status:            domain.StatusNeedsConfirmation,
			Prompt:            prompt,
			ConfirmationToken: token,
		}, nil
	}

	meal, err := s.buildMeal(userID, eventKey, text, analysis)
	if err != nil {
		return nil, err
	}
	stored, err := s.repo.AppendMeal(ctx, meal)
	if err != nil {
		return nil, err
	}
	daily, err := s.repo.GetTodaySummary(ctx, stored.LocalDate)
	if err != nil {
		return nil, err
	}
	return &domain.RecordMealResult{
		Status: domain.StatusStored,
		Meal:   stored,
		Daily:  daily,
	}, nil
}

func (s *MealService) AnswerClarification(ctx context.Context, userID int64, answerEventKey, answer string) (*domain.RecordMealResult, error) {
	s.mu.Lock()
	pending, ok := s.clarifications[userID]
	if !ok {
		s.mu.Unlock()
		return nil, nil
	}
	if s.now().After(pending.expiresAt) {
		delete(s.clarifications, userID)
		s.mu.Unlock()
		return nil, nil
	}
	delete(s.clarifications, userID)
	s.mu.Unlock()

	combined := fmt.Sprintf("%s\nУточнение пользователя: %s", pending.rawText, answer)
	return s.RecordMeal(ctx, userID, pending.eventKey, combined)
}

func (s *MealService) ConfirmMeal(ctx context.Context, userID int64, token string) (*domain.RecordMealResult, error) {
	s.mu.Lock()
	pending, ok := s.pending[token]
	if ok {
		delete(s.pending, token)
	}
	s.mu.Unlock()

	if !ok || pending.userID != userID || s.now().After(pending.expiresAt) {
		return nil, fmt.Errorf("%w: Подтверждение устарело. Отправьте приём пищи ещё раз.", domain.ErrMealValidation)
	}

	existing, err := s.repo.FindByEventKey(ctx, pending.eventKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return s.duplicateResult(ctx, existing)
	}

	meal, err := s.buildMeal(pending.userID, pending.eventKey, pending.rawText, pending.analysis)
	if err != nil {
		return nil, err
	}
	stored, err := s.repo.AppendMeal(ctx, meal)
	if err != nil {
		return nil, err
	}
	daily, err := s.repo.GetTodaySummary(ctx, stored.LocalDate)
	if err != nil {
		return nil, err
	}
	return &domain.RecordMealResult{
		Status: domain.StatusStored,
		Meal:   stored,
		Daily:  daily,
	}, nil
}

func (s *MealService) BeginEdit(ctx context.Context, userID int64) (*domain.MealRecord, error) {
	meal, err := s.repo.GetLastMeal(ctx)
	if err != nil {
		return nil, err
	}
	if meal == nil {
		return nil, fmt.Errorf("%w: Пока нет записей, которые можно исправить.", domain.ErrNoMealToEdit)
	}
	s.mu.Lock()
	s.edits[userID] = &editSession{
		mealID:    meal.MealID,
		expiresAt: s.now().Add(15 * time.Minute),
	}
	s.mu.Unlock()
	return meal, nil
}

func (s *MealService) HasEditSession(ctx context.Context, userID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.edits[userID]
	if !ok {
		return false
	}
	if s.now().After(sess.expiresAt) {
		delete(s.edits, userID)
		return false
	}
	return true
}

func (s *MealService) CorrectLast(ctx context.Context, userID int64, editEventKey, correctionText string) (*domain.RecordMealResult, error) {
	duplicate, err := s.repo.FindByEventKey(ctx, editEventKey)
	if err != nil {
		return nil, err
	}
	if duplicate != nil {
		return s.duplicateResult(ctx, duplicate)
	}

	s.mu.Lock()
	sess, ok := s.edits[userID]
	if !ok || s.now().After(sess.expiresAt) {
		delete(s.edits, userID)
		s.mu.Unlock()
		return nil, fmt.Errorf("%w: Режим исправления истёк. Снова вызовите /edit_last.", domain.ErrEditSessionExpired)
	}
	s.mu.Unlock()

	meal, err := s.repo.GetLastMeal(ctx)
	if err != nil {
		return nil, err
	}
	if meal == nil || meal.MealID != sess.mealID {
		return nil, fmt.Errorf("%w: Последняя запись изменилась. Снова вызовите /edit_last.", domain.ErrNoMealToEdit)
	}

	correction, err := s.analyzer.AnalyzeCorrection(ctx, correctionText, meal)
	if err != nil {
		return nil, err
	}
	if correction.NeedsClarification {
		q := ""
		if correction.ClarificationQuestion != nil {
			q = *correction.ClarificationQuestion
		}
		return &domain.RecordMealResult{
			Status: domain.StatusNeedsClarification,
			Meal:   meal,
			Prompt: q,
		}, nil
	}

	items := make([]domain.MealItem, 0, len(meal.Items))
	found := false
	for _, item := range meal.Items {
		if item.ItemID != correction.ItemID {
			items = append(items, item)
			continue
		}
		found = true
		ratio := correction.NewAmountG / item.AmountG
		scaled := item
		scaled.AmountG = correction.NewAmountG
		scaled.Kcal = item.Kcal * ratio
		scaled.ProteinG = item.ProteinG * ratio
		scaled.FatG = item.FatG * ratio
		scaled.CarbsG = item.CarbsG * ratio
		items = append(items, scaled)
	}

	if !found {
		return nil, fmt.Errorf("%w: Не удалось однозначно найти позицию для исправления.", domain.ErrMealValidation)
	}

	var sumKcal, sumProtein, sumFat, sumCarbs float64
	for _, it := range items {
		sumKcal += it.Kcal
		sumProtein += it.ProteinG
		sumFat += it.FatG
		sumCarbs += it.CarbsG
	}

	updated := *meal
	updated.Items = items
	updated.Kcal = math.Round(sumKcal*100) / 100
	updated.ProteinG = math.Round(sumProtein*100) / 100
	updated.FatG = math.Round(sumFat*100) / 100
	updated.CarbsG = math.Round(sumCarbs*100) / 100
	updated.Revision = meal.Revision + 1
	updated.LastEditEventKey = &editEventKey

	if err := updated.Validate(); err != nil {
		return nil, err
	}

	stored, err := s.repo.UpdateMeal(ctx, &updated)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	delete(s.edits, userID)
	s.mu.Unlock()

	daily, err := s.repo.GetTodaySummary(ctx, stored.LocalDate)
	if err != nil {
		return nil, err
	}

	return &domain.RecordMealResult{
		Status: domain.StatusStored,
		Meal:   stored,
		Daily:  daily,
	}, nil
}

func (s *MealService) Today(ctx context.Context, localDate string) (*domain.DailySummary, error) {
	if localDate == "" {
		localDate = s.now().Format("2006-01-02")
	}
	return s.repo.GetTodaySummary(ctx, localDate)
}

func (s *MealService) History(ctx context.Context, limit int) ([]*domain.MealRecord, error) {
	if limit <= 0 {
		limit = 10
	}
	return s.repo.GetRecentMeals(ctx, limit)
}

func (s *MealService) buildMeal(
	userID int64,
	eventKey, rawText string,
	analysis *domain.MealAnalysis,
) (*domain.MealRecord, error) {
	now := s.now()
	record := &domain.MealRecord{
		EventKey:       eventKey,
		MealID:         uuid.NewString(),
		TelegramUserID: userID,
		LoggedAt:       now,
		LocalDate:      now.Format("2006-01-02"),
		RawText:        rawText,
		Summary:        analysis.Summary(),
		Kcal:           analysis.Kcal(),
		ProteinG:       analysis.ProteinG(),
		FatG:           analysis.FatG(),
		CarbsG:         analysis.CarbsG(),
		Items:          analysis.Items,
		Assumptions:    analysis.Assumptions,
		Revision:       1,
	}
	if err := record.Validate(); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *MealService) duplicateResult(ctx context.Context, meal *domain.MealRecord) (*domain.RecordMealResult, error) {
	daily, err := s.repo.GetTodaySummary(ctx, meal.LocalDate)
	if err != nil {
		return nil, err
	}
	return &domain.RecordMealResult{
		Status: domain.StatusDuplicate,
		Meal:   meal,
		Daily:  daily,
	}, nil
}
