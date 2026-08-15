package models

import "testing"

func TestChildProfileZeroValueHasNoImplicitFilters(t *testing.T) {
	var p ChildProfile
	if p.DietType != "" || p.RegionCulture != "" || len(p.Allergens) != 0 {
		t.Fatal("zero-value ChildProfile must express no preference on every optional field")
	}
	if p.Vegan {
		t.Fatal("zero-value ChildProfile must not default to vegan")
	}
}
