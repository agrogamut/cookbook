package portal

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// This fixture is compiled only by go test and binds only to loopback.
func TestPortalBrowserServer(t *testing.T) {
	if os.Getenv("PORTAL_BROWSER_TEST") != "1" {
		t.Skip("browser fixture disabled")
	}
	f := newPaidFixture(t)

	id, secret := f.register(t, "Browser Child")
	_, _ = f.register(t, "Private Sibling")
	ctx := context.Background()
	var approvedID, pendingID string
	for _, book := range []string{"book1", "book2"} {
		var releaseID string
		if err := f.pool.QueryRow(ctx, `INSERT INTO app_private.book_release(registration_id,child_id,book,pdf,generated_by) VALUES ($1,$1,$2,$3,$4) RETURNING id`, id, book, []byte("%PDF-1.4\n% Browser test fixture\n%%EOF"), f.admin.ID).Scan(&releaseID); err != nil {
			t.Fatal(err)
		}
		if book == "book1" {
			approvedID = releaseID
			expectCode(t, portalRequest(f.router, "PATCH", "/api/admin/book-releases/"+releaseID, map[string]string{"action": "approve"}, f.adminCookie, ""), 200)
		} else {
			pendingID = releaseID
		}
	}
	f.server.options.Origin = "http://127.0.0.1:3307"
	router := chi.NewRouter()
	router.Post("/__test/reset", func(w http.ResponseWriter, r *http.Request) {
		tx, err := f.pool.Begin(r.Context())
		if err != nil {
			serverError(w, err)
			return
		}
		defer tx.Rollback(r.Context())
		for _, query := range []string{
			`DELETE FROM app_private.payment_event`,
			`DELETE FROM app_private.consultation_order`,
			`DELETE FROM app_private.appointment`,
			`DELETE FROM app_private.consultation_registration WHERE child_name='New Browser Child'`,
			`DELETE FROM public.child_profile WHERE display_name='New Browser Child'`,
		} {
			if _, err = tx.Exec(r.Context(), query); err != nil {
				serverError(w, err)
				return
			}
		}
		if err = tx.Commit(r.Context()); err != nil {
			serverError(w, err)
			return
		}
		respond(w, 200, map[string]bool{"ok": true})
	})
	router.Get("/__test/fixture", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"registration_id": id, "token": secret, "admin_email": f.admin.Email, "password": "test-password-only-123", "doctor_id": f.doctors[0].ID, "date": f.start.Format("2006-01-02"), "approved_id": approvedID, "pending_id": pendingID})
	})
	router.Post("/__test/payment", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			OrderID string `json:"order_id"`
			Status  string `json:"status"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			fail(w, 400, "Invalid fixture request")
			return
		}
		var amount int
		if err := f.pool.QueryRow(r.Context(), `SELECT amount_paise FROM app_private.consultation_order WHERE id=$1`, body.OrderID).Scan(&amount); err != nil {
			serverError(w, err)
			return
		}
		p := Payment{ID: "pay_" + token()[:16], OrderID: body.OrderID, Amount: amount, Currency: "INR", Status: body.Status, Captured: body.Status == "captured"}
		f.gateway.set(p)
		respond(w, 200, map[string]string{"razorpay_order_id": p.OrderID, "razorpay_payment_id": p.ID, "razorpay_signature": sign(f.gateway.Secret, []byte(p.OrderID+"|"+p.ID))})
	})
	router.Mount("/", f.router)
	server := &http.Server{Addr: "127.0.0.1:8807", Handler: router, ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	t.Log("browser fixture ready at 127.0.0.1:8807")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
