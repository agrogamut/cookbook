package portal

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("service is not configured")
var ErrCredentials = errors.New("email or password is incorrect")

type Identity struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// IdentityProvider validates credentials without exposing its privileged key to a browser.
type IdentityProvider interface {
	Enabled() bool
	SignIn(context.Context, string, string) (Identity, error)
	Create(context.Context, string, string) (Identity, error)
	Password(context.Context, string, string) error
	Delete(context.Context, string) error
}

type Supabase struct {
	URL, Key string
	HTTP     *http.Client
}

func NewSupabase(baseURL, key string) *Supabase {
	return &Supabase{URL: strings.TrimRight(baseURL, "/"), Key: key, HTTP: &http.Client{Timeout: 15 * time.Second}}
}
func (s *Supabase) Enabled() bool { return s.URL != "" && s.Key != "" }

func (s *Supabase) call(ctx context.Context, method, path string, body, out any) error {
	if !s.Enabled() {
		return ErrUnavailable
	}
	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode identity request: %w", err)
		}
		payload = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.URL+"/auth/v1"+path, payload)
	if err != nil {
		return fmt.Errorf("build identity request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", s.Key)
	// Legacy service keys are JWTs. New secret keys belong only in apikey.
	if !strings.HasPrefix(s.Key, "sb_secret_") {
		req.Header.Set("Authorization", "Bearer "+s.Key)
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("identity service request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if strings.HasPrefix(path, "/token") && (resp.StatusCode == 400 || resp.StatusCode == 401 || resp.StatusCode == 422) {
			return ErrCredentials
		}
		return fmt.Errorf("identity service returned HTTP %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("decode identity response: %w", err)
	}
	return nil
}

func (s *Supabase) SignIn(ctx context.Context, email, password string) (Identity, error) {
	var response struct {
		User Identity `json:"user"`
	}
	err := s.call(ctx, "POST", "/token?grant_type=password", map[string]string{"email": email, "password": password}, &response)
	if err == nil && !validUUID(response.User.ID) {
		err = errors.New("identity service returned no valid user")
	}
	return response.User, err
}
func (s *Supabase) Create(ctx context.Context, email, password string) (Identity, error) {
	var identity Identity
	err := s.call(ctx, "POST", "/admin/users", map[string]any{"email": email, "password": password, "email_confirm": true}, &identity)
	if err == nil && !validUUID(identity.ID) {
		err = errors.New("identity service returned no valid user")
	}
	return identity, err
}
func (s *Supabase) Password(ctx context.Context, id, password string) error {
	return s.call(ctx, "PUT", "/admin/users/"+url.PathEscape(id), map[string]string{"password": password}, nil)
}
func (s *Supabase) Delete(ctx context.Context, id string) error {
	return s.call(ctx, "DELETE", "/admin/users/"+url.PathEscape(id), nil, nil)
}

type Payment struct {
	ID             string `json:"id"`
	OrderID        string `json:"order_id"`
	Amount         int    `json:"amount"`
	Currency       string `json:"currency"`
	Status         string `json:"status"`
	Captured       bool   `json:"captured"`
	AmountRefunded int    `json:"amount_refunded"`
}
type GatewayOrder struct {
	ID       string `json:"id"`
	Amount   int    `json:"amount"`
	Currency string `json:"currency"`
}
type PaymentGateway interface {
	Enabled() bool
	PublicKey() string
	CreateOrder(context.Context, string, int, string) (GatewayOrder, error)
	FetchPayment(context.Context, string) (Payment, error)
	VerifyCheckout(string, string, string) bool
	VerifyWebhook([]byte, string) bool
}
type Razorpay struct {
	KeyID, Secret, WebhookSecret, URL string
	HTTP                              *http.Client
}

func NewRazorpay(keyID, secret, webhookSecret string) *Razorpay {
	return &Razorpay{KeyID: keyID, Secret: secret, WebhookSecret: webhookSecret, URL: "https://api.razorpay.com/v1", HTTP: &http.Client{Timeout: 15 * time.Second}}
}
func (g *Razorpay) Enabled() bool     { return g.KeyID != "" && g.Secret != "" && g.WebhookSecret != "" }
func (g *Razorpay) PublicKey() string { return g.KeyID }
func (g *Razorpay) call(ctx context.Context, method, path string, body, out any) error {
	if !g.Enabled() {
		return ErrUnavailable
	}
	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode payment request: %w", err)
		}
		payload = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.URL+path, payload)
	if err != nil {
		return fmt.Errorf("build payment request: %w", err)
	}
	req.SetBasicAuth(g.KeyID, g.Secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("payment service request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("payment service returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("decode payment response: %w", err)
	}
	return nil
}
func (g *Razorpay) CreateOrder(ctx context.Context, receipt string, amount int, currency string) (GatewayOrder, error) {
	var order GatewayOrder
	err := g.call(ctx, "POST", "/orders", map[string]any{"receipt": receipt, "amount": amount, "currency": currency}, &order)
	if err == nil && (!validProviderID(order.ID, "order_") || order.Amount != amount || order.Currency != currency) {
		err = errors.New("payment service returned a mismatched order")
	}
	return order, err
}
func (g *Razorpay) FetchPayment(ctx context.Context, id string) (Payment, error) {
	var payment Payment
	if !validProviderID(id, "pay_") {
		return payment, errors.New("invalid payment identifier")
	}
	err := g.call(ctx, "GET", "/payments/"+url.PathEscape(id), nil, &payment)
	if err == nil && payment.ID != id {
		err = errors.New("payment service returned a different payment")
	}
	return payment, err
}
func validProviderID(id, prefix string) bool {
	if !strings.HasPrefix(id, prefix) || len(id) <= len(prefix) || len(id) > 100 {
		return false
	}
	for _, c := range id[len(prefix):] {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
func signatureMatches(secret string, payload []byte, signature string) bool {
	if secret == "" {
		return false
	}
	got, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hmac.Equal(got, mac.Sum(nil))
}
func (g *Razorpay) VerifyCheckout(orderID, paymentID, signature string) bool {
	return signatureMatches(g.Secret, []byte(orderID+"|"+paymentID), signature)
}
func (g *Razorpay) VerifyWebhook(body []byte, signature string) bool {
	return signatureMatches(g.WebhookSecret, body, signature)
}
