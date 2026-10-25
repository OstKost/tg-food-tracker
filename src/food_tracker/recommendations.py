from food_tracker.domain.models import DailySummary, FoodGroup, MealRecord


def daily_recommendation(summary: DailySummary, meals: list[MealRecord]) -> str:
    """Return one conservative, non-medical recommendation."""

    groups = {group for meal in meals for item in meal.items for group in item.food_groups}
    if not groups.intersection({FoodGroup.VEGETABLE, FoodGroup.FRUIT}):
        return "Если это весь день, можно добавить овощи или фрукт."
    if FoodGroup.PROTEIN not in groups:
        return "Если это весь день, можно добавить источник белка."
    return "Рацион выглядит разнообразно. Ориентируйтесь также на голод и самочувствие."
