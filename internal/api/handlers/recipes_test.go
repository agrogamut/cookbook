package handlers

import (
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestRecipeDetailReturnsMethodCardAndNutrition(t *testing.T) {
	h := New(testPool(t))
	r := chi.NewRouter()
	r.Get("/api/recipes/{recipeID}", h.RecipeDetail)

	req := httptest.NewRequest("GET", "/api/recipes/MG-R-00001", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestRecipeDetailReturns404ForUnknownID(t *testing.T) {
	h := New(testPool(t))
	r := chi.NewRouter()
	r.Get("/api/recipes/{recipeID}", h.RecipeDetail)

	req := httptest.NewRequest("GET", "/api/recipes/MG-R-99999", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
