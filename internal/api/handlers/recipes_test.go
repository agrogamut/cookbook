package handlers

import (
	"bytes"
	"encoding/json"
	"github.com/madamgy/recipie/internal/aidraft"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestRecipeDetailReturnsMethodCardAndNutrition(t *testing.T) {
	h := New(testPool(t), aidraft.Disabled)
	r := chi.NewRouter()
	r.Get("/api/recipes/{recipeID}", h.RecipeDetail)

	req := httptest.NewRequest("GET", "/api/recipes/MG-R-00001", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	for _, id := range []string{"MG%2DR%2D00001", "%4DG-R-00001"} {
		t.Run(id, func(t *testing.T) {
			encoded := httptest.NewRecorder()
			r.ServeHTTP(encoded, httptest.NewRequest("GET", "/api/recipes/"+id, nil))
			if encoded.Code != 200 || !bytes.Equal(encoded.Body.Bytes(), rec.Body.Bytes()) {
				t.Fatalf("encoded legacy detail differs: status=%d body=%s", encoded.Code, encoded.Body.String())
			}
		})
	}
}

func TestRecipeDetailReturns404ForUnknownID(t *testing.T) {
	h := New(testPool(t), aidraft.Disabled)
	r := chi.NewRouter()
	r.Get("/api/recipes/{recipeID}", h.RecipeDetail)

	for _, tc := range []struct{ path, id string }{
		{"MG-R-99999", "MG-R-99999"},
		{"MG%252DR%252D00001", "MG%2DR%2D00001"},
		{"missing%25value", "missing%value"},
		{"missing+value", "missing+value"},
		{"missing%2Bvalue", "missing+value"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/recipes/"+tc.path, nil))
			var body struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if rec.Code != 404 || body.Error != "recipe not found: "+tc.id {
				t.Fatalf("missing identity changed: status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}
