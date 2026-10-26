package domain

import "errors"

var (
	// ErrFoodTracker is the base error.
	ErrFoodTracker = errors.New("food tracker error")

	// ErrAnalysis indicates the AI provider could not produce a valid analysis.
	ErrAnalysis = errors.New("analysis error")

	// ErrMealValidation indicates a meal violates application-level nutrition limits.
	ErrMealValidation = errors.New("meal validation error")

	// ErrRepository indicates the sheet cannot be read or changed safely.
	ErrRepository = errors.New("repository error")

	// ErrSchema indicates the spreadsheet schema differs from the expected schema.
	ErrSchema = errors.New("spreadsheet schema error")

	// ErrNoMealToEdit indicates there is no prior meal available for correction.
	ErrNoMealToEdit = errors.New("no meal to edit")

	// ErrEditSessionExpired indicates a correction was attempted after the edit window closed.
	ErrEditSessionExpired = errors.New("edit session expired")
)
