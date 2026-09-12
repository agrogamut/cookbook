package portal

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIntakeValidation(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	base := Intake{GuardianName: "Test Guardian", ChildName: "Test Child", DateOfBirth: "2024-02-29", Phone: "9876543210", Token: token()}
	for _, test := range []struct {
		name   string
		change func(*Intake)
		valid  bool
	}{
		{"optional email", func(*Intake) {}, true},
		{"valid country code", func(v *Intake) { v.Phone = "+880 1712 345678" }, true},
		{"invalid calendar date", func(v *Intake) { v.DateOfBirth = "2025-02-29" }, false},
		{"future birth", func(v *Intake) { v.DateOfBirth = "2026-09-12" }, false},
		{"birth today", func(v *Intake) { v.DateOfBirth = "2026-09-11" }, true},
		{"blank child", func(v *Intake) { v.ChildName = "  " }, false},
		{"control character", func(v *Intake) { v.GuardianName = "Test\nGuardian" }, false},
		{"invalid email", func(v *Intake) { v.Email = "someone@" }, false},
		{"display name email", func(v *Intake) { v.Email = "Person <test@example.com>" }, false},
		{"invalid phone", func(v *Intake) { v.Phone = "+91letters" }, false},
		{"phone missing", func(v *Intake) { v.Phone = "" }, false},
		{"weak token", func(v *Intake) { v.Token = "short" }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := base
			test.change(&v)
			message := v.validate(now)
			if (message == "") != test.valid {
				t.Fatalf("validation = %q, valid expected %v", message, test.valid)
			}
		})
	}
	v := base
	v.validate(now)
	if v.Phone != "+919876543210" {
		t.Fatalf("phone normalization = %q", v.Phone)
	}
	// IST is already tomorrow even when UTC still has the previous date.
	v.DateOfBirth = "2026-09-12"
	if message := v.validate(time.Date(2026, 9, 11, 20, 0, 0, 0, time.UTC)); message != "" {
		t.Fatal(message)
	}
}

func sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

func TestPaymentSignaturesAndState(t *testing.T) {
	g := NewRazorpay("test_key", "test_checkout_secret", "test_webhook_secret")
	if !g.VerifyCheckout("order_test", "pay_test", sign(g.Secret, []byte("order_test|pay_test"))) {
		t.Fatal("valid checkout rejected")
	}
	if g.VerifyCheckout("order_other", "pay_test", sign(g.Secret, []byte("order_test|pay_test"))) {
		t.Fatal("different order accepted")
	}
	body := []byte(`{"event":"payment.captured"}`)
	if !g.VerifyWebhook(body, sign(g.WebhookSecret, body)) {
		t.Fatal("valid webhook rejected")
	}
	if g.VerifyWebhook(append(body, ' '), sign(g.WebhookSecret, body)) {
		t.Fatal("mutated raw body accepted")
	}
	if g.VerifyWebhook(body, sign(g.Secret, body)) {
		t.Fatal("checkout secret accepted for webhook")
	}
	if signatureMatches("", body, sign("", body)) {
		t.Fatal("empty secret accepted")
	}
	for _, test := range []struct {
		name, status string
		captured     bool
		refund       int
		want         string
	}{
		{"authorized is not paid", "authorized", false, 0, "authorized"},
		{"captured", "captured", true, 0, "paid"},
		{"missing captured flag", "captured", false, 0, ""},
		{"partial refund", "captured", true, 500, "partially_refunded"},
		{"full refund", "refunded", true, 1000, "refunded"},
		{"excess refund", "captured", true, 1001, ""},
		{"negative refund", "captured", true, -1, ""},
		{"failed", "failed", false, 0, "failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := paymentState(Payment{Amount: 1000, Status: test.status, Captured: test.captured, AmountRefunded: test.refund})
			if got != test.want || (err != nil) != (test.want == "") {
				t.Fatalf("state=(%q,%v), want %q", got, err, test.want)
			}
		})
	}
}

func TestSupabaseHTTPContract(t *testing.T) {
	const id = "f1d040f1-6c34-4d84-b80b-00de3cd760ca"
	for _, key := range []string{"sb_secret_test", "legacy.test.jwt"} {
		t.Run(key, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("apikey") != key {
					t.Error("missing API key")
				}
				if key == "sb_secret_test" && r.Header.Get("Authorization") != "" {
					t.Error("secret key used as bearer JWT")
				}
				if key != "sb_secret_test" && r.Header.Get("Authorization") != "Bearer "+key {
					t.Error("legacy bearer key missing")
				}
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				switch r.URL.Path {
				case "/auth/v1/token":
					if r.URL.Query().Get("grant_type") != "password" {
						t.Error("wrong grant")
					}
					if body["password"] == "wrong" {
						w.WriteHeader(400)
						return
					}
					fmt.Fprintf(w, `{"user":{"id":%q,"email":"staff@example.com"},"access_token":"must-not-reach-browser"}`, id)
				case "/auth/v1/admin/users":
					if r.Method != "POST" || body["email_confirm"] != true {
						t.Error("invalid creation request")
					}
					fmt.Fprintf(w, `{"id":%q,"email":"staff@example.com"}`, id)
				case "/auth/v1/admin/users/" + id:
					if r.Method != "PUT" && r.Method != "DELETE" {
						t.Error("invalid admin method")
					}
					fmt.Fprint(w, `{}`)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			s := NewSupabase(server.URL, key)
			if identity, err := s.SignIn(context.Background(), "staff@example.com", "test-password"); err != nil || identity.ID != id {
				t.Fatalf("sign in: %v %v", identity, err)
			}
			if _, err := s.SignIn(context.Background(), "staff@example.com", "wrong"); err != ErrCredentials {
				t.Fatalf("bad credentials: %v", err)
			}
			if _, err := s.Create(context.Background(), "staff@example.com", "test-password"); err != nil {
				t.Fatal(err)
			}
			if err := s.Password(context.Background(), id, "new-test-password"); err != nil {
				t.Fatal(err)
			}
			if err := s.Delete(context.Background(), id); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRazorpayHTTPContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "test_key" || password != "test_secret" {
			t.Error("missing server authentication")
		}
		switch r.URL.Path {
		case "/orders":
			var body struct {
				Amount   int    `json:"amount"`
				Currency string `json:"currency"`
				Receipt  string `json:"receipt"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Amount != 12345 || body.Currency != "INR" || body.Receipt != "registration" {
				t.Error("wrong order body")
			}
			fmt.Fprint(w, `{"id":"order_test123","amount":12345,"currency":"INR"}`)
		case "/payments/pay_test123":
			if r.Method != "GET" || r.ContentLength != 0 {
				t.Error("payment lookup must be a GET without a body")
			}
			fmt.Fprint(w, `{"id":"pay_test123","order_id":"order_test123","amount":12345,"currency":"INR","status":"captured","captured":true}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	g := NewRazorpay("test_key", "test_secret", "test_webhook")
	g.URL = server.URL
	if order, err := g.CreateOrder(context.Background(), "registration", 12345, "INR"); err != nil || order.Amount != 12345 {
		t.Fatalf("order: %v %v", order, err)
	}
	if payment, err := g.FetchPayment(context.Background(), "pay_test123"); err != nil || !payment.Captured {
		t.Fatalf("payment: %v %v", payment, err)
	}
	if _, err := g.FetchPayment(context.Background(), "pay_../../other"); err == nil {
		t.Fatal("path traversal accepted")
	}
}
