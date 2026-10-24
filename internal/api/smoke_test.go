package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/casesource"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRecipeDetailHTTPRouting(t *testing.T) {
	pool := nativeReleasePool(t)
	token := strings.Repeat("e", 64)
	hash := sha256.Sum256([]byte(token))
	var staffID string
	if err := pool.QueryRow(context.Background(), `INSERT INTO app_private.staff_account(id,name,email,role)
		VALUES(gen_random_uuid(),'Synthetic route staff','recipe-route@example.invalid','doctor') RETURNING id`).Scan(&staffID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO app_private.staff_session(token_hash,staff_id,expires_at) VALUES($1,$2,now()+interval '1 hour')`, hex.EncodeToString(hash[:]), staffID); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewRouter(pool, aidraft.Disabled))
	t.Cleanup(srv.Close)
	client := srv.Client()
	client.Timeout = 5 * time.Second
	get := func(t *testing.T, id string, authenticated bool, want int) []byte {
		t.Helper()
		req, err := http.NewRequest("GET", srv.URL+"/api/recipes/"+id, nil)
		if err != nil {
			t.Fatal(err)
		}
		if authenticated {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != want {
			t.Fatalf("HTTP detail status=%d want=%d body=%s", resp.StatusCode, want, body)
		}
		return body
	}
	for _, original := range []string{"MG-R-00001", "MG-WB-C01-001", "MG-WB-C26-001"} {
		t.Run(original, func(t *testing.T) {
			id := original
			if strings.HasPrefix(id, "MG-WB-") {
				id = casesource.CandidateID("3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2", id)
			}
			literal := get(t, id, true, 200)
			upper := strings.NewReplacer(":", "%3A", "-", "%2D").Replace(id)
			lower := strings.NewReplacer("%3A", "%3a", "%2D", "%2d").Replace(upper)
			for _, tc := range []struct{ name, path string }{{"upper", upper}, {"lower", lower}} {
				t.Run(tc.name, func(t *testing.T) {
					if !bytes.Equal(literal, get(t, tc.path, true, 200)) {
						t.Fatal("encoded HTTP detail differs from literal response")
					}
				})
			}
			get(t, upper, false, 401)
			get(t, strings.ReplaceAll(upper, "%", "%25"), true, 404)
		})
	}
	for _, bad := range []string{"%", "%2", "%GG"} {
		t.Run("malformed_"+bad, func(t *testing.T) {
			conn, err := net.DialTimeout("tcp", srv.Listener.Addr().String(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			if _, err := fmt.Fprintf(conn, "GET /api/recipes/case%s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", bad, srv.Listener.Addr()); err != nil {
				t.Fatal(err)
			}
			resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 400 {
				t.Fatalf("malformed path status=%d", resp.StatusCode)
			}
		})
	}
}

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
