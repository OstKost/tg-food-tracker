package sheets

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OstKost/tg-food-tracker/internal/domain"
	"google.golang.org/api/option"
	sheetsapi "google.golang.org/api/sheets/v4"
)

var (
	MealsHeaders = []string{
		"event_key", "meal_id", "logged_at", "local_date", "raw_text",
		"summary", "kcal", "protein_g", "fat_g", "carbs_g",
		"items_json", "assumptions", "revision", "last_edit_event_key",
	}
	ReportHeaders    = []string{"report_key", "local_date", "status", "sent_at", "telegram_message_id"}
	DashboardHeaders = []string{"date", "kcal", "protein_g", "fat_g", "carbs_g"}
	DashboardFormulas = []string{
		`=TEXT(TODAY(),"yyyy-mm-dd")`,
		`=SUMIF(Meals!D2:D10000,A2,Meals!G2:G10000)`,
		`=SUMIF(Meals!D2:D10000,A2,Meals!H2:H10000)`,
		`=SUMIF(Meals!D2:D10000,A2,Meals!I2:I10000)`,
		`=SUMIF(Meals!D2:D10000,A2,Meals!J2:J10000)`,
	}
)

type GoogleSheetsRepository struct {
	spreadsheetID string
	allowedUserID int64
	location      *time.Location
	service       *sheetsapi.Service
	mu            sync.Mutex
}

func NewGoogleSheetsRepository(
	ctx context.Context,
	spreadsheetID string,
	serviceAccountFile string,
	allowedUserID int64,
	location *time.Location,
	service *sheetsapi.Service,
) (*GoogleSheetsRepository, error) {
	if service == nil {
		var err error
		service, err = sheetsapi.NewService(ctx, option.WithCredentialsFile(serviceAccountFile), option.WithScopes("https://www.googleapis.com/auth/spreadsheets"))
		if err != nil {
			return nil, fmt.Errorf("%w: failed to initialize Google Sheets service: %w", domain.ErrRepository, err)
		}
	}
	return &GoogleSheetsRepository{
		spreadsheetID: spreadsheetID,
		allowedUserID: allowedUserID,
		location:      location,
		service:       service,
	}, nil
}

func (r *GoogleSheetsRepository) Bootstrap(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	meta, err := r.service.Spreadsheets.Get(r.spreadsheetID).Fields("properties,sheets.properties").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("%w: failed to get spreadsheet metadata (%w)", domain.ErrRepository, err)
	}

	existingSheets := make(map[string]bool)
	for _, s := range meta.Sheets {
		if s.Properties != nil {
			existingSheets[s.Properties.Title] = true
		}
	}

	requests := []*sheetsapi.Request{
		{
			UpdateSpreadsheetProperties: &sheetsapi.UpdateSpreadsheetPropertiesRequest{
				Properties: &sheetsapi.SpreadsheetProperties{
					TimeZone: r.location.String(),
				},
				Fields: "timeZone",
			},
		},
	}

	for _, name := range []string{"Meals", "Dashboard", "ReportLog"} {
		if !existingSheets[name] {
			requests = append(requests, &sheetsapi.Request{
				AddSheet: &sheetsapi.AddSheetRequest{
					Properties: &sheetsapi.SheetProperties{
						Title: name,
					},
				},
			})
		}
	}

	if len(requests) > 0 {
		_, err := r.service.Spreadsheets.BatchUpdate(r.spreadsheetID, &sheetsapi.BatchUpdateSpreadsheetRequest{
			Requests: requests,
		}).Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("%w: failed to batch update sheets (%w)", domain.ErrRepository, err)
		}
	}

	if err := r.ensureRow(ctx, "Meals!A1:N1", MealsHeaders, "Meals"); err != nil {
		return err
	}
	if err := r.ensureRow(ctx, "ReportLog!A1:E1", ReportHeaders, "ReportLog"); err != nil {
		return err
	}
	if err := r.ensureRow(ctx, "Dashboard!A1:E1", DashboardHeaders, "Dashboard"); err != nil {
		return err
	}
	if err := r.ensureRow(ctx, "Dashboard!A2:E2", DashboardFormulas, "Dashboard formulas"); err != nil {
		return err
	}

	return r.validateSchemaLocked(ctx)
}

func (r *GoogleSheetsRepository) ValidateSchema(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.validateSchemaLocked(ctx)
}

func (r *GoogleSheetsRepository) validateSchemaLocked(ctx context.Context) error {
	meta, err := r.service.Spreadsheets.Get(r.spreadsheetID).
		Fields("properties.timeZone,sheets.properties.title").
		Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("%w: failed to get metadata (%w)", domain.ErrRepository, err)
	}

	tz := ""
	if meta.Properties != nil {
		tz = meta.Properties.TimeZone
	}
	if tz != r.location.String() {
		return fmt.Errorf("%w: spreadsheet timezone is %q; expected %q", domain.ErrSchema, tz, r.location.String())
	}

	existingSheets := make(map[string]bool)
	for _, s := range meta.Sheets {
		if s.Properties != nil {
			existingSheets[s.Properties.Title] = true
		}
	}
	for _, req := range []string{"Meals", "Dashboard", "ReportLog"} {
		if !existingSheets[req] {
			return fmt.Errorf("%w: missing required sheet %q", domain.ErrSchema, req)
		}
	}

	if err := r.expectRow(ctx, "Meals!A1:N1", MealsHeaders, "Meals"); err != nil {
		return err
	}
	if err := r.expectRow(ctx, "ReportLog!A1:E1", ReportHeaders, "ReportLog"); err != nil {
		return err
	}
	if err := r.expectRow(ctx, "Dashboard!A1:E1", DashboardHeaders, "Dashboard"); err != nil {
		return err
	}
	if err := r.expectRow(ctx, "Dashboard!A2:E2", DashboardFormulas, "Dashboard formulas"); err != nil {
		return err
	}

	return nil
}

func (r *GoogleSheetsRepository) ensureRow(ctx context.Context, rangeName string, expected []string, label string) error {
	current, err := r.getValues(ctx, rangeName, "FORMULA")
	if err != nil {
		return err
	}
	if len(current) == 0 || len(current[0]) == 0 {
		return r.updateValues(ctx, rangeName, [][]any{toStringInterfaceSlice(expected)}, "USER_ENTERED")
	}
	row := current[0]
	if len(row) < len(expected) {
		return fmt.Errorf("%w: %s schema is damaged; expected %v", domain.ErrSchema, label, expected)
	}
	for i, exp := range expected {
		if fmt.Sprint(row[i]) != exp {
			return fmt.Errorf("%w: %s schema is damaged; expected %v", domain.ErrSchema, label, expected)
		}
	}
	return nil
}

func (r *GoogleSheetsRepository) expectRow(ctx context.Context, rangeName string, expected []string, label string) error {
	current, err := r.getValues(ctx, rangeName, "FORMULA")
	if err != nil {
		return err
	}
	if len(current) == 0 {
		return fmt.Errorf("%w: %s schema is damaged; expected %v", domain.ErrSchema, label, expected)
	}
	row := current[0]
	if len(row) < len(expected) {
		return fmt.Errorf("%w: %s schema is damaged; expected %v", domain.ErrSchema, label, expected)
	}
	for i, exp := range expected {
		if fmt.Sprint(row[i]) != exp {
			return fmt.Errorf("%w: %s schema is damaged; expected %v", domain.ErrSchema, label, expected)
		}
	}
	return nil
}

func (r *GoogleSheetsRepository) FindByEventKey(ctx context.Context, eventKey string) (*domain.MealRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.findByEventKeyLocked(ctx, eventKey)
}

func (r *GoogleSheetsRepository) findByEventKeyLocked(ctx context.Context, eventKey string) (*domain.MealRecord, error) {
	meals, err := r.readMealsLocked(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range meals {
		if m.EventKey == eventKey || (m.LastEditEventKey != nil && *m.LastEditEventKey == eventKey) {
			return m, nil
		}
	}
	return nil, nil
}

func (r *GoogleSheetsRepository) AppendMeal(ctx context.Context, meal *domain.MealRecord) (*domain.MealRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, err := r.findByEventKeyLocked(ctx, meal.EventKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	row := r.mealToRow(meal)
	vr := &sheetsapi.ValueRange{
		Values: [][]any{row},
	}
	_, appendErr := r.service.Spreadsheets.Values.Append(r.spreadsheetID, "Meals!A:N", vr).
		ValueInputOption("RAW").
		InsertDataOption("INSERT_ROWS").
		Context(ctx).Do()
	if appendErr != nil {
		// Reconcile by event_key in case append actually completed before network/timeout error
		reconciled, err := r.findByEventKeyLocked(ctx, meal.EventKey)
		if err == nil && reconciled != nil {
			return reconciled, nil
		}
		return nil, fmt.Errorf("%w: Google Sheets append failed (%w)", domain.ErrRepository, appendErr)
	}

	return meal, nil
}

func (r *GoogleSheetsRepository) GetTodaySummary(ctx context.Context, localDate string) (*domain.DailySummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	today := time.Now().In(r.location).Format("2006-01-02")
	if localDate == today {
		dash, err := r.dashboardSummaryLocked(ctx)
		if err == nil && dash != nil && dash.LocalDate == localDate {
			return dash, nil
		}
	}

	meals, err := r.readMealsLocked(ctx)
	if err != nil {
		return nil, err
	}

	var kcal, protein, fat, carbs float64
	for _, m := range meals {
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

func (r *GoogleSheetsRepository) dashboardSummaryLocked(ctx context.Context) (*domain.DailySummary, error) {
	rows, err := r.getValues(ctx, "Dashboard!A2:E2", "UNFORMATTED_VALUE")
	if err != nil || len(rows) == 0 || len(rows[0]) < 5 {
		return nil, fmt.Errorf("%w: Dashboard has no calculated summary", domain.ErrSchema)
	}
	row := rows[0]
	dStr := fmt.Sprint(row[0])
	kcal, errK := toFloat(row[1])
	p, errP := toFloat(row[2])
	f, errF := toFloat(row[3])
	c, errC := toFloat(row[4])
	if errK != nil || errP != nil || errF != nil || errC != nil {
		return nil, fmt.Errorf("%w: Dashboard contains invalid calculated values", domain.ErrSchema)
	}
	return &domain.DailySummary{
		LocalDate: dStr,
		Kcal:      kcal,
		ProteinG:  p,
		FatG:      f,
		CarbsG:    c,
	}, nil
}

func (r *GoogleSheetsRepository) GetRecentMeals(ctx context.Context, limit int) ([]*domain.MealRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	meals, err := r.readMealsLocked(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(meals, func(i, j int) bool {
		return meals[i].LoggedAt.After(meals[j].LoggedAt)
	})
	if limit > len(meals) {
		limit = len(meals)
	}
	return meals[:limit], nil
}

func (r *GoogleSheetsRepository) GetLastMeal(ctx context.Context) (*domain.MealRecord, error) {
	recent, err := r.GetRecentMeals(ctx, 1)
	if err != nil || len(recent) == 0 {
		return nil, err
	}
	return recent[0], nil
}

func (r *GoogleSheetsRepository) UpdateMeal(ctx context.Context, meal *domain.MealRecord) (*domain.MealRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if meal.LastEditEventKey != nil && *meal.LastEditEventKey != "" {
		existing, err := r.findByEventKeyLocked(ctx, *meal.LastEditEventKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
	}

	rows, err := r.getValues(ctx, "Meals!A2:N", "UNFORMATTED_VALUE")
	if err != nil {
		return nil, err
	}

	for offset, row := range rows {
		if len(row) > 1 && fmt.Sprint(row[1]) == meal.MealID {
			targetRange := fmt.Sprintf("Meals!A%d:N%d", offset+2, offset+2)
			newRow := r.mealToRow(meal)
			if err := r.updateValues(ctx, targetRange, [][]any{newRow}, "RAW"); err != nil {
				return nil, err
			}
			return meal, nil
		}
	}

	return nil, fmt.Errorf("%w: meal not found: %s", domain.ErrRepository, meal.MealID)
}

func (r *GoogleSheetsRepository) IsReportSent(ctx context.Context, reportKey string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rec, err := r.findReportLocked(ctx, reportKey)
	if err != nil {
		return false, err
	}
	return rec != nil, nil
}

func (r *GoogleSheetsRepository) ClaimReport(ctx context.Context, reportKey string, localDate string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rec, err := r.findReportLocked(ctx, reportKey)
	if err != nil {
		return false, err
	}
	if rec != nil {
		return false, nil
	}

	vr := &sheetsapi.ValueRange{
		Values: [][]any{{reportKey, localDate, "sending", "", ""}},
	}
	_, err = r.service.Spreadsheets.Values.Append(r.spreadsheetID, "ReportLog!A:E", vr).
		ValueInputOption("RAW").
		InsertDataOption("INSERT_ROWS").
		Context(ctx).Do()
	if err != nil {
		return false, fmt.Errorf("%w: failed to claim report (%w)", domain.ErrRepository, err)
	}

	return true, nil
}

func (r *GoogleSheetsRepository) MarkReportSent(ctx context.Context, reportKey string, sentAt time.Time, telegramMessageID int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	rows, err := r.getValues(ctx, "ReportLog!A2:E", "UNFORMATTED_VALUE")
	if err != nil {
		return err
	}

	for offset, row := range rows {
		if len(row) > 0 && fmt.Sprint(row[0]) == reportKey {
			targetRange := fmt.Sprintf("ReportLog!C%d:E%d", offset+2, offset+2)
			updateData := [][]any{{"sent", sentAt.Format(time.RFC3339), telegramMessageID}}
			return r.updateValues(ctx, targetRange, updateData, "RAW")
		}
	}

	return fmt.Errorf("%w: report claim not found: %s", domain.ErrRepository, reportKey)
}

func (r *GoogleSheetsRepository) ClearPersonalData(ctx context.Context) (*domain.DeleteResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	meals, _ := r.getValues(ctx, "Meals!A2:N", "UNFORMATTED_VALUE")
	reports, _ := r.getValues(ctx, "ReportLog!A2:E", "UNFORMATTED_VALUE")

	_, errM := r.service.Spreadsheets.Values.Clear(r.spreadsheetID, "Meals!A2:N", &sheetsapi.ClearValuesRequest{}).Context(ctx).Do()
	if errM != nil {
		return nil, fmt.Errorf("%w: failed to clear Meals: %w", domain.ErrRepository, errM)
	}
	_, errR := r.service.Spreadsheets.Values.Clear(r.spreadsheetID, "ReportLog!A2:E", &sheetsapi.ClearValuesRequest{}).Context(ctx).Do()
	if errR != nil {
		return nil, fmt.Errorf("%w: failed to clear ReportLog: %w", domain.ErrRepository, errR)
	}

	return &domain.DeleteResult{
		DeletedMeals:   len(meals),
		DeletedReports: len(reports),
	}, nil
}

func (r *GoogleSheetsRepository) readMealsLocked(ctx context.Context) ([]*domain.MealRecord, error) {
	rows, err := r.getValues(ctx, "Meals!A2:N", "UNFORMATTED_VALUE")
	if err != nil {
		return nil, err
	}

	meals := make([]*domain.MealRecord, 0, len(rows))
	for _, row := range rows {
		if len(row) == 0 || strings.TrimSpace(fmt.Sprint(row[0])) == "" {
			continue
		}
		m, err := r.rowToMeal(row)
		if err != nil {
			return nil, err
		}
		meals = append(meals, m)
	}
	return meals, nil
}

func (r *GoogleSheetsRepository) findReportLocked(ctx context.Context, reportKey string) (*domain.ReportRecord, error) {
	rows, err := r.getValues(ctx, "ReportLog!A2:E", "UNFORMATTED_VALUE")
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if len(row) > 0 && fmt.Sprint(row[0]) == reportKey {
			status := ""
			if len(row) > 2 {
				status = fmt.Sprint(row[2])
			}
			return &domain.ReportRecord{
				ReportKey: reportKey,
				LocalDate: fmt.Sprint(row[1]),
				Status:    status,
			}, nil
		}
	}
	return nil, nil
}

func (r *GoogleSheetsRepository) mealToRow(meal *domain.MealRecord) []any {
	itemsJSON, _ := json.Marshal(meal.Items)
	assumptionsJSON, _ := json.Marshal(meal.Assumptions)
	lastEdit := ""
	if meal.LastEditEventKey != nil {
		lastEdit = *meal.LastEditEventKey
	}
	return []any{
		meal.EventKey,
		meal.MealID,
		meal.LoggedAt.Format(time.RFC3339),
		meal.LocalDate,
		meal.RawText,
		meal.Summary,
		meal.Kcal,
		meal.ProteinG,
		meal.FatG,
		meal.CarbsG,
		string(itemsJSON),
		string(assumptionsJSON),
		meal.Revision,
		lastEdit,
	}
}

func (r *GoogleSheetsRepository) rowToMeal(row []any) (*domain.MealRecord, error) {
	padded := make([]any, 14)
	copy(padded, row)
	for i := len(row); i < 14; i++ {
		padded[i] = ""
	}

	eventKey := fmt.Sprint(padded[0])
	mealID := fmt.Sprint(padded[1])
	loggedAtStr := fmt.Sprint(padded[2])
	localDate := fmt.Sprint(padded[3])
	rawText := fmt.Sprint(padded[4])
	summary := fmt.Sprint(padded[5])

	loggedAt, err := time.Parse(time.RFC3339, loggedAtStr)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid logged_at for event_key %s: %w", domain.ErrSchema, eventKey, err)
	}

	kcal, errK := toFloat(padded[6])
	protein, errP := toFloat(padded[7])
	fat, errF := toFloat(padded[8])
	carbs, errC := toFloat(padded[9])
	if errK != nil || errP != nil || errF != nil || errC != nil {
		return nil, fmt.Errorf("%w: invalid numbers in row for event_key %s", domain.ErrSchema, eventKey)
	}

	var items []domain.MealItem
	if err := json.Unmarshal([]byte(fmt.Sprint(padded[10])), &items); err != nil {
		return nil, fmt.Errorf("%w: invalid items_json for event_key %s: %w", domain.ErrSchema, eventKey, err)
	}

	var assumptions []string
	_ = json.Unmarshal([]byte(fmt.Sprint(padded[11])), &assumptions)

	revStr := fmt.Sprint(padded[12])
	rev, _ := strconv.Atoi(revStr)
	if rev < 1 {
		rev = 1
	}

	var lastEdit *string
	if le := strings.TrimSpace(fmt.Sprint(padded[13])); le != "" {
		lastEdit = &le
	}

	rec := &domain.MealRecord{
		EventKey:         eventKey,
		MealID:           mealID,
		TelegramUserID:   r.allowedUserID,
		LoggedAt:         loggedAt,
		LocalDate:        localDate,
		RawText:          rawText,
		Summary:          summary,
		Kcal:             kcal,
		ProteinG:         protein,
		FatG:             fat,
		CarbsG:           carbs,
		Items:            items,
		Assumptions:      assumptions,
		Revision:         rev,
		LastEditEventKey: lastEdit,
	}

	if err := rec.Validate(); err != nil {
		return nil, fmt.Errorf("%w: row validation failed for event_key %s: %w", domain.ErrSchema, eventKey, err)
	}

	return rec, nil
}

func (r *GoogleSheetsRepository) getValues(ctx context.Context, rangeName, renderOption string) ([][]any, error) {
	resp, err := r.service.Spreadsheets.Values.Get(r.spreadsheetID, rangeName).
		ValueRenderOption(renderOption).
		Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read range %s (%w)", domain.ErrRepository, rangeName, err)
	}
	return resp.Values, nil
}

func (r *GoogleSheetsRepository) updateValues(ctx context.Context, rangeName string, values [][]any, inputOption string) error {
	vr := &sheetsapi.ValueRange{Values: values}
	_, err := r.service.Spreadsheets.Values.Update(r.spreadsheetID, rangeName, vr).
		ValueInputOption(inputOption).
		Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("%w: failed to update range %s (%w)", domain.ErrRepository, rangeName, err)
	}
	return nil
}

func toStringInterfaceSlice(s []string) []any {
	res := make([]any, len(s))
	for i, v := range s {
		res[i] = v
	}
	return res
}

func toFloat(val any) (float64, error) {
	switch v := val.(type) {
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(v), 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", val)
	}
}
