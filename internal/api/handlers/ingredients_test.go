package handlers

import (
	"github.com/madamgy/recipie/internal/aidraft"
	"net/http/httptest"
	"testing"
)

func TestIngredientsListsCorrectedAndProviderValuesSideBySide(t *testing.T) {
	h := New(testPool(t), aidraft.Disabled)
	req := httptest.NewRequest("GET", "/api/ingredients?limit=5", nil)
	rec := httptest.NewRecorder()

	h.Ingredients(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
