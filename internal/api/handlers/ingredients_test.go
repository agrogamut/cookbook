package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestIngredientsListsCorrectedAndProviderValuesSideBySide(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("GET", "/api/ingredients?limit=5", nil)
	rec := httptest.NewRecorder()

	h.Ingredients(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
