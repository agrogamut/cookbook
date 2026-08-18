package handlers

import (
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
		RunID     int64      `json:"run_id"`
		StartedAt time.Time  `json:"started_at"`
		FinishedAt *time.Time `json:"finished_at"`
		SourceDir string     `json:"source_dir"`
		OK        bool       `json:"ok"`
		Tables    []struct {
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
