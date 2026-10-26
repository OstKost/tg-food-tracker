package domain

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type FoodGroup string

const (
	FoodGroupProtein   FoodGroup = "protein"
	FoodGroupVegetable FoodGroup = "vegetable"
	FoodGroupFruit     FoodGroup = "fruit"
	FoodGroupGrain     FoodGroup = "grain"
	FoodGroupDairy     FoodGroup = "dairy"
	FoodGroupFat       FoodGroup = "fat"
	FoodGroupSweet     FoodGroup = "sweet"
	FoodGroupDrink     FoodGroup = "drink"
	FoodGroupOther     FoodGroup = "other"
)

func IsValidFoodGroup(fg FoodGroup) bool {
	switch fg {
	case FoodGroupProtein, FoodGroupVegetable, FoodGroupFruit, FoodGroupGrain,
		FoodGroupDairy, FoodGroupFat, FoodGroupSweet, FoodGroupDrink, FoodGroupOther:
		return true
	default:
		return false
	}
}

func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

type MealItem struct {
	ItemID      string      `json:"item_id"`
	Name        string      `json:"name"`
	AmountG     float64     `json:"amount_g"`
	Kcal        float64     `json:"kcal"`
	ProteinG    float64     `json:"protein_g"`
	FatG        float64     `json:"fat_g"`
	CarbsG      float64     `json:"carbs_g"`
	FoodGroups  []FoodGroup `json:"food_groups"`
	Confidence  float64     `json:"confidence"`
	Assumptions []string    `json:"assumptions"`
}

func (item *MealItem) Validate() error {
	item.ItemID = strings.TrimSpace(item.ItemID)
	if len(item.ItemID) < 1 || len(item.ItemID) > 64 {
		return fmt.Errorf("%w: item_id must be between 1 and 64 characters", ErrMealValidation)
	}

	item.Name = strings.TrimSpace(item.Name)
	if len(item.Name) < 1 || len(item.Name) > 160 {
		return fmt.Errorf("%w: name cannot be blank and must be <= 160 characters", ErrMealValidation)
	}

	if !isFinite(item.AmountG) || item.AmountG <= 0 || item.AmountG > 5000 {
		return fmt.Errorf("%w: amount_g must be in (0, 5000]", ErrMealValidation)
	}

	if !isFinite(item.Kcal) || item.Kcal < 0 || item.Kcal > 10000 {
		return fmt.Errorf("%w: kcal must be in [0, 10000]", ErrMealValidation)
	}

	if !isFinite(item.ProteinG) || item.ProteinG < 0 || item.ProteinG > 1000 {
		return fmt.Errorf("%w: protein_g must be in [0, 1000]", ErrMealValidation)
	}

	if !isFinite(item.FatG) || item.FatG < 0 || item.FatG > 1000 {
		return fmt.Errorf("%w: fat_g must be in [0, 1000]", ErrMealValidation)
	}

	if !isFinite(item.CarbsG) || item.CarbsG < 0 || item.CarbsG > 2000 {
		return fmt.Errorf("%w: carbs_g must be in [0, 2000]", ErrMealValidation)
	}

	if len(item.FoodGroups) > 5 {
		return fmt.Errorf("%w: food_groups must contain at most 5 items", ErrMealValidation)
	}
	for _, fg := range item.FoodGroups {
		if !IsValidFoodGroup(fg) {
			return fmt.Errorf("%w: invalid food group %q", ErrMealValidation, fg)
		}
	}

	if !isFinite(item.Confidence) || item.Confidence < 0 || item.Confidence > 1 {
		return fmt.Errorf("%w: confidence must be in [0, 1]", ErrMealValidation)
	}

	if len(item.Assumptions) > 10 {
		return fmt.Errorf("%w: assumptions must contain at most 10 items", ErrMealValidation)
	}

	if item.FoodGroups == nil {
		item.FoodGroups = []FoodGroup{}
	}
	if item.Assumptions == nil {
		item.Assumptions = []string{}
	}

	return nil
}

type MealAnalysis struct {
	SchemaVersion         string     `json:"schema_version"`
	Items                 []MealItem `json:"items"`
	Assumptions           []string   `json:"assumptions"`
	Confidence            float64    `json:"confidence"`
	NeedsClarification    bool       `json:"needs_clarification"`
	ClarificationQuestion *string    `json:"clarification_question"`
	HighCalorieReason     *string    `json:"high_calorie_reason"`
}

func (a *MealAnalysis) Validate() error {
	if a.SchemaVersion != "1.0" {
		return fmt.Errorf("%w: invalid schema_version %q", ErrMealValidation, a.SchemaVersion)
	}
	if len(a.Items) < 1 || len(a.Items) > 20 {
		return fmt.Errorf("%w: items count must be in [1, 20]", ErrMealValidation)
	}
	if len(a.Assumptions) > 20 {
		return fmt.Errorf("%w: assumptions count must be <= 20", ErrMealValidation)
	}
	if !isFinite(a.Confidence) || a.Confidence < 0 || a.Confidence > 1 {
		return fmt.Errorf("%w: confidence must be in [0, 1]", ErrMealValidation)
	}

	itemIDs := make(map[string]struct{}, len(a.Items))
	for i := range a.Items {
		if err := a.Items[i].Validate(); err != nil {
			return err
		}
		if _, exists := itemIDs[a.Items[i].ItemID]; exists {
			return fmt.Errorf("%w: item_id values must be unique: %q", ErrMealValidation, a.Items[i].ItemID)
		}
		itemIDs[a.Items[i].ItemID] = struct{}{}
	}

	if a.NeedsClarification {
		if a.ClarificationQuestion == nil || strings.TrimSpace(*a.ClarificationQuestion) == "" {
			return fmt.Errorf("%w: clarification_question is required when needs_clarification is true", ErrMealValidation)
		}
	}

	if a.ClarificationQuestion != nil && len(*a.ClarificationQuestion) > 500 {
		return fmt.Errorf("%w: clarification_question must be <= 500 characters", ErrMealValidation)
	}
	if a.HighCalorieReason != nil && len(*a.HighCalorieReason) > 500 {
		return fmt.Errorf("%w: high_calorie_reason must be <= 500 characters", ErrMealValidation)
	}

	if a.Assumptions == nil {
		a.Assumptions = []string{}
	}

	return nil
}

func (a *MealAnalysis) Kcal() float64 {
	var total float64
	for _, item := range a.Items {
		total += item.Kcal
	}
	return total
}

func (a *MealAnalysis) ProteinG() float64 {
	var total float64
	for _, item := range a.Items {
		total += item.ProteinG
	}
	return total
}

func (a *MealAnalysis) FatG() float64 {
	var total float64
	for _, item := range a.Items {
		total += item.FatG
	}
	return total
}

func (a *MealAnalysis) CarbsG() float64 {
	var total float64
	for _, item := range a.Items {
		total += item.CarbsG
	}
	return total
}

func (a *MealAnalysis) Summary() string {
	names := make([]string, 0, len(a.Items))
	for _, item := range a.Items {
		names = append(names, item.Name)
	}
	return strings.Join(names, ", ")
}

type CorrectionAnalysis struct {
	SchemaVersion         string  `json:"schema_version"`
	ItemID                string  `json:"item_id"`
	NewAmountG           float64 `json:"new_amount_g"`
	Confidence            float64 `json:"confidence"`
	NeedsClarification    bool    `json:"needs_clarification"`
	ClarificationQuestion *string `json:"clarification_question"`
}

func (c *CorrectionAnalysis) Validate() error {
	if c.SchemaVersion != "1.0" {
		return fmt.Errorf("%w: invalid schema_version %q", ErrMealValidation, c.SchemaVersion)
	}
	c.ItemID = strings.TrimSpace(c.ItemID)
	if len(c.ItemID) < 1 || len(c.ItemID) > 64 {
		return fmt.Errorf("%w: item_id must be between 1 and 64 characters", ErrMealValidation)
	}
	if !isFinite(c.NewAmountG) || c.NewAmountG <= 0 || c.NewAmountG > 5000 {
		return fmt.Errorf("%w: new_amount_g must be in (0, 5000]", ErrMealValidation)
	}
	if !isFinite(c.Confidence) || c.Confidence < 0 || c.Confidence > 1 {
		return fmt.Errorf("%w: confidence must be in [0, 1]", ErrMealValidation)
	}
	if c.NeedsClarification {
		if c.ClarificationQuestion == nil || strings.TrimSpace(*c.ClarificationQuestion) == "" {
			return fmt.Errorf("%w: clarification_question is required when needs_clarification is true", ErrMealValidation)
		}
	}
	if c.ClarificationQuestion != nil && len(*c.ClarificationQuestion) > 500 {
		return fmt.Errorf("%w: clarification_question must be <= 500 characters", ErrMealValidation)
	}
	return nil
}

type MealRecord struct {
	EventKey          string     `json:"event_key"`
	MealID            string     `json:"meal_id"`
	TelegramUserID    int64      `json:"telegram_user_id"`
	LoggedAt          time.Time  `json:"logged_at"`
	LocalDate         string     `json:"local_date"` // YYYY-MM-DD
	RawText           string     `json:"raw_text"`
	Summary           string     `json:"summary"`
	Kcal              float64    `json:"kcal"`
	ProteinG          float64    `json:"protein_g"`
	FatG              float64    `json:"fat_g"`
	CarbsG            float64    `json:"carbs_g"`
	Items             []MealItem `json:"items"`
	Assumptions       []string   `json:"assumptions"`
	Revision          int        `json:"revision"`
	LastEditEventKey  *string    `json:"last_edit_event_key"`
}

func (m *MealRecord) Validate() error {
	if len(m.EventKey) < 3 || len(m.EventKey) > 100 {
		return fmt.Errorf("%w: event_key must be between 3 and 100 characters", ErrMealValidation)
	}
	if len(m.MealID) < 1 || len(m.MealID) > 64 {
		return fmt.Errorf("%w: meal_id must be between 1 and 64 characters", ErrMealValidation)
	}
	if m.TelegramUserID <= 0 {
		return fmt.Errorf("%w: telegram_user_id must be > 0", ErrMealValidation)
	}
	if m.LoggedAt.Location() == nil {
		return fmt.Errorf("%w: logged_at must include a timezone", ErrMealValidation)
	}
	if len(m.LocalDate) != 10 {
		return fmt.Errorf("%w: local_date must be formatted YYYY-MM-DD", ErrMealValidation)
	}
	if len(m.RawText) < 1 || len(m.RawText) > 4000 {
		return fmt.Errorf("%w: raw_text must be between 1 and 4000 characters", ErrMealValidation)
	}
	if len(m.Summary) < 1 || len(m.Summary) > 1000 {
		return fmt.Errorf("%w: summary must be between 1 and 1000 characters", ErrMealValidation)
	}
	if !isFinite(m.Kcal) || m.Kcal < 0 {
		return fmt.Errorf("%w: kcal must be finite non-negative", ErrMealValidation)
	}
	if !isFinite(m.ProteinG) || m.ProteinG < 0 {
		return fmt.Errorf("%w: protein_g must be finite non-negative", ErrMealValidation)
	}
	if !isFinite(m.FatG) || m.FatG < 0 {
		return fmt.Errorf("%w: fat_g must be finite non-negative", ErrMealValidation)
	}
	if !isFinite(m.CarbsG) || m.CarbsG < 0 {
		return fmt.Errorf("%w: carbs_g must be finite non-negative", ErrMealValidation)
	}
	if len(m.Items) < 1 || len(m.Items) > 20 {
		return fmt.Errorf("%w: items count must be between 1 and 20", ErrMealValidation)
	}
	if len(m.Assumptions) > 20 {
		return fmt.Errorf("%w: assumptions count must be <= 20", ErrMealValidation)
	}
	if m.Revision < 1 {
		return fmt.Errorf("%w: revision must be >= 1", ErrMealValidation)
	}
	if m.LastEditEventKey != nil && (len(*m.LastEditEventKey) < 1 || len(*m.LastEditEventKey) > 100) {
		return fmt.Errorf("%w: last_edit_event_key must be <= 100 characters", ErrMealValidation)
	}

	var sumKcal, sumProtein, sumFat, sumCarbs float64
	for i := range m.Items {
		if err := m.Items[i].Validate(); err != nil {
			return err
		}
		sumKcal += m.Items[i].Kcal
		sumProtein += m.Items[i].ProteinG
		sumFat += m.Items[i].FatG
		sumCarbs += m.Items[i].CarbsG
	}

	const tol = 0.05
	if math.Abs(m.Kcal-sumKcal) > tol ||
		math.Abs(m.ProteinG-sumProtein) > tol ||
		math.Abs(m.FatG-sumFat) > tol ||
		math.Abs(m.CarbsG-sumCarbs) > tol {
		return fmt.Errorf("%w: meal totals must be calculated from items (kcal: %.2f vs %.2f, p: %.2f vs %.2f, f: %.2f vs %.2f, c: %.2f vs %.2f)",
			ErrMealValidation, m.Kcal, sumKcal, m.ProteinG, sumProtein, m.FatG, sumFat, m.CarbsG, sumCarbs)
	}

	if m.Assumptions == nil {
		m.Assumptions = []string{}
	}

	return nil
}

type DailySummary struct {
	LocalDate string  `json:"local_date"` // YYYY-MM-DD
	Kcal      float64 `json:"kcal"`
	ProteinG  float64 `json:"protein_g"`
	FatG      float64 `json:"fat_g"`
	CarbsG    float64 `json:"carbs_g"`
}

type ReportRecord struct {
	ReportKey         string     `json:"report_key"`
	LocalDate         string     `json:"local_date"`
	Status            string     `json:"status"` // "sending" or "sent"
	SentAt            *time.Time `json:"sent_at"`
	TelegramMessageID *int       `json:"telegram_message_id"`
}

type RecordMealStatus string

const (
	StatusStored             RecordMealStatus = "stored"
	StatusDuplicate          RecordMealStatus = "duplicate"
	StatusNeedsClarification RecordMealStatus = "needs_clarification"
	StatusNeedsConfirmation  RecordMealStatus = "needs_confirmation"
)

type RecordMealResult struct {
	Status            RecordMealStatus `json:"status"`
	Meal              *MealRecord      `json:"meal,omitempty"`
	Daily             *DailySummary    `json:"daily,omitempty"`
	Prompt            string           `json:"prompt,omitempty"`
	ConfirmationToken string           `json:"confirmation_token,omitempty"`
}

type DeleteResult struct {
	DeletedMeals   int `json:"deleted_meals"`
	DeletedReports int `json:"deleted_reports"`
}
