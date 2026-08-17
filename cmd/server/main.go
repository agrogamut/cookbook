package main

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/madamgy/recipie/internal/api"
	"github.com/madamgy/recipie/internal/config"
	"github.com/madamgy/recipie/internal/db"
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

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Port),
		Handler:           api.NewRouter(pool),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("server: listening on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
