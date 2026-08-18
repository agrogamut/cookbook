package profile

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func date(s string) time.Time {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestAgeMonthsIsDerivedFromDateOfBirth(t *testing.T) {
	cases := []struct {
		name string
		dob  string
		asOf string
		want int
	}{
		{"exact month boundary", "2026-02-18", "2026-08-18", 6},
		{"one day short of the boundary", "2026-02-19", "2026-08-18", 5},
		{"exactly one year", "2025-08-18", "2026-08-18", 12},
		{"newborn", "2026-08-18", "2026-08-18", 0},
		{"leap-day birth in a non-leap year", "2024-02-29", "2026-03-01", 24},
		{"adolescent", "2008-08-18", "2026-08-18", 216},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ageMonths(date(c.dob), date(c.asOf))
			if got != c.want {
				t.Fatalf("ageMonths(%s, %s) = %d, want %d", c.dob, c.asOf, got, c.want)
			}
		})
	}
}

func TestAgeMonthsRejectsAFutureBirthDate(t *testing.T) {
	if got := ageMonths(date("2026-09-01"), date("2026-08-18")); got != -1 {
		t.Fatalf("a DOB after the reference date must return -1 so the caller can reject "+
			"it, not a plausible-looking 0; got %d", got)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	s := Stored{
		ChildID:     "TEST-CHILD-0001",
		DisplayName: "Round Trip",
		DateOfBirth: date("2023-08-18"),
		Sex:         "female",
		DietType:    "Vegetarian",
		CreatedBy:   "profile_test",
		Growth: []GrowthMeasurement{
			{MeasuredOn: date("2026-02-01"), WeightKg: ptr(12.1), HeightCm: ptr(88.0), MeasuredBy: "clinician-a"},
			{MeasuredOn: date("2026-08-01"), WeightKg: ptr(13.4), HeightCm: ptr(92.5), MeasuredBy: "clinician-a"},
		},
		Allergens: []DeclaredAllergen{
			{Group: "Peanut", Status: "confirmed", Severity: "systemic", Source: "clinician_documented", EnteredBy: "clinician-a"},
			{Group: "Milk", Status: "resolved", Source: "parent_reported", EnteredBy: "clinician-a"},
		},
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM child_profile WHERE child_id = $1`, s.ChildID)
	})

	if err := Save(ctx, pool, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(ctx, pool, s.ChildID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(got.Growth) != 2 {
		t.Fatalf("growth rows = %d, want 2; one child has many measurements and the trend "+
			"is the clinical point", len(got.Growth))
	}
	// Newest first, so a caller reading Growth[0] gets the current measurement.
	if !got.Growth[0].MeasuredOn.After(got.Growth[1].MeasuredOn) {
		t.Fatalf("growth rows must load newest first, got %v then %v",
			got.Growth[0].MeasuredOn, got.Growth[1].MeasuredOn)
	}
	if len(got.Allergens) != 2 {
		t.Fatalf("allergen rows = %d, want 2", len(got.Allergens))
	}

	// Saving again must not duplicate: this is the same upsert-and-sweep contract the
	// workbook importer holds itself to.
	if err := Save(ctx, pool, s); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	again, err := Load(ctx, pool, s.ChildID)
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if len(again.Growth) != 2 || len(again.Allergens) != 2 {
		t.Fatalf("re-saving duplicated rows: growth=%d allergens=%d", len(again.Growth), len(again.Allergens))
	}
}

func TestToChildProfileCarriesConfirmedAllergensAndDropsResolved(t *testing.T) {
	s := Stored{
		ChildID:     "T",
		DateOfBirth: date("2023-08-18"),
		DietType:    "Vegetarian",
		Allergens: []DeclaredAllergen{
			{Group: "Peanut", Status: "confirmed"},
			{Group: "Milk", Status: "resolved"},
			{Group: "Soy", Status: "suspected"},
		},
	}
	cp, notes, err := s.ToChildProfile(date("2026-08-18"))
	if err != nil {
		t.Fatalf("ToChildProfile: %v", err)
	}
	if cp.AgeMonths != 36 {
		t.Fatalf("AgeMonths = %d, want 36", cp.AgeMonths)
	}
	if len(cp.Allergens) != 1 || cp.Allergens[0] != "Peanut" {
		t.Fatalf("Allergens = %v; only confirmed allergens are hard filters", cp.Allergens)
	}
	if len(cp.SuspectedAllergens) != 1 || cp.SuspectedAllergens[0] != "Soy" {
		t.Fatalf("SuspectedAllergens = %v; suspected ranks down, it never filters", cp.SuspectedAllergens)
	}
	// A resolved allergy that silently vanished would be indistinguishable from one that
	// was never recorded, so the caller is told.
	var mentionsMilk bool
	for _, n := range notes {
		if strings.Contains(n, "Milk") {
			mentionsMilk = true
		}
	}
	if !mentionsMilk {
		t.Fatalf("notes must record that a resolved allergen was excluded from filtering; got %v", notes)
	}
}

func ptr[T any](v T) *T { return &v }

func TestToChildProfileDropsExpiredAcuteConditions(t *testing.T) {
	onset := date("2026-07-01")
	fourteen := 14

	s := Stored{
		ChildID:     "T",
		DateOfBirth: date("2023-08-18"),
		Conditions: []ClinicalCondition{
			// Entered six weeks before the reference date with a 14-day window.
			{TriggerField: "Acute_Diarrhoea", FlagValue: "Yes", Class: "acute",
				OnsetDate: &onset, ExpiresAfterDays: &fourteen},
			// Chronic conditions never expire.
			{TriggerField: "Coeliac_Status", FlagValue: "Confirmed", Class: "chronic"},
		},
	}
	cp, notes, err := s.ToChildProfile(date("2026-08-18"))
	if err != nil {
		t.Fatalf("ToChildProfile: %v", err)
	}
	if _, present := cp.ClinicalFlags["Acute_Diarrhoea"]; present {
		t.Fatal("an acute condition 48 days past a 14-day window must not still drive a " +
			"nutrition target; a stale diarrhoea flag pushing NT12 distorts every later generation")
	}
	if cp.ClinicalFlags["Coeliac_Status"] != "Confirmed" {
		t.Fatalf("a chronic condition must persist; ClinicalFlags = %v", cp.ClinicalFlags)
	}
	var explained bool
	for _, n := range notes {
		if strings.Contains(n, "Acute_Diarrhoea") {
			explained = true
		}
	}
	if !explained {
		t.Fatalf("dropping a condition must be explained, not silent; notes = %v", notes)
	}
}

func TestToChildProfileKeepsALiveAcuteCondition(t *testing.T) {
	onset := date("2026-08-15")
	fourteen := 14
	s := Stored{
		ChildID: "T", DateOfBirth: date("2023-08-18"),
		Conditions: []ClinicalCondition{
			{TriggerField: "Acute_Diarrhoea", FlagValue: "Yes", Class: "acute",
				OnsetDate: &onset, ExpiresAfterDays: &fourteen},
		},
	}
	cp, _, err := s.ToChildProfile(date("2026-08-18"))
	if err != nil {
		t.Fatalf("ToChildProfile: %v", err)
	}
	if cp.ClinicalFlags["Acute_Diarrhoea"] != "Yes" {
		t.Fatalf("an acute condition 3 days into a 14-day window is live; ClinicalFlags = %v", cp.ClinicalFlags)
	}
}

func TestToChildProfileFlagsAnAcuteConditionWithNoWindow(t *testing.T) {
	onset := date("2026-01-01")
	s := Stored{
		ChildID: "T", DateOfBirth: date("2023-08-18"),
		Conditions: []ClinicalCondition{
			// No expires_after_days: the clinical window for this class is outstanding to
			// the provider (question 12), so the code cannot invent one.
			{TriggerField: "Persistent_Vomiting", FlagValue: "Yes", Class: "acute", OnsetDate: &onset},
		},
	}
	cp, notes, err := s.ToChildProfile(date("2026-08-18"))
	if err != nil {
		t.Fatalf("ToChildProfile: %v", err)
	}
	// Kept, because dropping it would mean inventing an expiry window nobody has stated,
	// and because Persistent_Vomiting escalates -- failing to apply it is the dangerous
	// direction. But the staleness must be visible.
	if cp.ClinicalFlags["Persistent_Vomiting"] != "Yes" {
		t.Fatal("an acute condition with no stated window must still apply: inventing an " +
			"expiry is exactly what the hard rule forbids")
	}
	var warned bool
	for _, n := range notes {
		if strings.Contains(n, "229 days") || strings.Contains(n, "no expiry window") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("an acute condition with no window must be reported as possibly stale; notes = %v", notes)
	}
}
