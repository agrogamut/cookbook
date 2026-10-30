package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/madamgy/recipie/internal/casesource"
)

func TestCLIArguments(t *testing.T) {
	var output bytes.Buffer
	if err := execute(context.Background(), []string{"-archive", "not-read-without-explicit-database"}, &output); err == nil || !strings.Contains(err.Error(), "database-url is required") {
		t.Fatal("ambient database access was not refused", err)
	}
	if err := execute(context.Background(), []string{"-check", "unexpected"}, &output); err == nil {
		t.Fatal("positional argument ignored")
	}
	if err := execute(context.Background(), []string{"-check", "-archive", "../../data/case-source/3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2.zip", "-baseline", "absent"}, &output); err != nil || !strings.Contains(output.String(), "workbooks=60 valid") {
		t.Fatal("legacy CLI contract changed", err)
	}
}

func TestCLILocaleBlocked(t *testing.T) {
	path := os.Getenv("CASE_SOURCE_BENGALI_ARCHIVE")
	if path == "" {
		t.Skip("CASE_SOURCE_BENGALI_ARCHIVE not set")
	}
	args := []string{"-check", "-archive", path, "-baseline", "../../data/case-source/3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2.zip", "-database-url", "postgres://127.0.0.1:1/never_connect"}
	var output bytes.Buffer
	if err := execute(context.Background(), args, &output); err != nil {
		t.Fatal(err)
	}
	var report casesource.IntakeReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil || !report.ActivationBlocked || report.Translations != 78106 || report.FindingCounts["allergen_associations_removed"] != 376 {
		t.Fatal("staging blockers missing", err)
	}
	output.Reset()
	if err := execute(context.Background(), append(args, "-activate"), &output); !errors.Is(err, casesource.ErrLocaleActivation) {
		t.Fatal("v2 activation did not fail before database connection", err)
	}
	t.Log("INTAKE_BLOCKERS_OK CLI dry validation reports source findings and refuses activation without DB access")
}
