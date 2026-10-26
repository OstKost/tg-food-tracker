package recommendation

import (
	"github.com/OstKost/tg-food-tracker/internal/domain"
)

// DailyRecommendation returns one conservative, non-medical recommendation.
func DailyRecommendation(summary *domain.DailySummary, meals []*domain.MealRecord) string {
	groups := make(map[domain.FoodGroup]struct{})
	for _, meal := range meals {
		for _, item := range meal.Items {
			for _, fg := range item.FoodGroups {
				groups[fg] = struct{}{}
			}
		}
	}

	_, hasVeg := groups[domain.FoodGroupVegetable]
	_, hasFruit := groups[domain.FoodGroupFruit]
	if !hasVeg && !hasFruit {
		return "Если это весь день, можно добавить овощи или фрукт."
	}

	if _, hasProtein := groups[domain.FoodGroupProtein]; !hasProtein {
		return "Если это весь день, можно добавить источник белка."
	}

	return "Рацион выглядит разнообразно. Ориентируйтесь также на голод и самочувствие."
}
