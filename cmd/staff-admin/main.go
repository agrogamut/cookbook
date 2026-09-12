package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/madamgy/recipie/internal/config"
	"github.com/madamgy/recipie/internal/db"
	"github.com/madamgy/recipie/internal/portal"
)

func main() {
	email := flag.String("email", "", "Administrator email")
	name := flag.String("name", "", "Administrator name")
	flag.Parse()
	if *email == "" || *name == "" {
		log.Fatal("Pass --email and --name; supply the password on standard input.")
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	if cfg.SupabaseURL == "" {
		log.Fatal("Configure SUPABASE_URL and SUPABASE_SECRET_KEY first.")
	}
	password, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		log.Fatal("Read a password line from standard input: ", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	service := portal.New(pool, portal.Options{Identity: portal.NewSupabase(cfg.SupabaseURL, cfg.SupabaseSecretKey)})
	actor, err := service.Provision(ctx, *name, *email, strings.TrimRight(password, "\r\n"), "admin")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Administrator account created: %s\n", actor.Email)
}
