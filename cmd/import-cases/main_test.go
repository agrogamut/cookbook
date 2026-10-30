package main

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestCLIFinalSourceCheck(t *testing.T) {
	path := os.Getenv("CASE_SOURCE_BENGALI_ARCHIVE")
	if path == "" {
		path = "../../data/case-source/" + casesource.FinalArchiveSHA256 + ".zip"
	}
	args := []string{"-check", "-archive", path, "-baseline", "../../data/case-source/3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2.zip", "-database-url", "postgres://127.0.0.1:1/never_connect"}
	var output bytes.Buffer
	if err := execute(context.Background(), args, &output); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Intake   casesource.IntakeReport `json:"intake"`
		Accepted bool                    `json:"accepted_final_source"`
		Explicit bool                    `json:"activation_requires_explicit_command"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || !result.Accepted || !result.Explicit || !result.Intake.ActivationBlocked || result.Intake.Translations != 78106 || result.Intake.FindingCounts["allergen_associations_removed"] != 376 {
		t.Fatal("staging blockers missing", err)
	}
	output.Reset()
	if err := execute(context.Background(), append(args, "-activate"), &output); err != nil {
		t.Fatal("check connected to the database", err)
	}
	t.Log("FINAL_CHECK_OK accepted exact contract, unchanged historical findings, dry check makes no database connection")
}
