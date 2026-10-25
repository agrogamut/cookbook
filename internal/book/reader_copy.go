package book

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"slices"
	"strings"
)

//go:embed reader_copy.json
var readerCopyJSON []byte

type readerCopyEntry struct {
	Canonical string `json:"canonical_en"`
	Slots     []struct {
		Token   string `json:"token"`
		Kind    string `json:"kind"`
		Restore string `json:"restore"`
	} `json:"slots"`
	Values map[string]struct {
		Text string `json:"text"`
	} `json:"values"`
}

type readerCopyCatalog struct {
	Version int                        `json:"version"`
	Status  string                     `json:"status"`
	Items   map[string]readerCopyEntry `json:"items"`
}

var readerCopyKeys = strings.Fields(`growth_kicker growth_cover_summary date_of_birth age_at_generation section_count_label section_count_unit recipe_kicker recipes_for recipe_cover_summary food_practice not_recorded recipe_count_label about_copy version_generation book_version release_id generation_date sources_version recipe_imprint_summary book_label generated_label release_label recipe_data_version footer_release footer_generated illness_action illness_monitor illness_warning growth_metadata_title recipe_metadata_title age_months age_years age_years_months`)

var readerCatalog, readerCatalogErr = parseReaderCopy(readerCopyJSON)
var readerCopyToken = regexp.MustCompile(`COPY(?:VAL|REF)[0-9]+|ZXQ[0-9]+QXZ`)
var readerCopyNumber = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)
var readerAgePattern = regexp.MustCompile(`^([0-9]+) (months|years)(?: ([0-9]+) months)?$`)
var readerTemplateComment = regexp.MustCompile(`(?s){{/\*.*?\*/}}`)
var readerTemplateText = regexp.MustCompile(`>([^<>]*)<`)

func parseReaderCopy(data []byte) (readerCopyCatalog, error) {
	var catalog readerCopyCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return catalog, fmt.Errorf("reader copy JSON: %w", err)
	}
	if catalog.Version != 2 || catalog.Status != "complete" || len(catalog.Items) != len(readerCopyKeys) {
		return catalog, fmt.Errorf("reader copy catalog is incomplete or unsupported")
	}
	for _, key := range readerCopyKeys {
		entry, ok := catalog.Items[key]
		if !ok || entry.Canonical == "" || len(entry.Values) != 3 {
			return catalog, fmt.Errorf("reader copy missing key or locales: %s", key)
		}
		english := entry.Canonical
		for _, slot := range entry.Slots {
			if slot.Kind == "runtime_integer" {
				english = strings.Replace(english, "%d", slot.Token, 1)
			}
		}
		if entry.Values["en"].Text != english {
			return catalog, fmt.Errorf("reader copy changed English source: %s", key)
		}
		numbers := readerCopyNumber.FindAllString(readerCopyToken.ReplaceAllString(english, ""), -1)
		slices.Sort(numbers)
		for _, language := range []string{"en", "bn", "hi"} {
			value := entry.Values[language].Text
			if strings.TrimSpace(value) == "" {
				return catalog, fmt.Errorf("reader copy missing %s/%s", language, key)
			}
			remaining := value
			for _, slot := range entry.Slots {
				switch slot.Kind {
				case "runtime_integer":
					if (slot.Token != "COPYVAL0" && slot.Token != "COPYVAL1") || strings.Count(value, slot.Token) != 1 {
						return catalog, fmt.Errorf("reader copy invalid slot %s/%s/%s", language, key, slot.Token)
					}
					remaining = strings.ReplaceAll(remaining, slot.Token, "")
				case "fixed_source_identifier":
					if slot.Restore == "" || strings.Count(value, slot.Restore) != strings.Count(entry.Canonical, slot.Restore) {
						return catalog, fmt.Errorf("reader copy lost identifier %s/%s", language, key)
					}
				default:
					return catalog, fmt.Errorf("reader copy unknown slot kind: %s", slot.Kind)
				}
			}
			if readerCopyToken.MatchString(remaining) {
				return catalog, fmt.Errorf("reader copy unresolved marker %s/%s", language, key)
			}
			gotNumbers := readerCopyNumber.FindAllString(remaining, -1)
			slices.Sort(gotNumbers)
			if !slices.Equal(numbers, gotNumbers) {
				return catalog, fmt.Errorf("reader copy changed number %s/%s", language, key)
			}
		}
	}
	return catalog, nil
}

func readerLocale(language string) (map[string]string, error) {
	if readerCatalogErr != nil {
		return nil, readerCatalogErr
	}
	if language != "en" && language != "bn" && language != "hi" {
		return nil, fmt.Errorf("reader copy unsupported language: %s", language)
	}
	values := make(map[string]string, len(readerCatalog.Items))
	for key, entry := range readerCatalog.Items {
		values[key] = entry.Values[language].Text
	}
	return values, nil
}

func readerCopyText(values map[string]string, key string) (string, error) {
	value, ok := values[key]
	if !ok || value == "" {
		return "", fmt.Errorf("reader copy unknown key: %s", key)
	}
	return value, nil
}

func readerCopyAge(value string, values map[string]string) string {
	matches := readerAgePattern.FindStringSubmatch(value)
	if matches == nil || (matches[2] == "months" && matches[3] != "") {
		return value
	}
	key := "age_" + matches[2]
	if matches[3] != "" {
		key = "age_years_months"
	}
	return strings.NewReplacer("COPYVAL0", matches[1], "COPYVAL1", matches[3]).Replace(values[key])
}

func readerCopyTitle(value string, values map[string]string) string {
	for _, key := range []string{"growth_metadata_title", "recipe_metadata_title"} {
		if value == readerCatalog.Items[key].Canonical {
			return values[key]
		}
	}
	return value
}

var readerTemplateCopyKeys = map[string][]string{
	"templates/book1/cover.html": strings.Fields("growth_kicker growth_cover_summary date_of_birth age_at_generation section_count_label section_count_unit"),
	"templates/book1/end.html":   strings.Fields("about_copy version_generation book_version release_id generation_date"),
	"templates/book2/cover.html": strings.Fields("recipe_kicker recipes_for recipe_cover_summary age_at_generation food_practice not_recorded recipe_count_label"),
	"templates/book2/end.html":   strings.Fields("about_copy sources_version recipe_imprint_summary book_label book_version generated_label release_label recipe_data_version"),
}

func readerCopyTemplate(path string, source []byte) (string, error) {
	keys, ok := readerTemplateCopyKeys[path]
	if !ok {
		return "", fmt.Errorf("reader copy unknown template: %s", path)
	}
	counts := make(map[string]int)
	normalize := func(s string) string { return strings.Join(strings.Fields(html.UnescapeString(s)), " ") }
	// Bind only immutable template text, before any profile or source content is rendered.
	text := readerTemplateComment.ReplaceAllString(string(source), "")
	text = readerTemplateText.ReplaceAllStringFunc(text, func(match string) string {
		inner := match[1 : len(match)-1]
		canonical := normalize(inner)
		for _, key := range keys {
			want := readerCatalog.Items[key].Canonical
			prefix := ""
			switch key {
			case "section_count_label":
				want = "Pages in this book"
			case "section_count_unit":
				want = "{{ .SectionCount }} sections"
				prefix = "{{ .SectionCount }} "
			case "growth_kicker", "recipe_kicker":
				want = "MadamGY · " + want
				prefix = "MadamGY &middot; "
			}
			if canonical == want {
				counts[key]++
				return ">" + prefix + `{{ readerCopy "` + key + `" }}<`
			}
		}
		// The fallback phrase is inside a conditional action, not a separate HTML node.
		if path == "templates/book2/cover.html" && strings.Contains(inner, "{{ else }}not recorded{{ end }}") {
			counts["not_recorded"]++
			return ">" + strings.Replace(inner, "{{ else }}not recorded{{ end }}", `{{ else }}{{ readerCopy "not_recorded" }}{{ end }}`, 1) + "<"
		}
		return match
	})
	for _, key := range keys {
		if counts[key] != 1 {
			return "", fmt.Errorf("reader copy template anchor %s/%s: got %d, want 1", path, key, counts[key])
		}
	}
	return text, nil
}
