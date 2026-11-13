// Package config loads every environment-derived setting in one place and fails fast
// when something required is missing. No other package calls os.Getenv.
package config

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL string // Postgres connection string
	XlsxDir     string // directory holding the provider workbooks
	Port        int    // HTTP listen port (unused until phase 2)

	// GeminiAPIKey is optional, unlike everything above. An empty value does not fail
	// startup -- it means the AI-drafting feature (internal/aidraft) stays off: no
	// clinical modification notes, no invented-recipe fallback, chapters that fall
	// short just report the gap the way they always have.
	GeminiAPIKey              string
	SupabaseURL               string
	SupabaseSecretKey         string
	RazorpayKeyID             string
	RazorpayKeySecret         string
	RazorpayWebhookSecret     string
	AppOrigin                 string
	SecureCookies             bool
	LegacyGuardianUntil       time.Time
	WalletEncryptionKey       []byte
	PatientTaxPolicy          string
	WithdrawalDeductionPolicy string
}

// Load reads the environment. It returns an error naming every missing variable at
// once rather than one per run.
func Load() (Config, error) {
	var c Config
	var missing []string

	c.DatabaseURL = os.Getenv("DATABASE_URL")
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}

	c.XlsxDir = os.Getenv("XLSX_DIR")
	if c.XlsxDir == "" {
		c.XlsxDir = "data/provider"
	}

	c.Port = 8080
	if v := os.Getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return c, fmt.Errorf("config: PORT %q is not a number: %w", v, err)
		}
		c.Port = p
	}

	if v := strings.TrimSpace(os.Getenv("PORTAL_LEGACY_GUARDIAN_UNTIL")); v != "" {
		deadline, err := time.Parse(time.RFC3339, v)
		if err != nil || deadline.After(time.Now().Add(24*time.Hour)) {
			return c, fmt.Errorf("config: PORTAL_LEGACY_GUARDIAN_UNTIL must be an RFC3339 deadline no more than 24 hours ahead")
		}
		c.LegacyGuardianUntil = deadline
	}
	if v := os.Getenv("WALLET_ENCRYPTION_KEY"); v != "" {
		key, err := hex.DecodeString(v)
		if err != nil || len(key) != 32 {
			return c, fmt.Errorf("config: WALLET_ENCRYPTION_KEY must contain 64 hexadecimal characters")
		}
		c.WalletEncryptionKey = key
	}

	c.PatientTaxPolicy = strings.TrimSpace(os.Getenv("PORTAL_PATIENT_TAX_POLICY"))
	c.WithdrawalDeductionPolicy = strings.TrimSpace(os.Getenv("PORTAL_WITHDRAWAL_DEDUCTION_POLICY"))
	for _, value := range []*string{&c.PatientTaxPolicy, &c.WithdrawalDeductionPolicy} {
		if *value == "" {
			*value = "owner-18-v1"
		}
		if *value != "owner-18-v1" && *value != "inactive" {
			return c, fmt.Errorf("config: portal tax policy must be owner-18-v1 or inactive")
		}
	}
	c.GeminiAPIKey = os.Getenv("GEMINI_API_KEY")
	c.SupabaseURL = strings.TrimRight(os.Getenv("SUPABASE_URL"), "/")
	c.SupabaseSecretKey = os.Getenv("SUPABASE_SECRET_KEY")
	c.RazorpayKeyID = os.Getenv("RAZORPAY_KEY_ID")
	c.RazorpayKeySecret = os.Getenv("RAZORPAY_KEY_SECRET")
	c.RazorpayWebhookSecret = os.Getenv("RAZORPAY_WEBHOOK_SECRET")
	c.AppOrigin = strings.TrimRight(os.Getenv("APP_ORIGIN"), "/")
	if c.AppOrigin == "" {
		c.AppOrigin = "http://localhost:3000"
	}
	origin, err := url.Parse(c.AppOrigin)
	if err != nil || origin.Host == "" || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" || origin.User != nil {
		return c, fmt.Errorf("config: APP_ORIGIN must be an http(s) origin without a path")
	}
	c.SecureCookies = origin.Scheme == "https"
	if (c.SupabaseURL == "") != (c.SupabaseSecretKey == "") {
		return c, fmt.Errorf("config: set both SUPABASE_URL and SUPABASE_SECRET_KEY, or neither")
	}
	if c.SupabaseURL != "" {
		u, err := url.Parse(c.SupabaseURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return c, fmt.Errorf("config: SUPABASE_URL must be an https project origin")
		}
	}
	if (c.RazorpayKeyID == "") != (c.RazorpayKeySecret == "") {
		return c, fmt.Errorf("config: set both RAZORPAY_KEY_ID and RAZORPAY_KEY_SECRET, or neither")
	}

	if len(missing) > 0 {
		return c, fmt.Errorf("config: missing required environment variables: %v", missing)
	}
	return c, nil
}
