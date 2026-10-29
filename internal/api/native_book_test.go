package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/book"
	"github.com/madamgy/recipie/internal/casesource"
	"github.com/madamgy/recipie/internal/db"
	"github.com/madamgy/recipie/internal/importer"
)

type nativeReleaseTranslator struct{ aidraft.Drafter }

func (nativeReleaseTranslator) TranslateTexts(_ context.Context, req aidraft.TranslateRequest) (aidraft.TranslatedTexts, error) {
	out := append([]string(nil), req.Texts...)
	for i := range out {
		if len(req.Texts) == 8 && req.Texts[0] == "Supplied prescription annex. Footer page numbers restart within this annex." {
			out[i] = "Localized " + req.TargetLanguage + " annex"
		}
	}
	return aidraft.TranslatedTexts{Texts: out}, nil
}

func nativeReleasePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("CASE_SOURCE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CASE_SOURCE_TEST_DATABASE_URL requires disposable local PostgreSQL")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatal("native release tests require loopback database")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	name := fmt.Sprintf("native_release_%d_%d", os.Getpid(), time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	u.Path = "/" + name
	pool, err := db.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := importer.Run(ctx, pool, "../../data/provider"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("../../data/case-source/3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2.zip")
	if err != nil {
		t.Fatal(err)
	}
	r, err := casesource.Import(ctx, pool, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := casesource.Activate(ctx, pool, r.ArchiveSHA256); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestNativeReleaseJourney(t *testing.T) {
	pool := nativeReleasePool(t)
	ctx := context.Background()
	router := NewRouter(pool, nativeReleaseTranslator{aidraft.Disabled})
	adminID := "10000000-0000-4000-8000-000000000001"
	guardianID := "10000000-0000-4000-8000-000000000002"
	otherID := "10000000-0000-4000-8000-000000000003"
	adminToken, familyToken, otherToken := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	hash := func(v string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(v))) }
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO app_private.staff_account(id,name,email,role) VALUES($1,'Synthetic admin','native-admin@example.invalid','admin')`, []any{adminID}},
		{`INSERT INTO app_private.staff_session(token_hash,staff_id,expires_at) VALUES($1,$2,now()+interval '1 hour')`, []any{hash(adminToken), adminID}},
		{`INSERT INTO app_private.guardian_account(id,name,email) VALUES($1,'Synthetic family','native-family@example.invalid'),($2,'Other family','native-other@example.invalid')`, []any{guardianID, otherID}},
		{`INSERT INTO app_private.guardian_session(token_hash,guardian_id,expires_at) VALUES($1,$2,now()+interval '1 hour'),($3,$4,now()+interval '1 hour')`, []any{hash(familyToken), guardianID, hash(otherToken), otherID}},
	} {
		if _, err := pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	request := func(method, path string, body any, token string, want int) *httptest.ResponseRecorder {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Madamgy-Request", "1")
		if token == adminToken {
			r.Header.Set("Authorization", "Bearer "+token)
		} else if token != "" {
			r.AddCookie(&http.Cookie{Name: "madamgy_guardian", Value: token})
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: got %d want %d: %.500s", method, path, w.Code, want, w.Body.String())
		}
		return w
	}
	dob := time.Now().UTC().AddDate(0, -8, 0).Format("2006-01-02")
	w := request("POST", "/api/family/registrations", map[string]string{"child_name": "Synthetic native child", "date_of_birth": dob, "phone": "+919000000013"}, familyToken, 201)
	var registration map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &registration); err != nil {
		t.Fatal(err)
	}
	reg := registration["id"]
	for _, fixture := range []struct {
		language string
		uploads  bool
	}{{"en", false}, {"en", true}, {"bn", true}, {"hi", true}} {
		lang := fixture.language
		t.Run(fmt.Sprintf("%s/uploads=%t", lang, fixture.uploads), func(t *testing.T) {
			body := map[string]string{"child_id": reg, "display_name": "Synthetic native child", "date_of_birth": dob, "language_id": lang}
			if fixture.uploads {
				body["photo_data_uri"] = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAFklEQVR4nGOUC+hhYGBgYmBgYGBgAAALdAD+3y8XygAAAABJRU5ErkJggg=="
				body["parents_photo_data_uri"] = body["photo_data_uri"]
				body["prescription_photo_data_uri"] = body["photo_data_uri"]
				body["prescription_photo_caption"] = "Synthetic prescription upload"
			}
			w := request("POST", "/api/registrations/"+reg+"/book-releases", body, adminToken, 201)
			var generated struct {
				Book1LayoutVersion string `json:"book1_layout_version"`
				Book2LayoutVersion string `json:"book2_layout_version"`
				Book1              struct {
					PDF []byte `json:"pdf"`
				} `json:"book1_print"`
				Book2 struct {
					PDF []byte `json:"pdf"`
				} `json:"book2_print"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &generated); err != nil {
				t.Fatal(err)
			}
			if generated.Book1LayoutVersion != book.ReaderLayoutVersion || generated.Book2LayoutVersion != book.ReaderLayoutVersion {
				t.Fatal("release response lost reader layout provenance")
			}
			for name, pdf := range map[string][]byte{"book1": generated.Book1.PDF, "book2": generated.Book2.PDF} {
				path := filepath.Join(t.TempDir(), name+".pdf")
				if err := os.WriteFile(path, pdf, 0600); err != nil {
					t.Fatal(err)
				}
				output, err := exec.Command("qpdf", "--check", path).CombinedOutput()
				if err != nil || strings.Contains(strings.ToLower(string(output)), "warning") {
					t.Fatalf("final %s/%s PDF structure: %v: %s", lang, name, err, output)
				}
			}
			if fixture.uploads {
				path := filepath.Join(t.TempDir(), "localized-annex.pdf")
				if err := os.WriteFile(path, generated.Book1.PDF, 0600); err != nil {
					t.Fatal(err)
				}
				text, err := exec.Command("pdftotext", path, "-").CombinedOutput()
				if err != nil {
					t.Fatalf("extract annex: %v: %s", err, text)
				}
				label := "Supplied prescription annex."
				if lang != "en" {
					label = "Localized " + map[string]string{"bn": "Bengali", "hi": "Hindi"}[lang] + " annex"
				}
				if !strings.Contains(string(text), label) {
					t.Fatal("configured translator did not reach prescription annex")
				}
			}
			var releases []struct{ ID, Book, Status string }
			w = request("GET", "/api/book-releases?registration_id="+reg, nil, adminToken, 200)
			if err := json.Unmarshal(w.Body.Bytes(), &releases); err != nil {
				t.Fatal(err)
			}
			if len(releases) < 2 {
				t.Fatal("release pair absent")
			}
			for _, r := range releases[:2] {
				pdf := generated.Book1.PDF
				if r.Book == "book2" {
					pdf = generated.Book2.PDF
				}
				if !bytes.HasPrefix(pdf, []byte("%PDF-")) || r.Status != "pending_admin" {
					t.Fatalf("invalid pending native PDF: %s", r.Status)
				}
				familyPath := "/api/family/book-releases/" + r.ID + "/download"
				request("GET", familyPath, nil, familyToken, 404)
				request("GET", familyPath, nil, "", 401)
				staffPath := "/api/book-releases/" + r.ID + "/pdf"
				if !bytes.Equal(request("GET", staffPath, nil, adminToken, 200).Body.Bytes(), pdf) {
					t.Fatal("stored pending bytes differ")
				}
				action := "approve"
				if lang == "hi" && r.Book == "book2" {
					action = "reject"
				}
				request("PATCH", "/api/admin/book-releases/"+r.ID, map[string]string{"action": action}, adminToken, 200)
				if action == "approve" {
					if !bytes.Equal(request("GET", familyPath, nil, familyToken, 200).Body.Bytes(), pdf) {
						t.Fatal("approved bytes differ")
					}
				} else {
					request("GET", familyPath, nil, familyToken, 404)
				}
				request("GET", familyPath, nil, otherToken, 404)
				if !bytes.Equal(request("GET", staffPath, nil, adminToken, 200).Body.Bytes(), pdf) {
					t.Fatal("decision changed stored bytes")
				}
				t.Logf("NATIVE_RELEASE_OK language=%s book=%s bytes=%d sha256=%x decision=%s", lang, r.Book, len(pdf), sha256.Sum256(pdf), action)
			}
		})
	}
}
