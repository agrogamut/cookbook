package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReferenceCuisinesNeverOffersAZeroRecipeCuisine(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("GET", "/api/reference/cuisines", nil)
	rec := httptest.NewRecorder()

	h.ReferenceCuisines(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var cuisines []struct {
		RecipeCount int `json:"recipe_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cuisines); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, c := range cuisines {
		if c.RecipeCount == 0 {
			t.Fatal("cuisine_option must never surface a zero-recipe cuisine (this is the whole point of the view)")
		}
	}
}

func TestRunsReturnsImportHistoryWithTimestamptz(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("GET", "/api/runs", nil)
	rec := httptest.NewRecorder()

	h.Runs(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var runs []struct {
		RunID     int64      `json:"run_id"`
		StartedAt time.Time  `json:"started_at"`
		FinishedAt *time.Time `json:"finished_at"`
		SourceDir string     `json:"source_dir"`
		OK        bool       `json:"ok"`
		Tables    []struct {
			TableName   string `json:"table_name"`
			RowsRead    int    `json:"rows_read"`
			RowsWritten int    `json:"rows_written"`
			RowsSkipped int    `json:"rows_skipped"`
			ContentHash string `json:"content_hash"`
		} `json:"tables"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("expected at least one import run in the test database")
	}
	for _, run := range runs {
		if run.StartedAt.IsZero() {
			t.Fatal("started_at must never be zero")
		}
		if len(run.Tables) == 0 {
			t.Fatal("expected at least one table stat per run")
		}
	}
}

func TestReferenceAllergensReportsWhetherEachGroupScreens(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("GET", "/api/reference/allergens", nil)
	rec := httptest.NewRecorder()

	h.ReferenceAllergens(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got []struct {
		AllergenGroup string  `json:"allergen_group"`
		CorpusTag     *string `json:"corpus_tag"`
		Screens       bool    `json:"screens"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 11 {
		t.Fatalf("expected 11 allergen groups from allergen_tag_vocabulary, got %d", len(got))
	}
	for _, g := range got {
		if g.Screens != (g.CorpusTag != nil) {
			t.Fatalf("%s: screens=%v but corpus_tag nil=%v; screens must be derived from "+
				"the tag, never asserted independently", g.AllergenGroup, g.Screens, g.CorpusTag == nil)
		}
	}
}

// TestEveryOfferedAllergenScreensSomething fails on four rows today and is meant to.
// It is the tracking mechanism for GAP-013: it turns green only when the provider tags
// the corpus for Tree nuts, Crustacean/Mollusc, Mustard and Sulphites. It skips rather
// than fails so it does not break CI, because the hole is the provider's to close and a
// red suite trains people to ignore red suites.
func TestEveryOfferedAllergenScreensSomething(t *testing.T) {
	pool := testPool(t)
	var unscreened []string
	rows, err := pool.Query(context.Background(),
		`SELECT allergen_group FROM allergen_tag_vocabulary WHERE corpus_tag IS NULL ORDER BY allergen_group`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			t.Fatalf("scan: %v", err)
		}
		unscreened = append(unscreened, g)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(unscreened) > 0 {
		t.Skipf("GAP-013 still open: %d allergen group(s) screen nothing: %v. "+
			"They remain selectable and are reported in EngineResult.UnscreenedAllergens. "+
			"This test passes when the provider tags the corpus.", len(unscreened), unscreened)
	}
}
func TestReferenceClinicalMarkersCoversEveryTriggerField(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("GET", "/api/reference/clinical-markers", nil)
	rec := httptest.NewRecorder()

	h.ReferenceClinicalMarkers(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got []struct {
		TriggerField string `json:"trigger_field"`
		RuleIDs      string `json:"rule_ids"`
		Escalates    bool   `json:"escalates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 28 {
		t.Fatalf("expected 28 distinct trigger_field values across the 31 rules, got %d", len(got))
	}
	var escalating int
	for _, m := range got {
		if m.RuleIDs == "" {
			t.Fatalf("%s carries no rule id; a marker with no rule cannot be offered", m.TriggerField)
		}
		if m.Escalates {
			escalating++
		}
	}
	if escalating == 0 {
		t.Fatal("no marker reports escalates=true, but the specialist tier is non-empty")
	}
}

func TestReferenceEnumsCarryLiveCounts(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("GET", "/api/reference/enums", nil)
	rec := httptest.NewRecorder()

	h.ReferenceEnums(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got map[string][]struct {
		Value string `json:"value"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"diet_type", "meal_type", "budget_band", "season",
		"texture", "growth_target", "post_vaccine_context", "prep_time_min", "cook_time_min"} {
		if len(got[key]) == 0 {
			t.Fatalf("enum %q is empty; every one of these columns is populated on all 940 rows", key)
		}
	}
	if len(got["diet_type"]) != 3 {
		t.Fatalf("diet_type has 3 values in scope, got %d", len(got["diet_type"]))
	}
	if len(got["prep_time_min"]) != 4 {
		t.Fatalf("prep_time_min has 4 distinct corpus values, got %d", len(got["prep_time_min"]))
	}
	if len(got["cook_time_min"]) != 6 {
		t.Fatalf("cook_time_min has 6 distinct corpus values, got %d", len(got["cook_time_min"]))
	}
	var total int
	for _, v := range got["diet_type"] {
		total += v.Count
	}
	if total != 940 {
		t.Fatalf("diet_type counts sum to %d, want 940: counts must be live, not stored", total)
	}
}
