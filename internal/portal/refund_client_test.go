package portal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestRefundClientPaginationAndProcessedIdentity(t *testing.T) {
	payment := Payment{ID: "pay_Test123", Amount: 10000, Currency: "INR"}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/payments/pay_Test123/refunds" || r.URL.Query().Get("count") != "100" {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
		skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))
		items := []PaymentRefund{}
		for i := skip; i < min(skip+100, 103); i++ {
			status := "processed"
			if i == 3 {
				status = "pending"
			}
			if i == 102 {
				status = "failed"
			}
			items = append(items, PaymentRefund{ID: fmt.Sprintf("rfnd_Test%d", i), PaymentID: payment.ID, Amount: 1, Currency: "INR", Status: status, CreatedAt: 1700000000 + int64(i)})
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	defer server.Close()
	gateway := NewRazorpay("local", "local", "local")
	gateway.URL = server.URL
	gateway.HTTP = server.Client()
	refunds, err := gateway.FetchRefunds(context.Background(), payment)
	if err != nil || len(refunds) != 101 || calls != 2 {
		t.Fatalf("refunds=%d calls=%d err=%v", len(refunds), calls, err)
	}
	for _, v := range refunds {
		if v.Status != "processed" {
			t.Fatal("pending refund returned")
		}
	}
}
func TestRefundClientRejectsInconsistentPages(t *testing.T) {
	for _, kind := range []string{"duplicate", "currency", "payment", "amount", "malformed", "unavailable"} {
		t.Run(kind, func(t *testing.T) {
			p := Payment{ID: "pay_Test123", Amount: 100, Currency: "INR"}
			v := PaymentRefund{ID: "rfnd_Test123", PaymentID: p.ID, Amount: 1, Currency: "INR", Status: "processed", CreatedAt: 1700000000}
			switch kind {
			case "currency":
				v.Currency = "USD"
			case "payment":
				v.PaymentID = "pay_Other"
			case "amount":
				v.Amount = 101
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if kind == "unavailable" {
					http.Error(w, "private provider response", 502)
					return
				}
				if kind == "malformed" {
					fmt.Fprint(w, "{")
					return
				}
				items := []PaymentRefund{v}
				if kind == "duplicate" {
					items = append(items, v)
				}
				json.NewEncoder(w).Encode(map[string]any{"items": items})
			}))
			defer server.Close()
			g := NewRazorpay("local", "local", "local")
			g.URL = server.URL
			g.HTTP = server.Client()
			if _, err := g.FetchRefunds(context.Background(), p); err == nil || strings.Contains(err.Error(), "private provider response") {
				t.Fatalf("unsafe result %v", err)
			}
		})
	}
}
