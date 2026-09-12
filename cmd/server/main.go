package main

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/api"
	"github.com/madamgy/recipie/internal/book"
	"github.com/madamgy/recipie/internal/config"
	"github.com/madamgy/recipie/internal/db"
	"github.com/madamgy/recipie/internal/portal"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("server: %v", err)
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("server: %v", err)
	}
	defer pool.Close()

	// Gemini has always been optional here: NewClient returns a no-op Drafter for an empty
	// key and only errors when a *present* key fails the SDK's own constructor, so an unset
	// GEMINI_API_KEY never reaches this branch -- generation just runs without drafted
	// content, exactly as it always has.
	drafter, err := aidraft.NewClient(ctx, cfg.GeminiAPIKey)
	if err != nil {
		log.Fatalf("server: %v", err)
	}

	// Launch the print browser before the first request needs it.
	//
	// Chromium's launch is about fourteen seconds on the deployed free instance -- measured,
	// not estimated: Book 1 alone took 24.4s and Book 2 alone 20.5s while both together in
	// one browser took 31.2s. Paying that at boot, when nobody is waiting, is the difference
	// between an operator's first PDF being slow and it reading as broken.
	//
	// Not fatal. A service that cannot print is still one that serves the console, the audit
	// pages and the HTML previews, and every print route already reports a missing renderer
	// as a 503 rather than pretending.
	if err := book.WarmUp(); err != nil {
		log.Printf("server: print browser unavailable, PDF routes will return 503: %v", err)
	}
	defer book.ShutdownBrowser()

	srv := &http.Server{
		Addr: ":" + strconv.Itoa(cfg.Port),
		Handler: api.NewRouter(pool, drafter, portal.New(pool, portal.Options{
			Identity: portal.NewSupabase(cfg.SupabaseURL, cfg.SupabaseSecretKey),
			Gateway:  portal.NewRazorpay(cfg.RazorpayKeyID, cfg.RazorpayKeySecret, cfg.RazorpayWebhookSecret),
			Origin:   cfg.AppOrigin, SecureCookies: cfg.SecureCookies,
		})),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("server: listening on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
