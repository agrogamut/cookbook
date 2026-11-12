package config

import (
	"strings"
	"testing"
	"time"
)

func TestShutdownTimeouts(t *testing.T) {
	for _, name := range []string{"DATABASE_URL", "PORT", "APP_ORIGIN", "SUPABASE_URL", "SUPABASE_SECRET_KEY", "RAZORPAY_KEY_ID", "RAZORPAY_KEY_SECRET", "SHUTDOWN_TIMEOUT", "SHUTDOWN_CLEANUP_TIMEOUT"} {
		t.Setenv(name, "")
	}
	t.Setenv("DATABASE_URL", "postgres://unused/test")
	for _, tc := range []struct {
		name, drain, cleanup, wantError string
		wantDrain, wantCleanup          time.Duration
	}{
		{name: "defaults", wantDrain: 5 * time.Second, wantCleanup: 3 * time.Second},
		{name: "long print drain", drain: "20m", cleanup: "4s", wantDrain: 20 * time.Minute, wantCleanup: 4 * time.Second},
		{name: "zero drain", drain: "0s", wantError: "SHUTDOWN_TIMEOUT"},
		{name: "negative drain", drain: "-1s", wantError: "SHUTDOWN_TIMEOUT"},
		{name: "invalid drain", drain: "soon", wantError: "SHUTDOWN_TIMEOUT"},
		{name: "overflow", drain: "999999999999999999999h", wantError: "SHUTDOWN_TIMEOUT"},
		{name: "zero cleanup", cleanup: "0s", wantError: "SHUTDOWN_CLEANUP_TIMEOUT"},
		{name: "invalid cleanup", cleanup: "soon", wantError: "SHUTDOWN_CLEANUP_TIMEOUT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SHUTDOWN_TIMEOUT", tc.drain)
			t.Setenv("SHUTDOWN_CLEANUP_TIMEOUT", tc.cleanup)
			cfg, err := Load()
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("expected %s error, got %v", tc.wantError, err)
				}
				return
			}
			if err != nil || cfg.ShutdownTimeout != tc.wantDrain || cfg.ShutdownCleanupTimeout != tc.wantCleanup {
				t.Fatalf("timeouts = %s/%s, error %v", cfg.ShutdownTimeout, cfg.ShutdownCleanupTimeout, err)
			}
		})
	}
}
