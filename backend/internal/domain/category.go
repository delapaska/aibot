package domain

import (
	"fmt"
	"strings"
)

// Category is one of the fixed expense categories, stored as a slug.
type Category string

const (
	CategoryFood          Category = "food"
	CategoryTransport     Category = "transport"
	CategoryHousing       Category = "housing"
	CategoryEntertainment Category = "entertainment"
	CategoryHealth        Category = "health"
	CategoryShopping      Category = "shopping"
	CategoryOther         Category = "other"
)

// allCategories is the fixed display order used by the panel and prompts.
var allCategories = []Category{
	CategoryFood,
	CategoryTransport,
	CategoryHousing,
	CategoryEntertainment,
	CategoryHealth,
	CategoryShopping,
	CategoryOther,
}

// titles maps categories to Russian display names used in bot replies
// and the web panel.
var titles = map[Category]string{
	CategoryFood:          "Еда",
	CategoryTransport:     "Транспорт",
	CategoryHousing:       "Жильё",
	CategoryEntertainment: "Развлечения",
	CategoryHealth:        "Здоровье",
	CategoryShopping:      "Покупки",
	CategoryOther:         "Другое",
}

// Categories returns all categories in display order.
func Categories() []Category {
	return append([]Category(nil), allCategories...)
}

// ParseCategory parses a category slug. Only the exact slug is accepted:
// mapping free text ("кофе") to a category is the agent's job, not a
// domain concern.
func ParseCategory(s string) (Category, error) {
	c := Category(strings.ToLower(strings.TrimSpace(s)))
	if !c.Valid() {
		return "", fmt.Errorf("%w: %q", ErrInvalidCategory, s)
	}
	return c, nil
}

// Valid reports whether c is one of the known categories.
func (c Category) Valid() bool {
	for _, known := range allCategories {
		if c == known {
			return true
		}
	}
	return false
}

// Title returns the Russian display name.
func (c Category) Title() string {
	if t, ok := titles[c]; ok {
		return t
	}
	return string(c)
}
