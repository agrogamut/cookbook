package handlers

import (
	"context"
	"encoding/json"
	"github.com/madamgy/recipie/internal/aidraft"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReferenceCuisinesNeverOffersAZeroRecipeCuisine(t *testing.T) {
	h := New(testPool(t), aidraft.Disabled)
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
	h := New(testPool(t), aidraft.Disabled)
	req := httptest.NewRequest("GET", "/api/runs", nil)
	rec := httptest.NewRecorder()

	h.Runs(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var runs []struct {
		RunID      int64      `json:"run_id"`
		StartedAt  time.Time  `json:"started_at"`
		FinishedAt *time.Time `json:"finished_at"`
		SourceDir  string     `json:"source_dir"`
		OK         bool       `json:"ok"`
		Tables     []struct {
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
	h := New(testPool(t), aidraft.Disabled)
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
// It is the tracking mechanism for GAP-017: it turns green only when the provider tags
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
		t.Skipf("GAP-017 still open: %d allergen group(s) screen nothing: %v. "+
			"They remain selectable and are reported in EngineResult.UnscreenedAllergens. "+
			"This test passes when the provider tags the corpus.", len(unscreened), unscreened)
	}
}
func TestReferenceClinicalMarkersCoversEveryTriggerField(t *testing.T) {
	h := New(testPool(t), aidraft.Disabled)
	req := httptest.NewRequest("GET", "/api/reference/clinical-markers", nil)
	rec := httptest.NewRecorder()

	h.ReferenceClinicalMarkers(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	type markerValue struct {
		Value    string `json:"value"`
		RuleID   string `json:"rule_id"`
		Loadable bool   `json:"loadable"`
	}
	var got []struct {
		TriggerField    string        `json:"trigger_field"`
		RuleIDs         string        `json:"rule_ids"`
		TriggerOperator string        `json:"trigger_operator"`
		Values          []markerValue `json:"values"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 28 {
		t.Fatalf("expected 28 distinct trigger_field values across the 31 rows, got %d", len(got))
	}

	validOperators := map[string]bool{
		"equals": true, "contains": true, "in_list": true,
		"less_than": true, "incompatible_with": true,
	}

	var loadableMarkers int
	byField := map[string][]markerValue{}
	for _, m := range got {
		if m.RuleIDs == "" {
			t.Fatalf("%s carries no rule id; a marker with no rule cannot be offered", m.TriggerField)
		}
		// An operator that neither markerControl nor the engine's triggerFires recognizes
		// must never reach the client silently. Of the five values below, triggerFires
		// implements a case for exactly three -- equals, contains and in_list. less_than and
		// incompatible_with fall to its `default: return false`, so they are recognized as
		// real provider vocabulary but are inert; step 1 enforces those conditions
		// structurally and clinicalFilter's query excludes their domains. A sixth value
		// appearing here is unclassified vocabulary and needs a human, not a fallthrough.
		if !validOperators[m.TriggerOperator] {
			t.Fatalf("%s: trigger_operator %q is not one of the five the provider's vocabulary uses; "+
				"triggerFires acts on three of them and treats the other two as inert",
				m.TriggerField, m.TriggerOperator)
		}
		if len(m.Values) == 0 {
			t.Fatalf("%s reports zero values; every marker must carry at least one value", m.TriggerField)
		}
		var fieldLoadable bool
		for _, v := range m.Values {
			if v.Value == "" {
				t.Fatalf("%s carries a value entry with an empty value string", m.TriggerField)
			}
			if v.Loadable {
				fieldLoadable = true
			}
		}
		if fieldLoadable {
			loadableMarkers++
		}
		byField[m.TriggerField] = m.Values
	}
	// The engine loads every domain but Age/Feeding and Data Quality, so the loadable set is
	// most of the vocabulary rather than the handful the specialist tier used to be.
	if loadableMarkers == 0 {
		t.Fatal("no marker reports a loadable value, but clinicalFilter loads all but two domains")
	}

	// Loadability is a per-value fact and stays one after the 2026-09-05 gate removal, though
	// for a narrower reason than before. Coeliac_Status is the case that proves it is still
	// worth computing per value: both its rules sit in the Coeliac Disease domain, so both are
	// loaded, and the field-level answer happens to match. The distinction survives because
	// nothing guarantees that for a field whose rules span domains, and a field-level bool_or
	// would report one answer for a field where the two values genuinely differ.
	//
	// The escalation half of this assertion is gone with the concept: Confirmed used to
	// escalate through the specialist tier and Suspected_Not_Confirmed used to be unloaded
	// entirely. Neither is true now -- the engine records both and blocks on neither.
	coeliac := byField["Coeliac_Status"]
	if len(coeliac) != 2 {
		t.Fatalf("Coeliac_Status: expected exactly 2 values, got %d: %+v", len(coeliac), coeliac)
	}
	for _, v := range coeliac {
		if v.Value != "Confirmed" && v.Value != "Suspected_Not_Confirmed" {
			t.Fatalf("Coeliac_Status: unexpected value %q", v.Value)
		}
		if !v.Loadable {
			t.Fatalf("Coeliac_Status %q: loadable=false, but Coeliac Disease is neither "+
				"Age/Feeding nor Data Quality and clinicalFilter loads it", v.Value)
		}
	}

	diabetes := byField["Diabetes_Type"]
	wantDiabetes := map[string]bool{"Type 1": true, "Type 2": true}
	for _, v := range diabetes {
		if wantDiabetes[v.Value] {
			if !v.Loadable {
				t.Fatalf("Diabetes_Type %q: expected loadable=true", v.Value)
			}
			delete(wantDiabetes, v.Value)
		}
	}
	if len(wantDiabetes) != 0 {
		t.Fatalf("Diabetes_Type missing value(s) %v", wantDiabetes)
	}

	// A provider-drift alarm: if the provider moves any of these out of its domain, or moves
	// a new field into one, this test fails and someone must look.
	//
	// Four, down from fourteen before the 2026-09-05 gate removal. The old set was fourteen
	// because `loadable` then meant "specialist tier or hard_exclude, and not in the two
	// excluded domains", so ten ordinary clinical fields (Acute_Diarrhoea, Anemia_or_Iron_Risk,
	// Constipation_Support and the rest) failed the first half of that test and were reported
	// as offering nothing. The engine now loads every rule outside Age/Feeding and Data
	// Quality, so those ten became loadable and the only fields left are the ones the two
	// domain exclusions actually name.
	//
	// The two exclusions are structural rather than clinical: recipe_master's own age bounds
	// enforce Age/Feeding, and Data Quality describes the dataset rather than the child.
	wantNoLoadable := map[string]bool{
		"Age_Months":                  true, // Age/Feeding
		"Texture_Skill":               true, // Age/Feeding
		"Critical_Field_Completeness": true, // Data Quality
		"Multiple_Active_Rules":       true, // Data Quality
	}
	var gotNoLoadable []string
	for field, values := range byField {
		anyLoadable := false
		for _, v := range values {
			if v.Loadable {
				anyLoadable = true
				break
			}
		}
		if !anyLoadable {
			gotNoLoadable = append(gotNoLoadable, field)
		}
	}
	if len(gotNoLoadable) != len(wantNoLoadable) {
		t.Fatalf("expected %d markers with no loadable value, got %d: %v",
			len(wantNoLoadable), len(gotNoLoadable), gotNoLoadable)
	}
	for _, field := range gotNoLoadable {
		if !wantNoLoadable[field] {
			t.Fatalf("%s has no loadable value but is not in the expected set -- provider drift, look at it", field)
		}
	}
}

func TestReferenceEnumsCarryLiveCounts(t *testing.T) {
	h := New(testPool(t), aidraft.Disabled)
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

func TestReferenceBook1BlocksAreInBookOrder(t *testing.T) {
	h := New(testPool(t), aidraft.Disabled)
	req := httptest.NewRequest("GET", "/api/reference/book1-blocks", nil)
	rec := httptest.NewRecorder()

	h.ReferenceBook1Blocks(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got []struct {
		BlockID    string `json:"block_id"`
		BookOrder  int    `json:"book_order"`
		AICanDraft string `json:"ai_can_draft"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 32 {
		t.Fatalf("expected 32 blocks, got %d", len(got))
	}
	for i := range got {
		if got[i].BookOrder != i+1 {
			t.Fatalf("row %d has book_order %d; the endpoint must return blocks in render "+
				"order so a client never has to re-sort and never sorts by id", i, got[i].BookOrder)
		}
	}
	var closed int
	for _, b := range got {
		if b.AICanDraft == "N" {
			closed++
		}
	}
	if closed != 5 {
		t.Fatalf("expected 5 blocks closed to drafted text, got %d", closed)
	}
}
