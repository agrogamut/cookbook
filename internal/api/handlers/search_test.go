package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/models"
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

func TestSearchReturnsRankedRecipesAndSteps(t *testing.T) {
	h := New(testPool(t))
	body, _ := json.Marshal(models.ChildProfile{AgeMonths: 24})
	req := httptest.NewRequest("POST", "/api/search", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Search(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var result models.EngineResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Recipes) == 0 {
		t.Fatal("expected non-empty result for a no-preference 24mo profile")
	}
	if len(result.Steps) == 0 {
		t.Fatal("expected step accounting in the response")
	}
}

func TestSearchRejectsMissingAge(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("POST", "/api/search", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()

	h.Search(rec, req)

	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400 for a profile with no age", rec.Code)
	}
}

// TestSearchRejectsUnrecognizedAllergenWith400 pins the fix for the final whole-branch
// review's Important #6: an unrecognized allergen is an operator input mistake, not a
// server failure. Before ErrInvalidProfile existed, engine.Run's validation error was
// mapped to a bare 500 indistinguishable from a real database or query failure.
func TestSearchRejectsUnrecognizedAllergenWith400(t *testing.T) {
	h := New(testPool(t))
	body, _ := json.Marshal(models.ChildProfile{AgeMonths: 24, Allergens: []string{"not-a-real-allergen"}})
	req := httptest.NewRequest("POST", "/api/search", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Search(rec, req)

	if rec.Code != 400 {
		t.Fatalf("status = %d, body = %s, want 400 for an unrecognized allergen", rec.Code, rec.Body.String())
	}
}

// TestSearchBlockedResponseHasEmptyArrayNotNullRecipes pins a fix found during Task 3's
// frontend TypeScript review: the wire JSON must carry "recipes":[] on a blocked/empty
// result, never "recipes":null -- the frontend's EngineResult type promises a plain
// array, and a null would crash the results table instead of rendering the empty state.
func TestSearchBlockedResponseHasEmptyArrayNotNullRecipes(t *testing.T) {
	h := New(testPool(t))
	body, _ := json.Marshal(models.ChildProfile{AgeMonths: 36, ClinicalFlags: map[string]string{"CKD": "Yes"}})
	req := httptest.NewRequest("POST", "/api/search", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.Search(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"recipes":[]`)) {
		t.Fatalf("expected literal \"recipes\":[] in the response body, got %s", rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(`"recipes":null`)) {
		t.Fatalf("response body must never carry \"recipes\":null: %s", rec.Body.String())
	}
}
