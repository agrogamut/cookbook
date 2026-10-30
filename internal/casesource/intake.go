package casesource

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const LocaleParserVersion = "2-bn-staging"

// The measured four-sheet delivery expands to 138135088 bytes; v1 keeps its 128 MiB bound.
const maxLocaleExpanded = 160 << 20

var ErrLocaleActivation = errors.New("Bengali v2 is unreconciled staging only; canonical activation is forbidden")
var localeMemberName = regexp.MustCompile(`^MadamGY_WB_Case_([0-9]{2})_Bengali\.xlsx$`)

type LocaleRow struct {
	Case, Row, MasterRow, Ordinal int
	RecipeID, Field               string
	English, EnglishSHA256        string
	Bengali, RawQuantity, Note    string
	Ingredient                    *Ingredient
}

type GlossaryRow struct {
	Case, Row              int
	English, Site, Bengali string
}

type Finding struct {
	Code     string            `json:"code"`
	Case     int               `json:"case,omitempty"`
	RecipeID string            `json:"recipe_id,omitempty"`
	Sheet    string            `json:"sheet,omitempty"`
	Row      int               `json:"row,omitempty"`
	Evidence map[string]string `json:"evidence"`
}

type IntakeReport struct {
	ArchiveSHA256     string         `json:"archive_sha256"`
	ParserVersion     string         `json:"parser_version"`
	BaselineSHA256    string         `json:"baseline_sha256,omitempty"`
	ActivationBlocked bool           `json:"activation_blocked"`
	Workbooks         int            `json:"workbooks"`
	Recipes           int            `json:"recipes"`
	Audits            int            `json:"audits"`
	Occurrences       int            `json:"occurrences"`
	Translations      int            `json:"translations"`
	Glossary          int            `json:"glossary"`
	ExpandedBytes     uint64         `json:"expanded_bytes,omitempty"`
	FindingCounts     map[string]int `json:"finding_counts"`
	FieldChanges      map[string]int `json:"field_changes,omitempty"`
	AllergenRemovals  map[string]int `json:"allergen_removals,omitempty"`
	Findings          []Finding      `json:"findings,omitempty"`
}

type Intake struct {
	Archive  *Archive
	Baseline *Archive
	Locales  []LocaleRow
	Glossary []GlossaryRow
	Report   IntakeReport
}

// ParseIntake validates source associations, never clinical equivalence or active eligibility.
func ParseIntake(ctx context.Context, data, baseline []byte) (*Intake, error) {
	if len(data) > MaxArchiveBytes {
		return nil, fmt.Errorf("case archive exceeds size limit")
	}
	files, err := packageFiles(data, 128)
	if err != nil {
		return nil, err
	}
	locale := false
	for name := range files {
		locale = locale || localeMemberName.MatchString(name)
	}
	i := &Intake{Report: IntakeReport{ParserVersion: ParserVersion, FindingCounts: map[string]int{}}}
	legacy, legacyErr := Parse(ctx, data)
	if legacyErr == nil {
		i.Archive = legacy
	} else {
		if !locale {
			return nil, legacyErr
		}
		if len(baseline) == 0 {
			return nil, fmt.Errorf("v2 staging requires an explicit original v1 comparison archive")
		}
		i.Baseline, err = Parse(ctx, baseline)
		if err != nil {
			return nil, fmt.Errorf("v1 comparison archive: %w", err)
		}
		i.Report.ParserVersion = LocaleParserVersion
		i.Report.BaselineSHA256 = i.Baseline.SHA256
		i.Report.ActivationBlocked = true
		i.Archive = &Archive{SHA256: hash(data)}
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		sort.Strings(names)
		remaining := uint64(maxLocaleExpanded)
		seen := map[int]bool{}
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			match := localeMemberName.FindStringSubmatch(name)
			if match == nil {
				return nil, fmt.Errorf("mixed or unknown v2 member %q", name)
			}
			n, _ := strconv.Atoi(match[1])
			if n < 1 || n > 60 || seen[n] {
				return nil, fmt.Errorf("invalid or repeated v2 case %d", n)
			}
			seen[n] = true
			b, err := readMember(files[name])
			if err != nil {
				return nil, err
			}
			w, err := parseWorkbookFormat(b, n, &remaining, true)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			w.Name, w.SHA256, w.Case = name, hash(b), n
			if err := i.bindLocale(w); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			i.Archive.Workbooks = append(i.Archive.Workbooks, w)
		}
		i.Report.ExpandedBytes = maxLocaleExpanded - remaining
		i.compareBaseline()
	}
	i.Report.ArchiveSHA256 = i.Archive.SHA256
	i.Report.Workbooks = len(i.Archive.Workbooks)
	for _, w := range i.Archive.Workbooks {
		for _, s := range w.Sheets {
			for _, r := range s.Rows {
				if r.RecipeID != "" && s.Name == "Recipe_Master" {
					i.Report.Recipes++
					i.Report.Occurrences += len(r.Ingredients)
				} else if r.RecipeID != "" && s.Name == "Uniqueness_Audit" {
					i.Report.Audits++
				}
			}
		}
	}
	i.Report.Translations, i.Report.Glossary = len(i.Locales), len(i.Glossary)
	return i, nil
}

func sheetByName(w Workbook, name string) Sheet {
	for _, s := range w.Sheets {
		if s.Name == name {
			return s
		}
	}
	return Sheet{}
}

func (i *Intake) bindLocale(w Workbook) error {
	master := sheetByName(w, "Recipe_Master")
	masters := map[string]Row{}
	for _, r := range master.Rows {
		if r.RecipeID != "" {
			masters[r.RecipeID] = r
		}
	}
	seen := map[string]map[string]int{}
	for _, r := range sheetByName(w, "For_translation").Rows {
		if r.Number <= 2 {
			continue
		}
		values := rowValues(r)
		if len(values) == 0 {
			continue
		}
		v := Fields(sheetByName(w, "For_translation"), r)
		m, ok := masters[r.RecipeID]
		if !ok || v["Meal_Type"] != Fields(master, m)["Meal_Type"] {
			return fmt.Errorf("translation row %d has no matching recipe/meal", r.Number)
		}
		if strings.TrimSpace(v["English"]) == "" || strings.TrimSpace(v["Bengali"]) == "" {
			return fmt.Errorf("translation row %d has an empty source or target", r.Number)
		}
		if seen[r.RecipeID] == nil {
			seen[r.RecipeID] = map[string]int{}
		}
		field := v["Field"]
		seen[r.RecipeID][field]++
		row := LocaleRow{Case: w.Case, Row: r.Number, MasterRow: m.Number, RecipeID: r.RecipeID, Field: field, English: v["English"], EnglishSHA256: hash([]byte(v["English"])), Bengali: v["Bengali"], RawQuantity: v["Quantity_g"], Note: v["Note"]}
		if field == "ingredient" {
			row.Ordinal = seen[r.RecipeID][field]
			if row.Ordinal > len(m.Ingredients) {
				return fmt.Errorf("extra ingredient translation at row %d", r.Number)
			}
			ing := m.Ingredients[row.Ordinal-1]
			if row.English != strings.TrimSpace(ing.RawName) || strings.TrimSpace(row.RawQuantity) != ing.Grams {
				return fmt.Errorf("ingredient source/quantity/ordinal mismatch at row %d", r.Number)
			}
			row.Ingredient = &ing
		} else {
			key := map[string]string{"title": "Recipe_Name", "method": "Preparation_Method_FULL", "safety": "Safety_Rule"}[field]
			if key == "" || seen[r.RecipeID][field] != 1 || row.RawQuantity != "" || row.English != Fields(master, m)[key] {
				return fmt.Errorf("unknown, duplicate or mismatched translation field at row %d", r.Number)
			}
		}
		i.Locales = append(i.Locales, row)
	}
	for id, m := range masters {
		v := seen[id]
		if v["title"] != 1 || v["method"] != 1 || v["safety"] != 1 || v["ingredient"] != len(m.Ingredients) {
			return fmt.Errorf("recipe %s has missing translation fields/occurrences", id)
		}
	}
	keys := map[string]bool{}
	for _, r := range sheetByName(w, "Glossary").Rows {
		if r.Number <= 2 || len(rowValues(r)) == 0 {
			continue
		}
		v := Fields(sheetByName(w, "Glossary"), r)
		key := v["English"] + "\x00" + v["Where it prints"]
		if keys[key] || strings.TrimSpace(v["English"]) == "" || strings.TrimSpace(v["Where it prints"]) == "" || strings.TrimSpace(v["Bengali"]) == "" {
			return fmt.Errorf("duplicate or incomplete glossary site at row %d", r.Number)
		}
		keys[key] = true
		i.Glossary = append(i.Glossary, GlossaryRow{Case: w.Case, Row: r.Number, English: v["English"], Site: v["Where it prints"], Bengali: v["Bengali"]})
	}
	if len(keys) == 0 {
		return fmt.Errorf("empty glossary")
	}
	return nil
}
