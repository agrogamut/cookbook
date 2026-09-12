package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/madamgy/recipie/internal/aidraft"
	"net/http"
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
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	token := hex.EncodeToString(secret)
	hash := sha256.Sum256([]byte(token))
	var staffID string
	if err := pool.QueryRow(context.Background(), `INSERT INTO app_private.staff_account(id,name,email,role)
		VALUES(gen_random_uuid(),'Smoke test staff',$1,'doctor') RETURNING id`, "smoke-"+token[:16]+"@example.invalid").Scan(&staffID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM app_private.staff_account WHERE id=$1`, staffID) })
	if _, err := pool.Exec(context.Background(), `INSERT INTO app_private.staff_session(token_hash,staff_id,expires_at) VALUES($1,$2,now()+interval '1 hour')`, hex.EncodeToString(hash[:]), staffID); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(NewRouter(pool, aidraft.Disabled))
	t.Cleanup(srv.Close)

	body, _ := json.Marshal(map[string]any{"age_months": 24})
	req, err := http.NewRequest("POST", srv.URL+"/api/search", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Madamgy-Request", "1")
	req.AddCookie(&http.Cookie{Name: "madamgy_session", Value: token})
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /api/search: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("POST /api/search status = %d", resp.StatusCode)
	}

	req2, err := http.NewRequest("GET", srv.URL+"/api/reference/regions", nil)
	if err != nil {
		t.Fatal(err)
	}
	req2.AddCookie(&http.Cookie{Name: "madamgy_session", Value: token})
	resp2, err := srv.Client().Do(req2)
	if err != nil {
		t.Fatalf("GET /api/reference/regions: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("GET /api/reference/regions status = %d", resp2.StatusCode)
	}
}
