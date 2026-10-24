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

var nativeProtected = regexp.MustCompile(`https?://\S+|[A-Za-z]*[A-Za-z0-9]*[-_][A-Za-z0-9_-]+|[A-Za-z]+[0-9][A-Za-z0-9]*|[0-9]+(?:\.[0-9]+)?(?: ?(?:g|kg|mg|cm|kcal|ml)\b)?`)
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
		var originals, masked []string
		var tokens [][]string
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
			var protected []string
			masked = append(masked, nativeProtected.ReplaceAllStringFunc(text, func(token string) string {
				protected = append(protected, token)
				return fmt.Sprintf("ZXQ%dQXZ", len(protected)-1)
			}))
			tokens = append(tokens, protected)
		}
		translations := make([]string, len(originals))
		translateBatch := func(start int) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			end := min(start+translateBatchSize, len(masked))
			out, err := drafter.TranslateTexts(ctx, aidraft.TranslateRequest{TargetLanguage: target, Texts: masked[start:end]})
			if err != nil {
				return fmt.Errorf("book%d dynamic localization: %w", bi+1, err)
			}
			if len(out.Texts) != end-start {
				return fmt.Errorf("book%d dynamic localization returned %d of %d values", bi+1, len(out.Texts), end-start)
			}
			for j, text := range out.Texts {
				idx := start + j
				if strings.TrimSpace(text) == "" {
					return fmt.Errorf("book%d dynamic localization returned blank content", bi+1)
				}
				for _, char := range nativeMarker.ReplaceAllString(text, "") {
					if unicode.IsDigit(char) {
						return fmt.Errorf("book%d dynamic localization introduced a numeric value", bi+1)
					}
				}
				for k, token := range tokens[idx] {
					marker := fmt.Sprintf("ZXQ%dQXZ", k)
					if strings.Count(text, marker) != 1 {
						return fmt.Errorf("book%d dynamic localization changed a protected value", bi+1)
					}
					text = strings.Replace(text, marker, token, 1)
				}
				if strings.Contains(text, "ZXQ") {
					return fmt.Errorf("book%d dynamic localization returned an unknown protected value", bi+1)
				}
				translations[idx] = text
			}
			return nil
		}
		if len(masked) == 0 {
			continue
		}
		firstErr := translateBatch(0)
		if firstErr != nil && !errors.Is(firstErr, aidraft.ErrDraftingUnavailable) {
			return firstErr
		}
		unavailable := errors.Is(firstErr, aidraft.ErrDraftingUnavailable)
		if !unavailable {
			remaining := (len(masked) - 1) / translateBatchSize
			errs := make([]error, remaining)
			draftConcurrently(ctx, remaining, func(_ context.Context, i int) {
				errs[i] = translateBatch((i + 1) * translateBatchSize)
			})
			for _, err := range errs {
				if errors.Is(err, aidraft.ErrDraftingUnavailable) {
					unavailable = true
				} else if err != nil {
					return err
				}
			}
		}
		if unavailable {
			n.Omissions[bi] = append(n.Omissions[bi], "Dynamic localization into "+target+" is unavailable. Supplied template wording is preserved in "+target+"; inserted source text remains in its recorded language.")
			continue
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
	return !strings.ContainsAny(text, " \t\n") && (nativeProtected.MatchString(text) || strings.Contains(text, "/"))
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
