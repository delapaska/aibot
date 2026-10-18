package domain

import (
	"errors"
	"testing"
)

func TestParseCategory(t *testing.T) {
	valid := map[string]Category{
		"food":          CategoryFood,
		"FOOD":          CategoryFood,
		" Food ":        CategoryFood,
		"transport":     CategoryTransport,
		"housing":       CategoryHousing,
		"entertainment": CategoryEntertainment,
		"health":        CategoryHealth,
		"shopping":      CategoryShopping,
		"other":         CategoryOther,
	}
	for in, want := range valid {
		got, err := ParseCategory(in)
		if err != nil {
			t.Fatalf("ParseCategory(%q) unexpected error: %v", in, err)
		}
		if got != want {
			t.Errorf("ParseCategory(%q) = %q, want %q", in, got, want)
		}
	}

	for _, in := range []string{"", "cafe", "еда", "foods"} {
		if _, err := ParseCategory(in); !errors.Is(err, ErrInvalidCategory) {
			t.Errorf("ParseCategory(%q) error = %v, want ErrInvalidCategory", in, err)
		}
	}
}

func TestCategoryTitle(t *testing.T) {
	tests := []struct {
		c    Category
		want string
	}{
		{CategoryFood, "Еда"},
		{CategoryTransport, "Транспорт"},
		{CategoryHousing, "Жильё"},
		{CategoryEntertainment, "Развлечения"},
		{CategoryHealth, "Здоровье"},
		{CategoryShopping, "Покупки"},
		{CategoryOther, "Другое"},
	}
	for _, tt := range tests {
		if got := tt.c.Title(); got != tt.want {
			t.Errorf("Category(%q).Title() = %q, want %q", tt.c, got, tt.want)
		}
	}
}

func TestCategories(t *testing.T) {
	cats := Categories()
	if len(cats) != 7 {
		t.Fatalf("len(Categories()) = %d, want 7", len(cats))
	}
	seen := make(map[Category]bool, len(cats))
	for _, c := range cats {
		if !c.Valid() {
			t.Errorf("category %q is not valid", c)
		}
		if seen[c] {
			t.Errorf("duplicate category %q", c)
		}
		seen[c] = true
	}
}
