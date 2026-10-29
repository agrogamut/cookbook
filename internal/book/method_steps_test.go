package book

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/madamgy/recipie/internal/casesource"
	"github.com/madamgy/recipie/internal/profile"
	"github.com/madamgy/recipie/internal/xlsx"
)

func TestMethodStepsPreserveSourceText(t *testing.T) {
	for _, tt := range []struct {
		name, source string
		want         []string
	}{
		{"integer quantities", "1) Measure rice (25), dal (12) and oil (2). 2) Cook.", []string{"Measure rice (25), dal (12) and oil (2).", "Cook."}},
		{"decimal quantities", "1) Measure turmeric (0.3) and cumin (0.5). 2) Stir for 1.5 min.", []string{"Measure turmeric (0.3) and cumin (0.5).", "Stir for 1.5 min."}},
		{"nested parentheses", "1) Keep the label (batch (2), portion 3) intact. 2) Serve.", []string{"Keep the label (batch (2), portion 3) intact.", "Serve."}},
		{"spaced parentheses", "1) Measure ( 25) and ( 0.3). 2) Serve.", []string{"Measure ( 25) and ( 0.3).", "Serve."}},
		{"inline steps", "1) Wash 2) Cook 3) Serve", []string{"Wash", "Cook", "Serve"}},
		{"multiline steps", "  1) Wash\n\t2) Cook\r\n3) Serve", []string{"Wash", "Cook", "Serve"}},
		{"unicode separators", "1)\u00a0Wash\u20032)\tCook", []string{"Wash", "Cook"}},
		{"interior whitespace", "1) Keep  two\nspaces (25). 2) Done.  ", []string{"Keep  two\nspaces (25).", "Done.  "}},
		{"unnumbered", "  Measure rice (25) and oil (0.3).\n", []string{"  Measure rice (25) and oil (0.3).\n"}},
		{"preface", "Source warning. 1) Wash 2) Cook", []string{"Source warning. 1) Wash 2) Cook"}},
		{"duplicate step", "1) Wash 2) Cook 2) Serve", []string{"1) Wash 2) Cook 2) Serve"}},
		{"missing step", "1) Wash 3) Serve", []string{"1) Wash 3) Serve"}},
		{"missing first step", "2) Cook 3) Serve", []string{"2) Cook 3) Serve"}},
		{"empty step", "1) Wash 2)   ", []string{"1) Wash 2)   "}},
		{"empty first step", "1) 2) Cook", []string{"1) 2) Cook"}},
		{"parenthesized numeric phrase", "1) Keep (25 2) intact", []string{"Keep (25 2) intact"}},
		{"unclosed parenthesis", "1) Keep (25 and serve.", []string{"1) Keep (25 and serve."}},
		{"no separator", "1)Wash 2)Cook", []string{"1)Wash 2)Cook"}},
		{"other numbering", "1. Wash 2. Cook", []string{"1. Wash 2. Cook"}},
		{"leading zero", "01) Wash 02) Cook", []string{"01) Wash 02) Cook"}},
		{"whitespace only", " \n ", []string{" \n "}},
		{"empty", "", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := splitMethodSteps(tt.source); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("source=%q\ngot=%q\nwant=%q", tt.source, got, tt.want)
			}
		})
	}
}

type methodSourceEvidence struct {
	Corpus       string   `json:"corpus"`
	RecipeID     string   `json:"recipe_id"`
	Raw          string   `json:"raw"`
	Steps        []string `json:"steps"`
	NativeSteps  []string `json:"native_steps"`
	Markers      []string `json:"structural_markers"`
	QuantityText []string `json:"parenthesized_quantities"`
}

// This oracle describes the observed workbook format, independently of the parser scanner.
var corpusMethodMarker = regexp.MustCompile(`(?:^|[ \t\r\n]+)([1-9][0-9]*)\)[ \t\r\n]+`)
var methodNumericFact = regexp.MustCompile(`[+-]?(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)`)
var parenthesizedQuantity = regexp.MustCompile(`\([0-9]+(?:\.[0-9]+)?\)`)

func methodCorpusOracle(raw string) ([]string, []string) {
	matches := corpusMethodMarker.FindAllStringSubmatchIndex(raw, -1)
	if len(matches) == 0 || matches[0][0] != 0 {
		return []string{raw}, nil
	}
	var parts, markers []string
	for i, match := range matches {
		if raw[match[2]:match[3]] != strconv.Itoa(i+1) {
			return []string{raw}, nil
		}
		end := len(raw)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		part := raw[match[1]:end]
		if strings.TrimSpace(part) == "" {
			return []string{raw}, nil
		}
		parts = append(parts, part)
		markers = append(markers, raw[match[0]:match[1]])
	}
	return parts, markers
}

func TestMethodOriginalCorpusFidelity(t *testing.T) {
	archive, err := os.ReadFile("../../data/case-source/3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2.zip")
	if err != nil {
		t.Fatal(err)
	}
	source, err := casesource.Parse(context.Background(), archive)
	if err != nil {
		t.Fatal(err)
	}
	if source.SHA256 != "3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2" {
		t.Fatal("original source archive changed")
	}
	var records []methodSourceEvidence
	for _, workbook := range source.Workbooks {
		for _, sheet := range workbook.Sheets {
			if !slices.Contains(sheet.Headers, "Preparation_Method_FULL") {
				continue
			}
			for _, row := range sheet.Rows {
				if row.RecipeID == "" {
					continue
				}
				records = append(records, methodSourceEvidence{Corpus: "case", RecipeID: row.RecipeID, Raw: casesource.Fields(sheet, row)["Preparation_Method_FULL"]})
			}
		}
	}
	if len(records) != 6000 {
		t.Fatalf("case methods=%d", len(records))
	}
	legacy, err := xlsx.Load("../../data/provider/MadamGY_Recipe_Master_1000_Detailed_V2.xlsx", "Recipe_Master", 4)
	if err != nil {
		t.Fatal(err)
	}
	idCol, methodCol := legacy.Index("recipe_id"), legacy.Index("preparation_method_full")
	if idCol < 0 || methodCol < 0 || len(legacy.Rows) != 1000 {
		t.Fatalf("unexpected original provider layout: id=%d method=%d rows=%d", idCol, methodCol, len(legacy.Rows))
	}
	for _, row := range legacy.Rows {
		records = append(records, methodSourceEvidence{Corpus: "provider", RecipeID: row[idCol], Raw: row[methodCol]})
	}
	selected := map[string]int{"MG-WB-C12-010": 9, "MG-WB-C12-011": 8, "MG-WB-C12-012": 7, "MG-WB-C13-012": 9, "MG-WB-C13-013": 9, "MG-WB-C13-014": 7}
	seen := map[string]bool{}
	fallbacks := map[string]int{}
	var failures []string
	selectedQuantities := 0
	for start := 0; start < len(records); start += 100 {
		batch := records[start:min(start+100, len(records))]
		var cards []RecipeCard
		for i := range batch {
			r := &batch[i]
			if seen[r.RecipeID] {
				t.Fatalf("duplicate original recipe %s", r.RecipeID)
			}
			seen[r.RecipeID] = true
			r.Steps = splitMethodSteps(r.Raw)
			want, markers := methodCorpusOracle(r.Raw)
			r.Markers = markers
			if len(markers) == 0 {
				fallbacks[r.Corpus]++
			} else {
				var restored strings.Builder
				for j, marker := range markers {
					restored.WriteString(marker)
					restored.WriteString(want[j])
				}
				if restored.String() != r.Raw {
					t.Fatalf("%s oracle did not reconstruct the original source", r.RecipeID)
				}
			}
			r.QuantityText = parenthesizedQuantity.FindAllString(r.Raw, -1)
			if !reflect.DeepEqual(r.Steps, want) {
				failures = append(failures, r.RecipeID+": source characters or structural segmentation changed")
			}
			if !reflect.DeepEqual(methodNumericFact.FindAllString(strings.Join(r.Steps, ""), -1), methodNumericFact.FindAllString(strings.Join(want, ""), -1)) {
				failures = append(failures, r.RecipeID+": method numeric occurrence sequence changed")
			}
			if count, ok := selected[r.RecipeID]; ok {
				if len(r.QuantityText) != count {
					t.Fatalf("%s original parenthesized quantities=%d want=%d", r.RecipeID, len(r.QuantityText), count)
				}
				selectedQuantities += count
				if !reflect.DeepEqual(parenthesizedQuantity.FindAllString(strings.Join(r.Steps, ""), -1), r.QuantityText) {
					failures = append(failures, r.RecipeID+": known selected quantity loss")
				}
			}
			cards = append(cards, RecipeCard{RecipeID: r.RecipeID, Source: r.Corpus, MethodSteps: r.Steps})
		}
		bound := NativeBindings(profile.Stored{}, Set{Book2: Book2{MealSections: []MealSection{{Title: "Breakfast", Recipes: cards}}}})
		entries := bound.Books[1].Modules["B2-09"].Entries
		if len(entries) != len(batch) {
			t.Fatalf("native entries=%d source=%d", len(entries), len(batch))
		}
		for i, entry := range entries {
			if entry.ID != batch[i].RecipeID {
				t.Fatal("native method identity changed")
			}
			inMethod := false
			for _, block := range entry.Blocks {
				if block.Paragraph == nil {
					continue
				}
				var text strings.Builder
				for _, run := range block.Paragraph.Runs {
					text.WriteString(run.Text)
				}
				s := text.String()
				if strings.HasPrefix(s, "Verified recipe nutrition is unavailable.") {
					break
				}
				if inMethod {
					batch[i].NativeSteps = append(batch[i].NativeSteps, s)
				}
				if s == "Cooked yield and household measures are not recorded." {
					inMethod = true
				}
			}
			want, _ := methodCorpusOracle(batch[i].Raw)
			if !reflect.DeepEqual(batch[i].NativeSteps, want) {
				failures = append(failures, entry.ID+": method field differs in NativeBindings")
			}
		}
	}
	if selectedQuantities != 49 {
		t.Fatalf("selected original quantity occurrences=%d", selectedQuantities)
	}
	if dir := os.Getenv("BOOK_METHOD_EVIDENCE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		data, err := json.MarshalIndent(struct {
			Records  []methodSourceEvidence `json:"records"`
			Failures []string               `json:"failures"`
		}{records, failures}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "method-corpus.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("METHOD_CORPUS case=6000 provider=1000 selected_recipes=6 selected_quantities=49 verbatim_fallbacks=%v failures=%d", fallbacks, len(failures))
	if len(failures) > 0 {
		t.Fatal(fmt.Sprintf("%d field-scoped failures; first=%v", len(failures), failures[:min(20, len(failures))]))
	}
}
