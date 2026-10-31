package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/madamgy/recipie/internal/casesource"
)

func TestReleaseDatabaseInspection(t *testing.T) {
	dsn := os.Getenv("CASE_SOURCE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CASE_SOURCE_TEST_DATABASE_URL requires an isolated fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	base, err := pgx.ParseConfig(dsn)
	if err != nil || (base.Host != "127.0.0.1" && base.Host != "localhost") {
		t.Fatal("release inspection test requires loopback fixture")
	}
	admin, err := pgx.ConnectConfig(ctx, base)
	if err != nil {
		t.Fatal("cannot connect to release fixture")
	}
	defer admin.Close(ctx)
	c, _ := validContract(t)
	c.Target = target{Host: base.Host, Port: base.Port, Database: base.Database, Role: base.User}
	if err := admin.QueryRow(ctx, "SELECT system_identifier::text FROM pg_control_system()").Scan(&c.Target.ClusterIdentifier); err != nil {
		t.Fatal(err)
	}
	cfg, err := connection(c, dsn)
	if err != nil {
		t.Fatal(err)
	}
	observed, _ := inspect(ctx, c, cfg)
	if observed.Migration < 49 || observed.Dirty || !sha64.MatchString(observed.PointerSHA256) {
		t.Fatal("fixture inspection did not finish")
	}
	c.Expected = observed
	if after, err := inspect(ctx, c, cfg); err != nil || after != observed {
		t.Fatalf("repeat read-only inspection: %v", err)
	}
	c.Expected.CurrentRecipes++
	if _, err := inspect(ctx, c, cfg); err == nil {
		t.Fatal("count mismatch accepted")
	}
	c.Expected = observed
	c.Target.ClusterIdentifier += "1"
	if _, err := inspect(ctx, c, cfg); err == nil {
		t.Fatal("cluster mismatch accepted")
	}
	readOnly, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal("cannot verify read-only connection")
	}
	defer readOnly.Close(ctx)
	_, err = readOnly.Exec(ctx, "DELETE FROM case_source_recipe_current WHERE false")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "25006" {
		t.Fatalf("write was not refused by read-only connection: %v", err)
	}
}

func validContract(t *testing.T) (contract, []byte) {
	t.Helper()
	archive, err := os.ReadFile("../../data/case-source/" + casesource.FinalArchiveSHA256 + ".zip")
	if err != nil {
		t.Fatal(err)
	}
	head := strings.Repeat("1", 40)
	c := contract{Environment: "test-staging", Kind: "staging", Repository: "git@github.com:agrogamut/cookbook.git", SourceSHA: head, ArchiveSHA256: casesource.FinalArchiveSHA256, SourceContract: "final-bn-v1", Deployment: deployment{SourceSHA: head, ImageDigest: "sha256:" + strings.Repeat("2", 64), Evidence: "operator release receipt"}, Target: target{Host: "127.0.0.1", Port: 5432, Database: "staging", Role: "inspector", ClusterIdentifier: "123"}, Expected: state{Migration: 49, PointerSHA256: fmt.Sprintf("%x", sha256.Sum256(nil))}}
	return c, archive
}

func TestReleaseContract(t *testing.T) {
	c, archive := validContract(t)
	if err := c.validate(c.Environment, c.SourceSHA, archive); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*contract){
		"environment":         func(c *contract) { c.Environment = "another" },
		"repository":          func(c *contract) { c.Repository = "unrelated" },
		"revision":            func(c *contract) { c.SourceSHA = strings.Repeat("3", 40) },
		"archive":             func(c *contract) { c.ArchiveSHA256 = strings.Repeat("3", 64) },
		"source-contract":     func(c *contract) { c.SourceContract = "old" },
		"deployment":          func(c *contract) { c.Deployment.SourceSHA = strings.Repeat("3", 40) },
		"deployment-evidence": func(c *contract) { c.Deployment.Evidence = "" },
		"target":              func(c *contract) { c.Target.ClusterIdentifier = "" },
		"dirty":               func(c *contract) { c.Expected.Dirty = true },
		"count":               func(c *contract) { c.Expected.FinalLocales = -1 },
		"pointer":             func(c *contract) { c.Expected.PointerSHA256 = "" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := c
			change(&bad)
			if bad.validate(c.Environment, c.SourceSHA, archive) == nil {
				t.Fatal("invalid release accepted")
			}
		})
	}
	changed := append([]byte(nil), archive...)
	changed[0] ^= 1
	if c.validate(c.Environment, c.SourceSHA, changed) == nil {
		t.Fatal("changed archive accepted")
	}
}

func TestReleaseConnectionRequiresExactTarget(t *testing.T) {
	c, _ := validContract(t)
	dsn := "postgres://inspector:private@127.0.0.1:5432/staging?sslmode=disable"
	cfg, err := connection(c, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RuntimeParams["default_transaction_read_only"] != "on" || cfg.RuntimeParams["statement_timeout"] != "30000" {
		t.Fatal("inspection is not bounded/read-only")
	}
	for _, bad := range []string{"", strings.Replace(dsn, "127.0.0.1", "localhost", 1), strings.Replace(dsn, "5432", "5433", 1), strings.Replace(dsn, "staging", "other", 1), strings.Replace(dsn, "inspector", "other", 1)} {
		if _, err := connection(c, bad); err == nil {
			t.Fatal("unbound connection accepted")
		}
	}
	c.Kind = "production"
	if _, err := connection(c, dsn); err == nil {
		t.Fatal("unencrypted production inspection accepted")
	}
}

func TestReleaseNoImplicitTargetOrApply(t *testing.T) {
	for _, args := range [][]string{nil, {"--apply"}, {"--environment", "production"}} {
		if err := execute(context.Background(), args, &strings.Builder{}); err == nil {
			t.Fatal("implicit target or apply accepted")
		}
	}
	c, _ := validContract(t)
	b, _ := json.Marshal(c)
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw["expected"].(map[string]any), "final_locales")
	b, _ = json.Marshal(raw)
	path := filepath.Join(t.TempDir(), "contract.json")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	err := execute(context.Background(), []string{"--contract", path, "--environment", c.Environment, "--archive", "unused"}, &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "explicitly include final_locales") {
		t.Fatalf("missing prerequisite: %v", err)
	}
}

func TestReleaseReadBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(path, []byte("12345"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBounded(path, 4); err == nil {
		t.Fatal("oversized input accepted")
	}
	if value, err := readBounded(path, 5); err != nil || string(value) != "12345" {
		t.Fatalf("bounded input: %v", err)
	}
	if _, err := readBounded(filepath.Dir(path), 5); err == nil {
		t.Fatal("non-file input accepted")
	}
}

func TestReleaseOfflineDoesNotConnect(t *testing.T) {
	c, archive := validContract(t)
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture git: %v", err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "source.txt")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@localhost", "-c", "commit.gpgsign=false", "commit", "-qm", "test: fixture")
	c.SourceSHA = git("rev-parse", "HEAD")
	c.Deployment.SourceSHA = c.SourceSHA
	private := t.TempDir()
	contractPath := filepath.Join(private, "contract.json")
	archivePath := filepath.Join(private, "source.zip")
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contractPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, archive, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RELEASE_DATABASE_URL", "invalid-unreachable-not-used")
	args := []string{"--contract", contractPath, "--root", root, "--environment", c.Environment, "--archive", archivePath}
	var output strings.Builder
	if err := execute(context.Background(), args, &output); err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if err := json.Unmarshal([]byte(output.String()), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt["database_inspected"] != false || receipt["activation_performed"] != false || receipt["deployment_performed"] != false {
		t.Fatal("offline preflight claimed a mutation or connection")
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("dirty"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := execute(context.Background(), args, &strings.Builder{}); err == nil {
		t.Fatal("dirty release source accepted")
	}
}
