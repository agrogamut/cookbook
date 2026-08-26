package book

import "testing"

// TestDefaultLanguageFallsBackOnlyWhenUnset pins the one place a real language_id must still
// win over the product default -- see ChildSummary.Language's own doc comment in types.go.
func TestDefaultLanguageFallsBackOnlyWhenUnset(t *testing.T) {
	for _, tc := range []struct {
		name       string
		languageID string
		want       string
	}{
		{"unset intake defaults to English", "", "English"},
		{"a real recorded language_id is never overridden", "bn", "bn"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := defaultLanguage(tc.languageID); got != tc.want {
				t.Errorf("defaultLanguage(%q) = %q, want %q", tc.languageID, got, tc.want)
			}
		})
	}
}
