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
	h := New(testPool(t))
	req := httptest.NewRequest("GET", "/api/reference/clinical-markers", nil)
	rec := httptest.NewRecorder()

	h.ReferenceClinicalMarkers(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	type markerValue struct {
		Value     string `json:"value"`
		RuleID    string `json:"rule_id"`
		Loadable  bool   `json:"loadable"`
		Escalates bool   `json:"escalates"`
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

	var escalatingMarkers int
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
		var fieldEscalates bool
		for _, v := range m.Values {
			if v.Value == "" {
				t.Fatalf("%s carries a value entry with an empty value string", m.TriggerField)
			}
			if v.Escalates && !v.Loadable {
				t.Fatalf("%s value %q reports escalates=true but loadable=false; an unloaded rule can never fire", m.TriggerField, v.Value)
			}
			if v.Escalates {
				fieldEscalates = true
			}
		}
		if fieldEscalates {
			escalatingMarkers++
		}
		byField[m.TriggerField] = m.Values
	}
	if escalatingMarkers == 0 {
		t.Fatal("no marker reports an escalating value, but the specialist tier is non-empty")
	}

	// Finding 1, pinned: escalation is a per-value fact. Coeliac_Status must report both of
	// its two values, with Confirmed escalating (CR-CEL-002, specialist tier) and
	// Suspected_Not_Confirmed carrying loadable=false (CR-CEL-001 sits below the specialist
	// tier and is not hard_exclude, so clinicalFilter's own query never loads it) -- a
	// field-level bool_or would say the whole field holds, which is the bug this test kills.
	coeliac := byField["Coeliac_Status"]
	if len(coeliac) != 2 {
		t.Fatalf("Coeliac_Status: expected exactly 2 values, got %d: %+v", len(coeliac), coeliac)
	}
	wantCoeliac := map[string]struct {
		loadable, escalates bool
	}{
		"Confirmed":               {true, true},
		"Suspected_Not_Confirmed": {false, false},
	}
	for _, v := range coeliac {
		want, ok := wantCoeliac[v.Value]
		if !ok {
			t.Fatalf("Coeliac_Status: unexpected value %q", v.Value)
		}
		if v.Loadable != want.loadable || v.Escalates != want.escalates {
			t.Fatalf("Coeliac_Status %q: loadable=%v escalates=%v, want loadable=%v escalates=%v",
				v.Value, v.Loadable, v.Escalates, want.loadable, want.escalates)
		}
	}

	diabetes := byField["Diabetes_Type"]
	wantDiabetes := map[string]bool{"Type 1": true, "Type 2": true}
	for _, v := range diabetes {
		if wantDiabetes[v.Value] {
			if !v.Escalates {
				t.Fatalf("Diabetes_Type %q: expected escalates=true", v.Value)
			}
			delete(wantDiabetes, v.Value)
		}
	}
	if len(wantDiabetes) != 0 {
		t.Fatalf("Diabetes_Type missing value(s) %v", wantDiabetes)
	}

	// Finding 3, pinned, and it is a provider-drift alarm: if the provider makes any of
	// these loadable, this test fails and someone must look.
	//
	// The set actually measured against the live workbook is FOURTEEN markers with no
	// loadable value, not the eleven finding 3 names. It is the eleven named there, PLUS
	// Age_Months and Texture_Skill (already known -- the prior commit marked both inert for
	// an unsupported trigger_operator, and it turns out their rules are also never loaded:
	// both sit in the Age/Feeding domain, which clinicalFilter's own WHERE clause excludes
	// outright) AND Critical_Field_Completeness (Data Quality domain, excluded by the same
	// clause, hard_exclude_yn='Y' but human_approval_level is 'Editorial/clinical workflow
	// approval' rather than the specialist tier -- finding 3's enumeration missed it). The
	// true set is pinned here rather than the predicted one.
	wantNoLoadable := map[string]bool{
		"Acute_Diarrhoea": true, "Age_Months": true, "Anemia_or_Iron_Risk": true,
		"BMI_for_Age_Classification": true, "Bone_Health_Risk": true,
		"Constipation_Support": true, "Critical_Field_Completeness": true,
		"Diet_Type": true, "Force_Feeding_or_Cue_Issue": true,
		"Growth_Faltering_Flag": true, "Multiple_Active_Rules": true,
		"Post_Vaccine_Context": true, "Severe_Food_Aversion": true,
		"Texture_Skill": true,
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

// TestUnclassifiedMarkerValuesArePinned pins the invariant the console's
// markerControl depends on. For a rule the engine loads and that fires there are exactly two
// outcomes: it escalates, or clinicalFilter refuses the whole profile with an
// unclassified-rule error. There is no third "filters something" outcome, because no
// recipe-side column expresses these conditions.
//
// So a marker value with loadable = true and escalates = false always errors, and the
// console must not offer it as a live control -- doing so promises a screen that cannot
// happen, which is the false affordance this control has already been fixed for twice.
//
// Exactly one such rule exists today, CR-ALL-001, and it is invisible to the console only
// because its trigger_operator is `contains`, which markerControl renders inert. That is a
// coincidence of one column value, not a guarantee. If the provider adds an `equals` rule at
// 'Clinical approval' with hard_exclude_yn = 'Y' in a domain outside escalationOnlyDomains,
// this test fails -- and the right response is to classify that rule in the engine, not to
// relax this assertion.
func TestUnclassifiedMarkerValuesArePinned(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("GET", "/api/reference/clinical-markers", nil)
	rec := httptest.NewRecorder()

	h.ReferenceClinicalMarkers(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got []struct {
		TriggerField    string `json:"trigger_field"`
		TriggerOperator string `json:"trigger_operator"`
		Values          []struct {
			Value     string `json:"value"`
			RuleID    string `json:"rule_id"`
			Loadable  bool   `json:"loadable"`
			Escalates bool   `json:"escalates"`
		} `json:"values"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// The known set, pinned. CR-ALL-001 is loadable (hard_exclude_yn = 'Y') but sits at
	// 'Clinical approval' in the Food Allergy domain, which escalationOnlyDomains does not
	// name -- so firing it produces the engine's classification error rather than a hold.
	// It is invisible to the console only because its operator is `contains`, which
	// markerControl renders inert.
	//
	// A NEW entry here is the dangerous case, and an `equals` or `in_list` one especially:
	// markerControl would offer it as a live control, and setting it would 500 rather than
	// filter. The fix is to classify the rule in internal/engine/clinical.go -- at the
	// specialist tier or in escalationOnlyDomains -- not to add it to this list.
	want := map[string]bool{
		"Confirmed_or_Highly_Suspected_Allergen=Allergen": true,
	}

	got2 := map[string]string{}
	for _, m := range got {
		for _, v := range m.Values {
			if v.Loadable && !v.Escalates {
				got2[m.TriggerField+"="+v.Value] = v.RuleID + ", operator " + m.TriggerOperator
			}
		}
	}
	for k, detail := range got2 {
		if !want[k] {
			t.Fatalf("new unclassified marker value %q (%s): the engine loads its rule and "+
				"would refuse the profile rather than filter, and markerControl offers any "+
				"equals/in_list value as a live control. Classify the rule in "+
				"internal/engine/clinical.go rather than adding it here.", k, detail)
		}
	}
	for k := range want {
		if _, ok := got2[k]; !ok {
			t.Fatalf("%q is no longer loadable-without-escalating. If its rule was classified, "+
				"remove it from want and let markerControl offer it.", k)
		}
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

func TestReferenceBook1BlocksAreInBookOrder(t *testing.T) {
	h := New(testPool(t))
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
