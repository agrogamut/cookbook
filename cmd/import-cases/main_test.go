package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/madamgy/recipie/internal/casesource"
	"github.com/xuri/excelize/v2"
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

func TestCLICheckOnlyArguments(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-triage"}, "require -check"},
		{[]string{"-check", "-triage", "-activate"}, "forbid -activate"},
		{[]string{"-check", "-corrections", "missing", "-candidate-archive", "missing", "-activate"}, "forbid -activate"},
		{[]string{"-check", "-corrections", "missing"}, "must be supplied together"},
		{[]string{"-check", "-candidate-archive", "missing"}, "must be supplied together"},
		{[]string{"-check", "-provider-dir", "missing"}, "requires -triage"},
		{[]string{"-triage", "-provider-dir", "missing"}, "require -check"},
		{[]string{"-check", "-triage", "-provider-dir", "missing", "-activate"}, "forbid -activate"},
		{[]string{"-check", "-triage", "-provider-dir", "missing", "-corrections", "missing", "-candidate-archive", "missing"}, "cannot be combined"},
		{[]string{"-check", "-triage", "-provider-report", "missing"}, "flag provided but not defined"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var output bytes.Buffer
			args := append([]string{"-archive", "must-not-open", "-database-url", "postgres://127.0.0.1:1/never_connect"}, tc.args...)
			if err := execute(context.Background(), args, &output); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatal("invalid flags were not rejected before input or database access", err)
			}
		})
	}
}

func cliSourcePaths() (string, string) {
	path := os.Getenv("CASE_SOURCE_BENGALI_ARCHIVE")
	if path == "" {
		path = "../../data/case-source/" + casesource.FinalArchiveSHA256 + ".zip"
	}
	return path, "../../data/case-source/3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2.zip"
}

func TestCLITriage(t *testing.T) {
	archive, baseline := cliSourcePaths()
	args := []string{"-check", "-triage", "-archive", archive, "-baseline", baseline, "-database-url", "postgres://127.0.0.1:1/never_connect"}
	var output bytes.Buffer
	if err := execute(context.Background(), args, &output); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Triage casesource.TriageReport `json:"triage"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	r := result.Triage
	if r.Version != casesource.TriageVersion || r.ArchiveSHA256 != casesource.FinalArchiveSHA256 || len(r.Recipes) == 0 || r.ProviderPending != nil || r.CombinedPending != nil || r.ActivationPermitted || r.SupplierApprovalVerified {
		t.Fatal("check-only triage lost source/scope or granted approval")
	}
	t.Log("TRIAGE_CLI_OK source gaps emitted without a database; provider coverage remains unknown")
}

func TestCLIProviderTriage(t *testing.T) {
	archive, baseline := cliSourcePaths()
	args := []string{"-check", "-triage", "-archive", archive, "-baseline", baseline, "-database-url", "postgres://127.0.0.1:1/never_connect", "-provider-dir", "../../data/provider"}
	var output bytes.Buffer
	if err := execute(context.Background(), args, &output); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Triage casesource.TriageReport `json:"triage"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	r := result.Triage
	pending := 0
	for _, row := range r.Recipes {
		if row.SourceKind == "provider_observation" {
			if row.ProviderEvidence == nil || row.RawRowSHA256 == nil || row.RevisionID != "" {
				t.Fatal("provider source binding missing or runtime revision invented")
			}
			for _, hold := range row.Holds {
				if hold.Code == "whole_soy_reformulation_missing" {
					pending++
					break
				}
			}
		}
	}
	if r.ProviderSource == nil || len(r.ProviderSource.Workbooks) != 3 || r.ProviderPending == nil || *r.ProviderPending != pending || pending == 0 || r.CombinedPending == nil || *r.CombinedPending != r.CasePending+pending || r.ActivationPermitted || r.SupplierApprovalVerified {
		t.Fatal("provider source counts or nonactivation contract differs")
	}
	invalid := t.TempDir()
	if err := os.WriteFile(filepath.Join(invalid, r.ProviderSource.Workbooks[0].Workbook), []byte("wrong source bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	args[len(args)-1] = invalid
	output.Reset()
	if err := execute(context.Background(), args, &output); err == nil || !strings.Contains(err.Error(), "SHA-256") || output.Len() != 0 {
		t.Fatal("invalid provider source reached output or database path", err)
	}
	t.Logf("PORTABLE_PROVIDER_CLI_OK pending=%d combined=%d; invalid input rejected without database access", pending, *r.CombinedPending)
}

func TestCLICorrectionSourceBinding(t *testing.T) {
	archivePath, baselinePath := cliSourcePaths()
	original, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	base, err := casesource.ParseIntake(context.Background(), original, baseline)
	if err != nil {
		t.Fatal(err)
	}
	workbook := base.Archive.Workbooks[0]
	var master casesource.Sheet
	var row casesource.Row
	for _, sheet := range workbook.Sheets {
		if sheet.Name == "Recipe_Master" {
			master = sheet
		}
	}
	for _, r := range master.Rows {
		if r.RecipeID != "" {
			row = r
			break
		}
	}
	if row.RecipeID == "" {
		t.Fatal("source has no first recipe")
	}
	col := 0
	for n, name := range master.Headers {
		if name == "Energy_kcal" {
			col = n + 1
		}
	}
	cell, err := excelize.CoordinatesToCellName(col, row.Number)
	if err != nil {
		t.Fatal(err)
	}
	// The changed value is a temporary test fixture, never an approved source correction.
	candidateBytes := replaceCLIMember(t, original, workbook.Name, func(data []byte) []byte {
		book, err := excelize.OpenReader(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		defer book.Close()
		if err := book.SetCellValue("Recipe_Master", cell, 0); err != nil {
			t.Fatal(err)
		}
		b, err := book.WriteToBuffer()
		if err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	})
	candidate, err := casesource.ParseIntake(context.Background(), candidateBytes, baseline)
	if err != nil {
		t.Fatal(err)
	}
	var changedRow casesource.Row
	for _, sheet := range candidate.Archive.Workbooks[0].Sheets {
		if sheet.Name == "Recipe_Master" {
			for _, r := range sheet.Rows {
				if r.RecipeID == row.RecipeID {
					changedRow = r
				}
			}
		}
	}
	rowHash := func(r casesource.Row) string {
		b, err := json.Marshal(r.Cells)
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("%x", sha256.Sum256(b))
	}
	manifest := casesource.CorrectionManifest{Version: "source-correction-v1", BaseArchiveSHA256: base.Archive.SHA256,
		CandidateArchiveSHA256: candidate.Archive.SHA256, Corrections: []casesource.DeclaredCorrection{{
			RecipeID: row.RecipeID, BaseMemberSHA256: workbook.SHA256, BaseRowSHA256: rowHash(row),
			CandidateMemberSHA256: candidate.Archive.Workbooks[0].SHA256, CandidateRowSHA256: rowHash(changedRow),
			Fields: map[string]string{"Energy_kcal": "kcal"}, Evidence: casesource.CorrectionEvidence{
				DocumentSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("test fixture"))), DocumentReference: "test fixture",
				ReviewReference: "unverified test reference", NutritionBasis: "per_recipe", CookedYieldGrams: "100", NutritionSource: "test fixture only"}}}}
	dir := t.TempDir()
	candidatePath, manifestPath := filepath.Join(dir, "candidate.zip"), filepath.Join(dir, "corrections.json")
	if err := os.WriteFile(candidatePath, candidateBytes, 0600); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-check", "-archive", archivePath, "-baseline", baselinePath, "-corrections", manifestPath,
		"-candidate-archive", candidatePath, "-database-url", "postgres://127.0.0.1:1/never_connect"}
	var output bytes.Buffer
	if err := execute(context.Background(), args, &output); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Validation casesource.CorrectionValidation `json:"correction_validation"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Validation.StructurallyValid || result.Validation.ActivationPermitted || result.Validation.ClinicalApprovalVerified || result.Validation.SupplierApprovalVerified {
		t.Fatal("structural validation incorrectly granted approval")
	}
	if len(candidate.Glossary) == 0 || len(candidate.Locales) == 0 {
		t.Fatal("source lacks glossary or translation controls")
	}
	glossary, locale := candidate.Glossary[0], candidate.Locales[0]
	for _, tc := range []struct {
		name, sheet, field, current, rejection string
		row                                    int
	}{
		{"glossary English", "Glossary", "English", glossary.English, "unsupported glossary change", glossary.Row},
		{"glossary Bengali", "Glossary", "Bengali", glossary.Bengali, "unsupported glossary change", glossary.Row},
		{"glossary site", "Glossary", "Where it prints", glossary.Site, "unsupported glossary change", glossary.Row},
		{"translation note", "For_translation", "Note", locale.Note, "unsupported translation note change", locale.Row},
	} {
		t.Run(tc.name, func(t *testing.T) {
			column := 0
			for _, sheet := range candidate.Archive.Workbooks[0].Sheets {
				if sheet.Name == tc.sheet {
					for n, header := range sheet.Headers {
						if header == tc.field {
							column = n + 1
						}
					}
				}
			}
			cell, err := excelize.CoordinatesToCellName(column, tc.row)
			if err != nil {
				t.Fatal(err)
			}
			variantBytes := replaceCLIMember(t, candidateBytes, workbook.Name, func(data []byte) []byte {
				book, err := excelize.OpenReader(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				defer book.Close()
				if err := book.SetCellStr(tc.sheet, cell, tc.current+" [unit fixture]"); err != nil {
					t.Fatal(err)
				}
				b, err := book.WriteToBuffer()
				if err != nil {
					t.Fatal(err)
				}
				return b.Bytes()
			})
			parsed, err := casesource.ParseIntake(context.Background(), variantBytes, baseline)
			if err != nil {
				t.Fatal("negative fixture must reach correction validation", err)
			}
			variant := manifest
			variant.Corrections = append([]casesource.DeclaredCorrection(nil), manifest.Corrections...)
			variant.CandidateArchiveSHA256 = parsed.Archive.SHA256
			variant.Corrections[0].CandidateMemberSHA256 = parsed.Archive.Workbooks[0].SHA256
			for _, sheet := range parsed.Archive.Workbooks[0].Sheets {
				if sheet.Name == "Recipe_Master" {
					for _, r := range sheet.Rows {
						if r.RecipeID == row.RecipeID {
							variant.Corrections[0].CandidateRowSHA256 = rowHash(r)
						}
					}
				}
			}
			data, err := json.Marshal(variant)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(candidatePath, variantBytes, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifestPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			output.Reset()
			if err := execute(context.Background(), args, &output); err == nil || !strings.Contains(err.Error(), tc.rejection) || output.Len() != 0 {
				t.Fatal("undeclared semantic edit accepted despite refreshed byte bindings", err)
			}
		})
	}
	if err := os.WriteFile(candidatePath, []byte("invalid ZIP"), 0600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := execute(context.Background(), args, &output); err == nil || !strings.Contains(err.Error(), "candidate archive") || output.Len() != 0 {
		t.Fatal("malformed candidate was not rejected before database access/output", err)
	}
	t.Log("CORRECTION_CLI_OK real ZIP bindings checked, undeclared glossary/note changes and malformed candidate rejected, no database connection or approval")
}

func replaceCLIMember(t *testing.T, source []byte, name string, edit func([]byte) []byte) []byte {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(source), int64(len(source)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	found := false
	for _, f := range r.File {
		if f.Name != name {
			if err := w.Copy(f); err != nil {
				t.Fatal(err)
			}
			continue
		}
		found = true
		reader, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		header := f.FileHeader
		member, err := w.CreateHeader(&header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := member.Write(edit(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("fixture member not found")
	}
	return out.Bytes()
}

func TestCheckInputBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.json")
	for _, value := range []string{"", "12345", "1234"} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := readCheckFile(path, 4)
		if value == "1234" {
			if err != nil || string(got) != value {
				t.Fatal("valid bounded input rejected", err)
			}
		} else if err == nil {
			t.Fatal("empty or oversized input accepted")
		}
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
