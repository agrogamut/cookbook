package config

import (
	"strings"
	"testing"
	"time"
)

func TestWalletPolicyConfiguration(t *testing.T) {
	for _, name := range []string{"PORT", "SUPABASE_URL", "SUPABASE_SECRET_KEY", "RAZORPAY_KEY_ID", "RAZORPAY_KEY_SECRET", "APP_ORIGIN", "WALLET_ENCRYPTION_KEY", "PORTAL_PATIENT_TAX_POLICY", "PORTAL_WITHDRAWAL_DEDUCTION_POLICY", "PORTAL_LEGACY_GUARDIAN_UNTIL"} {
		t.Setenv(name, "")
	}
	t.Setenv("DATABASE_URL", "postgres://local.invalid/test")
	c, err := Load()
	if err != nil || len(c.WalletEncryptionKey) != 0 || c.PatientTaxPolicy != "owner-18-v1" || c.WithdrawalDeductionPolicy != "owner-18-v1" {
		t.Fatalf("owner policy defaults missing: %v", err)
	}
	for _, key := range []string{"short", strings.Repeat("z", 64), strings.Repeat("a", 62)} {
		t.Setenv("WALLET_ENCRYPTION_KEY", key)
		if _, err = Load(); err == nil || strings.Contains(err.Error(), key) {
			t.Fatal("invalid encryption key must be rejected without echoing it")
		}
	}
	t.Setenv("WALLET_ENCRYPTION_KEY", strings.Repeat("a", 64))
	c, err = Load()
	if err != nil || len(c.WalletEncryptionKey) != 32 {
		t.Fatalf("valid encryption key rejected: %v", err)
	}
	for _, name := range []string{"PORTAL_PATIENT_TAX_POLICY", "PORTAL_WITHDRAWAL_DEDUCTION_POLICY"} {
		for _, value := range []string{strings.Repeat("x", 201), "line1\nline2", "adviser-approved"} {
			t.Setenv(name, value)
			if _, err = Load(); err == nil {
				t.Fatal("invalid approval reference accepted")
			}
		}
		t.Setenv(name, "inactive")
	}
	c, err = Load()
	if err != nil || c.PatientTaxPolicy != "inactive" || c.WithdrawalDeductionPolicy != "inactive" {
		t.Fatalf("explicit policy not loaded: %v", err)
	}
}

func TestGuardianTransitionDeadlineConfiguration(t *testing.T) {
	for _, name := range []string{"PORT", "SUPABASE_URL", "SUPABASE_SECRET_KEY", "RAZORPAY_KEY_ID", "RAZORPAY_KEY_SECRET", "APP_ORIGIN", "WALLET_ENCRYPTION_KEY", "PORTAL_PATIENT_TAX_POLICY", "PORTAL_WITHDRAWAL_DEDUCTION_POLICY", "PORTAL_LEGACY_GUARDIAN_UNTIL"} {
		t.Setenv(name, "")
	}
	t.Setenv("DATABASE_URL", "postgres://local.invalid/test")
	c, err := Load()
	if err != nil || !c.LegacyGuardianUntil.IsZero() {
		t.Fatalf("strict default missing: %v", err)
	}
	for _, value := range []string{"true", "bad-date", time.Now().Add(25 * time.Hour).UTC().Format(time.RFC3339)} {
		t.Setenv("PORTAL_LEGACY_GUARDIAN_UNTIL", value)
		if _, err = Load(); err == nil {
			t.Fatal("unbounded or invalid transition accepted")
		}
	}
	for _, deadline := range []time.Time{time.Now().Add(time.Hour).UTC().Truncate(time.Second), time.Now().Add(-time.Hour).UTC().Truncate(time.Second)} {
		t.Setenv("PORTAL_LEGACY_GUARDIAN_UNTIL", deadline.Format(time.RFC3339))
		c, err = Load()
		if err != nil || !c.LegacyGuardianUntil.Equal(deadline) {
			t.Fatalf("absolute deadline not preserved: %v", err)
		}
	}
}
