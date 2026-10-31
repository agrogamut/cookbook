package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/madamgy/recipie/internal/casesource"
)

type contract struct {
	Environment    string     `json:"environment"`
	Kind           string     `json:"kind"`
	Repository     string     `json:"repository"`
	SourceSHA      string     `json:"source_sha"`
	ArchiveSHA256  string     `json:"archive_sha256"`
	SourceContract string     `json:"source_contract"`
	Deployment     deployment `json:"deployment"`
	Target         target     `json:"target"`
	Expected       state      `json:"expected"`
}

type deployment struct {
	SourceSHA   string `json:"source_sha"`
	ImageDigest string `json:"image_digest"`
	Evidence    string `json:"evidence"`
}

type target struct {
	Host              string `json:"host"`
	Port              uint16 `json:"port"`
	Database          string `json:"database"`
	Role              string `json:"role"`
	ClusterIdentifier string `json:"cluster_identifier"`
}

type state struct {
	Migration        int64  `json:"migration"`
	Dirty            bool   `json:"dirty"`
	Archives         int64  `json:"archives"`
	CurrentRecipes   int64  `json:"current_recipes"`
	FinalRecipes     int64  `json:"final_recipes"`
	FinalLocales     int64  `json:"final_locales"`
	FinalOccurrences int64  `json:"final_occurrences"`
	PointerSHA256    string `json:"pointer_sha256"`
}

var sha40 = regexp.MustCompile(`^[0-9a-f]{40}$`)
var sha64 = regexp.MustCompile(`^[0-9a-f]{64}$`)
var name = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > limit {
		return nil, errors.New("input must be a bounded regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, errors.New("input exceeded its read bound")
	}
	return b, nil
}

func (c contract) validate(environment, head string, archive []byte) error {
	if !name.MatchString(c.Environment) || environment != c.Environment || (c.Kind != "staging" && c.Kind != "production") {
		return errors.New("explicit environment does not match contract")
	}
	if c.Repository != "git@github.com:agrogamut/cookbook.git" || !sha40.MatchString(c.SourceSHA) || c.SourceSHA != head {
		return errors.New("repository or source revision mismatch")
	}
	if c.ArchiveSHA256 != casesource.FinalArchiveSHA256 || fmt.Sprintf("%x", sha256.Sum256(archive)) != c.ArchiveSHA256 || c.SourceContract != "final-bn-v1" {
		return errors.New("accepted source archive or contract mismatch")
	}
	if c.Deployment.SourceSHA != c.SourceSHA || !strings.HasPrefix(c.Deployment.ImageDigest, "sha256:") || !sha64.MatchString(strings.TrimPrefix(c.Deployment.ImageDigest, "sha256:")) || strings.TrimSpace(c.Deployment.Evidence) == "" {
		return errors.New("deployment revision and evidence are required")
	}
	if c.Target.Host == "" || strings.ContainsAny(c.Target.Host, "/@? \t\n") || c.Target.Port == 0 || !name.MatchString(c.Target.Database) || !name.MatchString(c.Target.Role) || !regexp.MustCompile(`^[0-9]+$`).MatchString(c.Target.ClusterIdentifier) {
		return errors.New("complete explicit database target is required")
	}
	if c.Expected.Migration < 49 || c.Expected.Dirty || !sha64.MatchString(c.Expected.PointerSHA256) || c.Expected.Archives < 0 || c.Expected.CurrentRecipes < 0 || c.Expected.FinalRecipes < 0 || c.Expected.FinalLocales < 0 || c.Expected.FinalOccurrences < 0 {
		return errors.New("explicit clean database revision/count prerequisites are required")
	}
	return nil
}

func connection(c contract, dsn string) (*pgx.ConnConfig, error) {
	if dsn == "" {
		return nil, errors.New("RELEASE_DATABASE_URL is required only for explicit database inspection")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("invalid release database connection")
	}
	if cfg.Host != c.Target.Host || cfg.Port != c.Target.Port || cfg.Database != c.Target.Database || cfg.User != c.Target.Role {
		return nil, errors.New("connection does not match the named target")
	}
	if c.Kind == "production" && (cfg.TLSConfig == nil || cfg.TLSConfig.InsecureSkipVerify) {
		return nil, errors.New("production inspection requires verified TLS")
	}
	if len(cfg.Fallbacks) != 0 {
		return nil, errors.New("connection fallbacks are not allowed for release inspection")
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	cfg.RuntimeParams["application_name"] = "recipe-release-inspection"
	cfg.RuntimeParams["default_transaction_read_only"] = "on"
	cfg.RuntimeParams["statement_timeout"] = "30000"
	cfg.RuntimeParams["search_path"] = "public,pg_catalog"
	cfg.ConnectTimeout = 10 * time.Second
	return cfg, nil
}

func inspect(ctx context.Context, c contract, cfg *pgx.ConnConfig) (state, error) {
	var observed state
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return observed, errors.New("cannot connect to declared release target")
	}
	defer conn.Close(ctx)
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return observed, errors.New("cannot begin read-only inspection")
	}
	defer tx.Rollback(ctx)
	var cluster, database, role string
	if err := tx.QueryRow(ctx, "SELECT system_identifier::text,current_database(),current_user FROM pg_control_system()").Scan(&cluster, &database, &role); err != nil {
		return observed, errors.New("cluster identity inspection unavailable; no activation performed")
	}
	if cluster != c.Target.ClusterIdentifier || database != c.Target.Database || role != c.Target.Role {
		return observed, errors.New("observed database identity mismatch")
	}
	if err := tx.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&observed.Migration, &observed.Dirty); err != nil {
		return observed, errors.New("migration state inspection failed")
	}
	queries := []struct {
		sql   string
		value *int64
	}{
		{"SELECT count(*) FROM case_source_archive", &observed.Archives},
		{"SELECT count(*) FROM case_source_recipe_current", &observed.CurrentRecipes},
		{"SELECT count(*) FROM case_source_active_recipe WHERE archive_sha256=$1", &observed.FinalRecipes},
		{"SELECT count(*) FROM case_source_locale_text WHERE archive_sha256=$1", &observed.FinalLocales},
		{"SELECT count(*) FROM case_source_ingredient WHERE archive_sha256=$1", &observed.FinalOccurrences},
	}
	for _, query := range queries {
		var args []any
		if strings.Contains(query.sql, "$1") {
			args = []any{c.ArchiveSHA256}
		}
		if err := tx.QueryRow(ctx, query.sql, args...).Scan(query.value); err != nil {
			return observed, errors.New("source count inspection failed")
		}
	}
	rows, err := tx.Query(ctx, "SELECT case_number,recipe_id,candidate_id FROM case_source_recipe_current ORDER BY case_number,recipe_id")
	if err != nil {
		return observed, errors.New("current source pointer inspection failed")
	}
	h := sha256.New()
	for rows.Next() {
		var number int
		var recipe, candidate string
		if err := rows.Scan(&number, &recipe, &candidate); err != nil {
			rows.Close()
			return observed, errors.New("invalid pointer row")
		}
		fmt.Fprintf(h, "%d\t%s\t%s\n", number, recipe, candidate)
	}
	rows.Close()
	if rows.Err() != nil {
		return observed, errors.New("incomplete current pointer inspection")
	}
	observed.PointerSHA256 = hex.EncodeToString(h.Sum(nil))
	if !reflect.DeepEqual(observed, c.Expected) {
		return observed, errors.New("observed database revision, counts or current pointers differ from contract")
	}
	if tx.Commit(ctx) != nil {
		return observed, errors.New("read-only snapshot close failed")
	}
	return observed, nil
}

func execute(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("release-preflight", flag.ContinueOnError)
	flags.SetOutput(out)
	contractPath := flags.String("contract", "", "operator-owned target and revision contract")
	environment := flags.String("environment", "", "exact named environment to inspect")
	root := flags.String("root", ".", "clean source checkout")
	archivePath := flags.String("archive", "", "exact accepted source ZIP")
	withDB := flags.Bool("inspect-database", false, "inspect only; never migrate, import, activate or deploy")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *contractPath == "" || *environment == "" || *archivePath == "" {
		return errors.New("contract, environment and archive are required")
	}
	b, err := readBounded(*contractPath, 64<<10)
	if err != nil {
		return errors.New("cannot read release contract")
	}
	var c contract
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return errors.New("invalid release contract")
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing release contract data")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	var prerequisites map[string]json.RawMessage
	if err := json.Unmarshal(fields["expected"], &prerequisites); err != nil {
		return errors.New("explicit expected state is required")
	}
	for _, key := range []string{"migration", "dirty", "archives", "current_recipes", "final_recipes", "final_locales", "final_occurrences", "pointer_sha256"} {
		if value, ok := prerequisites[key]; !ok || string(value) == "null" {
			return fmt.Errorf("expected state must explicitly include %s", key)
		}
	}
	git := func(arguments ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", arguments...)
		cmd.Dir = *root
		value, err := cmd.Output()
		return strings.TrimSpace(string(value)), err
	}
	head, err := git("rev-parse", "HEAD")
	if err != nil {
		return errors.New("cannot verify local source revision")
	}
	status, err := git("status", "--porcelain", "--untracked-files=all")
	if err != nil || status != "" {
		return errors.New("release source must be clean, including untracked files")
	}
	archive, err := readBounded(*archivePath, 32<<20)
	if err != nil {
		return errors.New("cannot read source archive")
	}
	if err := c.validate(*environment, head, archive); err != nil {
		return err
	}
	result := map[string]any{"environment": c.Environment, "source_sha": head, "archive_sha256": c.ArchiveSHA256, "activation_performed": false, "deployment_performed": false, "database_inspected": false, "deployment_evidence": "operator-supplied, not a runtime probe"}
	if *withDB {
		cfg, err := connection(c, os.Getenv("RELEASE_DATABASE_URL"))
		if err != nil {
			return err
		}
		observed, err := inspect(ctx, c, cfg)
		result["observed_database"] = observed
		result["database_inspection_attempted"] = true
		result["database_inspected"] = err == nil
		if err != nil {
			result["error"] = err.Error()
			_ = json.NewEncoder(out).Encode(result)
			return err
		}
	}
	return json.NewEncoder(out).Encode(result)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := execute(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
