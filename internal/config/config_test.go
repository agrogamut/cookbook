package config

import (
	"strings"
	"testing"
)

func TestWalletPolicyConfiguration(t *testing.T) {
	for _, name := range []string{"PORT", "SUPABASE_URL", "SUPABASE_SECRET_KEY", "RAZORPAY_KEY_ID", "RAZORPAY_KEY_SECRET", "APP_ORIGIN", "WALLET_ENCRYPTION_KEY", "PORTAL_PATIENT_TAX_APPROVAL", "PORTAL_WITHDRAWAL_DEDUCTION_APPROVAL"} {
		t.Setenv(name, "")
	}
	t.Setenv("DATABASE_URL", "postgres://local.invalid/test")
	c, err := Load()
	if err != nil || len(c.WalletEncryptionKey) != 0 || c.PatientTaxApproval != "" || c.WithdrawalDeductionApproval != "" {
		t.Fatalf("features were not inactive by default: %v", err)
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
	for _, name := range []string{"PORTAL_PATIENT_TAX_APPROVAL", "PORTAL_WITHDRAWAL_DEDUCTION_APPROVAL"} {
		for _, value := range []string{strings.Repeat("x", 201), "line1\nline2"} {
			t.Setenv(name, value)
			if _, err = Load(); err == nil {
				t.Fatal("invalid approval reference accepted")
			}
		}
		t.Setenv(name, "synthetic-reviewed-policy")
	}
	c, err = Load()
	if err != nil || c.PatientTaxApproval != "synthetic-reviewed-policy" || c.WithdrawalDeductionApproval != "synthetic-reviewed-policy" {
		t.Fatalf("explicit policy not loaded: %v", err)
	}
}
