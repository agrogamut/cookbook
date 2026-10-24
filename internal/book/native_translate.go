package book

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/bookdocx"
)

// Protect coded identities and numeric facts, never a word merely because it has a hyphen.
var nativeProtected = regexp.MustCompile(`https?://\S+|` +
	`\b[A-Za-z0-9][A-Za-z0-9_.-]*\.(?i:xlsx|docx|zip|pdf)\b|` +
	`\b[A-Za-z][A-Za-z0-9]*(?:_[A-Za-z0-9]+)+\b|` +
	`\b(?:ALG|MG|IAP|WHO|GENERIC)-[A-Z0-9_-]+\b|_{2,}|` +
	`\b[[:xdigit:]]{64}\b|\b[[:xdigit:]]{8}(?:-[[:xdigit:]]{4}){3}-[[:xdigit:]]{12}\b|` +
	`\b[A-Z][A-Z_-]*[0-9][A-Z0-9_-]*\b|\b[Bb]ook[12]\b|\b[vV][0-9]+(?:\.[0-9]+)*\b|` +
	`\b[0-9]{4}-[0-9]{2}-[0-9]{2}\b|` +
	`[-+]?(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)(?:[ \t]*(?:(?:g|kg|mg|µg|μg|mcg|cm|mm|mL|ml|L|kcal|kJ|min|h|mo|IU)\b|%))?`)
var nativeMarker = regexp.MustCompile(`ZXQ[0-9]+QXZ`)

// LocalizeNative translates only inserted content; the six fixed masters never enter this path.
func LocalizeNative(ctx context.Context, n *NativeSet, drafter aidraft.Drafter) error {
	// Modules can share source blocks before localization. Each output gets private values.
	encoded, err := json.Marshal(n.Books)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(encoded, &n.Books); err != nil {
		return err
	}
	for bi := range n.Books {
		req := &n.Books[bi]
		target, translate := targetLanguageName[req.Language]
		if !translate {
			continue
		}
		var cells []*string
		keys := make([]string, 0, len(req.Modules))
		for k := range req.Modules {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var values []bookdocx.Module
		for _, key := range keys {
			m := req.Modules[key]
			values = append(values, m)
		}
		for i := range values {
			m := &values[i]
			if m.Gap != "" {
				cells = append(cells, &m.Gap)
			}
			cells = append(cells, nativeTextCells(m.Blocks)...)
			for j := range m.Entries {
				cells = append(cells, nativeTextCells(m.Entries[j].Blocks)...)
			}
		}
		unique := map[string]int{}
		var originals []string
		for _, cell := range cells {
			text := *cell
			if strings.TrimSpace(text) == "" || nativeVerbatim(text) {
				continue
			}
			if _, ok := unique[text]; ok {
				continue
			}
			unique[text] = len(originals)
			originals = append(originals, text)
		}
		translations, err := translateProtectedTexts(ctx, originals, target, drafter)
		if errors.Is(err, aidraft.ErrDraftingUnavailable) {
			n.Omissions[bi] = append(n.Omissions[bi], "Dynamic localization into "+target+" is unavailable. Supplied template wording is preserved in "+target+"; inserted source text remains in its recorded language.")
			continue
		}
		if err != nil {
			return fmt.Errorf("book%d dynamic localization: %w", bi+1, err)
		}
		for _, cell := range cells {
			if idx, ok := unique[*cell]; ok {
				*cell = translations[idx]
			}
		}
		for i, key := range keys {
			req.Modules[key] = values[i]
		}
	}
	return nil
}

func translateProtectedTexts(ctx context.Context, originals []string, target string, drafter aidraft.Drafter) ([]string, error) {
	if len(originals) == 0 {
		return nil, nil
	}
	masked := make([]string, len(originals))
	tokens := make([][]string, len(originals))
	for i, text := range originals {
		masked[i] = nativeProtected.ReplaceAllStringFunc(text, func(token string) string {
			tokens[i] = append(tokens[i], token)
			return fmt.Sprintf("ZXQ%dQXZ", len(tokens[i])-1)
		})
	}
	translations := make([]string, len(originals))
	translateBatch := func(start int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(start+translateBatchSize, len(masked))
		out, err := drafter.TranslateTexts(ctx, aidraft.TranslateRequest{TargetLanguage: target, Texts: masked[start:end]})
		if err != nil {
			return err
		}
		if len(out.Texts) != end-start {
			return fmt.Errorf("returned %d of %d values", len(out.Texts), end-start)
		}
		for j, text := range out.Texts {
			idx := start + j
			if strings.TrimSpace(text) == "" {
				return fmt.Errorf("returned blank content")
			}
			for _, char := range nativeMarker.ReplaceAllString(text, "") {
				if unicode.IsDigit(char) {
					return fmt.Errorf("introduced a numeric value")
				}
			}
			for k, token := range tokens[idx] {
				marker := fmt.Sprintf("ZXQ%dQXZ", k)
				if strings.Count(text, marker) != 1 {
					return fmt.Errorf("changed a protected value")
				}
				text = strings.Replace(text, marker, token, 1)
			}
			if strings.Contains(text, "ZXQ") {
				return fmt.Errorf("returned an unknown protected value")
			}
			translations[idx] = text
		}
		return nil
	}
	if err := translateBatch(0); err != nil {
		return nil, err
	}
	remaining := (len(masked) - 1) / translateBatchSize
	errs := make([]error, remaining)
	draftConcurrently(ctx, remaining, func(_ context.Context, i int) { errs[i] = translateBatch((i + 1) * translateBatchSize) })
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return translations, nil
}

func nativeVerbatim(text string) bool {
	if label, _, ok := strings.Cut(text, ":"); ok && !strings.Contains(label, " ") && strings.Contains(label, "_") {
		return true
	}
	if strings.HasPrefix(text, "Source:") || strings.HasPrefix(text, "Allergen_Tags:") || strings.HasPrefix(text, "Clinical_Tag:") {
		return true
	}
	if strings.Contains(text, "Source review status") || strings.Contains(text, "Draft") || strings.Contains(text, "Provisional") || strings.Contains(text, "PASS") || strings.Contains(text, "BLOCKED") || strings.Contains(text, "NOT SELECTABLE") {
		return true
	}
	return !strings.ContainsAny(text, " \t\n") && (nativeProtected.FindString(text) == text || strings.Contains(text, "/"))
}

func nativeTextCells(blocks []bookdocx.Block) []*string {
	var cells []*string
	for i := range blocks {
		if p := blocks[i].Paragraph; p != nil {
			for j := range p.Runs {
				cells = append(cells, &p.Runs[j].Text)
			}
		}
		if t := blocks[i].Table; t != nil {
			for j := range t.Rows {
				// Source status and identity values remain verbatim beside their labels.
				verbatim := len(t.Rows[j]) == 2 && (strings.Contains(t.Rows[j][0], "status") || strings.Contains(t.Rows[j][0], "Status") || strings.Contains(t.Rows[j][0], "Quality") || strings.Contains(t.Rows[j][0], "SHA256") || t.Rows[j][0] == "Name" || t.Rows[j][0] == "Workbook" || t.Rows[j][0] == "Source")
				for k := range t.Rows[j] {
					if verbatim && k == 1 {
						continue
					}
					if len(t.Rows[j]) == 4 && k == 3 && strings.Contains(t.Rows[j][2], "_") {
						continue
					}
					cells = append(cells, &t.Rows[j][k])
				}
			}
		}
	}
	return cells
}
