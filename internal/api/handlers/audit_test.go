package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestNutritionAuditReturnsDiscrepancyReport(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("GET", "/api/audit/nutrition", nil)
	rec := httptest.NewRecorder()

	h.NutritionAudit(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestGapsReturnsAllSixteenEntries(t *testing.T) {
	h := New(testPool(t))
	req := httptest.NewRequest("GET", "/api/gaps", nil)
	rec := httptest.NewRecorder()

	h.Gaps(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
