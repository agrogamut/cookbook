package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestServerServesSearchAndReferenceEndToEnd(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	srv := httptest.NewServer(NewRouter(pool))
	t.Cleanup(srv.Close)

	body, _ := json.Marshal(map[string]any{"age_months": 24})
	resp, err := srv.Client().Post(srv.URL+"/api/search", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/search: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("POST /api/search status = %d", resp.StatusCode)
	}

	resp2, err := srv.Client().Get(srv.URL + "/api/reference/regions")
	if err != nil {
		t.Fatalf("GET /api/reference/regions: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("GET /api/reference/regions status = %d", resp2.StatusCode)
	}
}
