package enrich

import (
	"reflect"
	"testing"
)

func TestTokenise(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "drops quantities units and preparation words",
			in:   "2 tablespoon finely chopped Onion, 1 cup Rice - washed",
			want: []string{"onion", "rice"},
		},
		{
			name: "folds regional names onto one token",
			in:   "atta, besan, aloo, palak",
			want: []string{"chickpea", "potato", "spinach", "wheat"},
		},
		{
			name: "keeps the plant part, which identifies the food",
			in:   "pumpkin leaves",
			want: []string{"leave", "pumpkin"},
		},
		{
			name: "deduplicates across naming variants",
			in:   "moong dal, mung daal",
			want: []string{"dal", "mung"},
		},
		{
			name: "empty input yields no tokens",
			in:   "   2 cups of  ",
			want: []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Tokenise(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Tokenise(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestJaccard(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want float64
	}{
		{"identical sets score one", []string{"rice", "dal"}, []string{"dal", "rice"}, 1},
		{"disjoint sets score zero", []string{"rice"}, []string{"fish"}, 0},
		{"half overlap", []string{"rice", "dal"}, []string{"rice", "ghee"}, 1.0 / 3.0},
		{"empty input scores zero", nil, []string{"rice"}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := Jaccard(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("Jaccard(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// Cover must be asymmetric. Dividing by the smaller set let a short external recipe
// score a perfect cover against a longer provider recipe it only partly accounted for.
func TestCoverIsAsymmetric(t *testing.T) {
	provider := []string{"rice", "dal", "ghee"}
	external := []string{"rice"}

	if got := Cover(provider, external); got != 1.0/3.0 {
		t.Errorf("Cover(provider, external) = %v, want 1/3: the external recipe accounts for one of three provider ingredients", got)
	}
	if got := Cover(external, provider); got != 1 {
		t.Errorf("Cover(external, provider) = %v, want 1", got)
	}
}

// The guard that stopped "Peanut" matching "Groundnut oil" and "Pumpkin" matching
// "Pumpkin leaves". Both pass the coverage gate; both are different foods.
func TestUnmatchedQualifierRejectsDifferentFoods(t *testing.T) {
	qualifiers := map[string]bool{"oil": true, "leave": true, "flake": true}

	tests := []struct {
		name         string
		provider     string
		candidate    string
		wantRejected bool
	}{
		{"peanut is not groundnut oil", "Peanut", "Groundnut oil", true},
		{"pumpkin is not pumpkin leaves", "Pumpkin", "Pumpkin leaves, tender", true},
		{"rice is not rice flakes", "Rice", "Rice flakes", true},
		{"an oil may match an oil", "Mustard oil", "Mustard oil", false},
		{"a variety qualifier is still the same food", "Onion", "Onion, big", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			have := map[string]bool{}
			for _, tok := range Tokenise(tc.provider) {
				have[tok] = true
			}
			got := hasUnmatchedQualifier(Tokenise(tc.candidate), have, qualifiers)
			if got != tc.wantRejected {
				t.Errorf("hasUnmatchedQualifier(%q against %q) = %v, want %v",
					tc.candidate, tc.provider, got, tc.wantRejected)
			}
		})
	}
}

// Keyword matching must respect word boundaries. A substring match found "adai" inside
// "Vadai" and offered a deep-fried lentil snack as the method for an infant pancake.
func TestNameHasKeywordRespectsWordBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		dish     string
		keywords []string
		want     bool
	}{
		{"adai does not match vadai", "Vazhaipoo Bajra Adai Recipe", []string{"vada"}, false},
		{"adai matches adai", "Vazhaipoo Bajra Adai Recipe", []string{"adai"}, true},
		{"plural form still matches", "Mixed Vegetable Cutlets", []string{"cutlet"}, true},
		{"punctuation is a boundary", "Beetroot Kebab/Tikki Recipe", []string{"tikki"}, true},
		{"no keyword present", "Palak Paneer Recipe", []string{"khichdi"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := nameHasKeyword(tc.dish, tc.keywords); got != tc.want {
				t.Errorf("nameHasKeyword(%q, %v) = %v, want %v", tc.dish, tc.keywords, got, tc.want)
			}
		})
	}
}

func TestCertainty(t *testing.T) {
	if certainty(1.0) != "exact" {
		t.Error("a full name match must be exact")
	}
	if certainty(0.5) != "probable" {
		t.Error("a partial name match must be probable, not exact: the food is not confirmed")
	}
}
