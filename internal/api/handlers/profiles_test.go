package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// putProfileBody is the minimum a profile needs: an id and a date of birth. Everything
// else is optional, because a consultation does not always capture everything and a
// half-filled profile is more useful than a rejected one.
const putProfileBody = `{
  "child_id": "TEST-CHILD-001",
  "case_id": "TEST-CASE-001",
  "date_of_birth": "2023-08-18",
  "diet_type": "Vegetarian",
  "region_culture": "West Bengal / East India",
  "created_by": "integration-test",
  "allergens": [{"group": "Peanut", "status": "confirmed", "source": "clinician_documented", "entered_by": "integration-test"}]
}`

func profileRouter(t *testing.T) (*chi.Mux, *Handlers) {
	t.Helper()
	h := New(testPool(t))
	r := chi.NewRouter()
	r.Put("/api/profiles/{childID}", h.PutProfile)
	r.Get("/api/profiles/{childID}", h.GetProfile)
	r.Get("/api/profiles/{childID}/engine-input", h.GetProfileEngineInput)
	return r, h
}

// cleanupProfile removes the fixture. Child rows cascade from child_profile, so deleting
// the parent is enough.
func cleanupProfile(t *testing.T, h *Handlers) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(),
			`DELETE FROM child_profile WHERE child_id = 'TEST-CHILD-001'`)
	})
}

func TestProfileRoundTripsThroughHTTP(t *testing.T) {
	r, h := profileRouter(t)
	cleanupProfile(t, h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/profiles/TEST-CHILD-001",
		bytes.NewBufferString(putProfileBody))
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/profiles/TEST-CHILD-001", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		ChildID     string `json:"child_id"`
		DateOfBirth string `json:"date_of_birth"`
		DietType    string `json:"diet_type"`
		Allergens   []struct {
			Group  string `json:"group"`
			Status string `json:"status"`
		} `json:"allergens"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ChildID != "TEST-CHILD-001" {
		t.Fatalf("child_id round-trip: got %q", got.ChildID)
	}
	if got.DateOfBirth != "2023-08-18" {
		t.Fatalf("date_of_birth must round-trip as a plain date, got %q", got.DateOfBirth)
	}
	if got.DietType != "Vegetarian" {
		t.Fatalf("diet_type round-trip: got %q", got.DietType)
	}
	if len(got.Allergens) != 1 || got.Allergens[0].Group != "Peanut" {
		t.Fatalf("child allergens did not round-trip: %+v", got.Allergens)
	}
}

// A body naming a different child than the path would write to one id while the caller
// believes it wrote to another.
func TestPutProfileRejectsAChildIDMismatch(t *testing.T) {
	r, _ := profileRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("PUT", "/api/profiles/OTHER-CHILD",
		bytes.NewBufferString(putProfileBody)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when the body's child_id differs from the path, got %d: %s",
			rec.Code, rec.Body.String())
	}
}

func TestGetProfileReturns404ForUnknownChild(t *testing.T) {
	r, _ := profileRouter(t)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/profiles/NO-SUCH-CHILD", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown child, got %d", rec.Code)
	}
}

func TestEngineInputReportsDroppedFacts(t *testing.T) {
	r, h := profileRouter(t)
	cleanupProfile(t, h)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("PUT", "/api/profiles/TEST-CHILD-001",
		bytes.NewBufferString(putProfileBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET",
		"/api/profiles/TEST-CHILD-001/engine-input", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("engine-input: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Profile struct {
			AgeMonths int      `json:"age_months"`
			Allergens []string `json:"allergens"`
		} `json:"profile"`
		Dropped []string `json:"dropped"`
		AsOf    string   `json:"as_of"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Born 2023-08-18, so at any point from August 2026 the child is at least 36 months.
	if got.Profile.AgeMonths < 36 {
		t.Fatalf("age must be derived from date_of_birth, got %d months", got.Profile.AgeMonths)
	}
	if len(got.Profile.Allergens) != 1 || got.Profile.Allergens[0] != "Peanut" {
		t.Fatalf("a confirmed allergen must reach the engine query: %+v", got.Profile.Allergens)
	}
	if got.AsOf == "" {
		t.Fatal("as_of must be reported: age is derived from it, so a response without it is not reproducible")
	}
	// dropped may legitimately be empty for this fixture. The contract is that the key is
	// always present and never null, so a caller can render it without a nil check.
	if got.Dropped == nil {
		t.Fatal("dropped must serialise as [] rather than null")
	}
}

// The write path is where a bad vocabulary value has to be caught. Left to generation time,
// diet_type surfaces as a 500 nobody can trace back to the write that caused it, and
// region_culture and cuisine_code do not surface at all -- they produce a successful book
// ranked against a region the corpus does not carry, with nothing reported as omitted.
//
// Each row changes exactly one field of an otherwise valid body, so a 400 can only be about
// that field, and the message has to name it: an operator retyping a profile needs to know
// which of the four was wrong.
func TestPutProfileRejectsValuesTheCorpusDoesNotCarry(t *testing.T) {
	r, h := profileRouter(t)
	cleanupProfile(t, h)

	for _, c := range []struct {
		name  string
		field string
		body  string
	}{
		{
			name:  "diet type in the wrong case",
			field: "diet_type",
			// Lowercase is the real failure mode: engine.dietPermits is keyed on the
			// provider's capitalisation, so this used to write cleanly and blow up later.
			body: `{"child_id":"TEST-CHILD-001","date_of_birth":"2023-08-18","diet_type":"vegetarian"}`,
		},
		{
			name:  "region that does not exist",
			field: "region_culture",
			body:  `{"child_id":"TEST-CHILD-001","date_of_birth":"2023-08-18","region_culture":"Atlantis"}`,
		},
		{
			name:  "cuisine code that does not exist",
			field: "cuisine_code",
			body:  `{"child_id":"TEST-CHILD-001","date_of_birth":"2023-08-18","cuisine_code":"CL-XX-NOPE"}`,
		},
		{
			name:  "budget band that does not exist",
			field: "budget_band",
			body:  `{"child_id":"TEST-CHILD-001","date_of_birth":"2023-08-18","budget_band":"cheap"}`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest("PUT", "/api/profiles/TEST-CHILD-001",
				bytes.NewBufferString(c.body)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), c.field) {
				t.Fatalf("the 400 must name the field that was wrong, got %s", rec.Body.String())
			}
			// Nothing may have been written: a rejected profile that half-saved is worse
			// than one that failed outright.
			var exists bool
			if err := h.pool.QueryRow(context.Background(),
				`SELECT exists(SELECT 1 FROM child_profile WHERE child_id = 'TEST-CHILD-001')`).
				Scan(&exists); err != nil {
				t.Fatalf("existence check: %v", err)
			}
			if exists {
				t.Fatal("a rejected profile must not have been written")
			}
		})
	}

	// An empty value is legal -- an operator who has not asked for a region is not the same
	// as one who asked for a region that does not exist -- and a fully populated valid
	// profile still writes.
	for _, body := range []string{
		`{"child_id":"TEST-CHILD-001","date_of_birth":"2023-08-18"}`,
		putProfileBody,
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest("PUT", "/api/profiles/TEST-CHILD-001",
			bytes.NewBufferString(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("a valid profile must still write, got %d: %s", rec.Code, rec.Body.String())
		}
	}
}
