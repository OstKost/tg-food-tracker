class FoodTrackerError(Exception):
    """Base application error safe to handle at the boundary."""


class AnalysisError(FoodTrackerError):
    """The AI provider could not produce a valid analysis."""


class MealValidationError(FoodTrackerError):
    """A meal violates application-level nutrition limits."""


class RepositoryError(FoodTrackerError):
    """The sheet cannot be read or changed safely."""


class SchemaError(RepositoryError):
    """The spreadsheet schema differs from the expected schema."""


class NoMealToEdit(FoodTrackerError):
    """There is no prior meal available for correction."""


class EditSessionExpired(FoodTrackerError):
    """A correction was attempted after the edit window closed."""

