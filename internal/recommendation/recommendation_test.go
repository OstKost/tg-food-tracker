package recommendation

import (
	"testing"

	"github.com/OstKost/tg-food-tracker/internal/domain"
)

func TestDailyRecommendation(t *testing.T) {
	// No veggies or fruit
	meals1 := []*domain.MealRecord{
		{
			Items: []domain.MealItem{
				{FoodGroups: []domain.FoodGroup{domain.FoodGroupProtein}},
			},
		},
	}
	res1 := DailyRecommendation(nil, meals1)
	if res1 != "Если это весь день, можно добавить овощи или фрукт." {
		t.Fatalf("unexpected recommendation: %s", res1)
	}

	// Has veggie, no protein
	meals2 := []*domain.MealRecord{
		{
			Items: []domain.MealItem{
				{FoodGroups: []domain.FoodGroup{domain.FoodGroupVegetable}},
			},
		},
	}
	res2 := DailyRecommendation(nil, meals2)
	if res2 != "Если это весь день, можно добавить источник белка." {
		t.Fatalf("unexpected recommendation: %s", res2)
	}

	// Has both
	meals3 := []*domain.MealRecord{
		{
			Items: []domain.MealItem{
				{FoodGroups: []domain.FoodGroup{domain.FoodGroupFruit, domain.FoodGroupProtein}},
			},
		},
	}
	res3 := DailyRecommendation(nil, meals3)
	if res3 != "Рацион выглядит разнообразно. Ориентируйтесь также на голод и самочувствие." {
		t.Fatalf("unexpected recommendation: %s", res3)
	}
}
