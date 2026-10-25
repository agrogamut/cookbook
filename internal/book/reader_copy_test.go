package book

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
	"testing"
	"time"

	"github.com/madamgy/recipie/internal/bookdocx"
	"golang.org/x/net/html"
)

func readerTestLocale(t *testing.T, language string) map[string]string {
	t.Helper()
	values, err := readerLocale(language)
	if err != nil {
		t.Fatal(err)
	}
	return values
}

func TestReaderCopyPinnedCatalog(t *testing.T) {
	if got := fmt.Sprintf("%x", sha256.Sum256(readerCopyJSON)); got != "4e08619181e84eeb21fd8ef40343890b6300c455f093deb0a645e13278d72659" {
		t.Fatalf("catalog differs from audited input: %s", got)
	}
	if _, err := parseReaderCopy(readerCopyJSON); err != nil {
		t.Fatal(err)
	}
	if _, err := readerLocale("fr"); err == nil {
		t.Fatal("unknown language accepted")
	}
	if _, err := readerCopyText(readerTestLocale(t, "en"), "unknown"); err == nil {
		t.Fatal("unknown key accepted")
	}
}

func TestReaderCopyRejectsDamagedCatalog(t *testing.T) {
	for _, mutation := range []string{"key", "locale", "english", "slot", "marker", "identifier", "number", "blank"} {
		t.Run(mutation, func(t *testing.T) {
			var data map[string]any
			if err := json.Unmarshal(readerCopyJSON, &data); err != nil {
				t.Fatal(err)
			}
			items := data["items"].(map[string]any)
			value := func(key, lang string) map[string]any {
				return items[key].(map[string]any)["values"].(map[string]any)[lang].(map[string]any)
			}
			switch mutation {
			case "key":
				delete(items, "about_copy")
			case "locale":
				delete(items["about_copy"].(map[string]any)["values"].(map[string]any), "hi")
			case "english":
				value("about_copy", "en")["text"] = "changed source"
			case "slot":
				value("age_years_months", "bn")["text"] = "COPYVAL0 COPYVAL0"
			case "marker":
				value("about_copy", "hi")["text"] = "COPYREF0"
			case "identifier":
				v := value("recipe_imprint_summary", "bn")
				v["text"] = strings.ReplaceAll(v["text"].(string), "IFCT", "")
			case "number":
				v := value("recipe_imprint_summary", "hi")
				v["text"] = strings.ReplaceAll(v["text"].(string), "2017", "2018")
			case "blank":
				value("about_copy", "bn")["text"] = " "
			}
			encoded, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseReaderCopy(encoded); err == nil {
				t.Fatal("damaged catalog accepted")
			}
		})
	}
}

func TestReaderCopyExactSlotsAndProtectedValues(t *testing.T) {
	for _, language := range []string{"en", "bn", "hi"} {
		t.Run(language, func(t *testing.T) {
			values := readerTestLocale(t, language)
			for _, tc := range []struct{ input, key, first, second string }{
				{"007 months", "age_months", "007", ""},
				{"04 years", "age_years", "04", ""},
				{"04 years 09 months", "age_years_months", "04", "09"},
			} {
				want := strings.NewReplacer("COPYVAL0", tc.first, "COPYVAL1", tc.second).Replace(values[tc.key])
				if got := readerCopyAge(tc.input, values); got != want {
					t.Fatalf("age %q: %q want %q", tc.input, got, want)
				}
			}
			for _, value := range []string{"19 মাস", "19 महीने", "raw 0.005 g", "24 months 3 months", "recorded-value"} {
				if got := readerCopyAge(value, values); got != value {
					t.Fatalf("nonmatching value changed: %q", got)
				}
			}
			if readerCopyTitle("Original supplied title", values) != "Original supplied title" {
				t.Fatal("unknown title changed")
			}
		})
	}
}

func readerVisibleText(t *testing.T, source string) string {
	t.Helper()
	node, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "style" || n.Data == "script") {
			return
		}
		if n.Type == html.TextNode {
			texts = append(texts, n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(node)
	return strings.Join(strings.Fields(strings.Join(texts, " ")), " ")
}

func TestReaderCopyAllPresentationSites(t *testing.T) {
	for _, language := range []string{"en", "bn", "hi"} {
		for _, book := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/%d", language, book), func(t *testing.T) {
				values := readerTestLocale(t, language)
				meta := Metadata{Language: language, BookVersion: "V-0007", ReleaseID: "R-0008", GenerationDate: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)}
				child := ChildSummary{DisplayName: "<source & identity>", AgeLabel: "04 years 09 months", DateOfBirth: "2021-01-24"}
				b1, b2 := &Book1{Metadata: meta, Child: child}, &Book2{Metadata: meta, Child: child, MealSections: []MealSection{{Recipes: []RecipeCard{{RecipeID: "R-001", RecipeVersion: "SOURCE-0009"}}}}}
				b1.Metadata.Title = readerCatalog.Items["growth_metadata_title"].Canonical
				b2.Metadata.Title = readerCatalog.Items["recipe_metadata_title"].Canonical
				d := readerFixture(t, book, language)
				if book == 1 {
					for i := range d.Nodes {
						if m := d.Nodes[i].Module; m != nil && m.Code == "B1-50" {
							m.Content = bookdocx.Module{Blocks: []bookdocx.Block{
								{Paragraph: &bookdocx.Paragraph{Runs: []bookdocx.Run{{Text: "IF-003 exact context"}}}},
								{Table: &bookdocx.Table{Rows: [][]string{{"What to do source 0.005 g", "Watch for source 007", "Warning: seek medical care source", "scope unchanged"}}}},
							}}
						}
					}
				}
				var output bytes.Buffer
				if err := RenderReaderHTML(&output, d, ReaderOptions{Book1: b1, Book2: b2}); err != nil {
					t.Fatal(err)
				}
				text := readerVisibleText(t, output.String())
				for _, path := range []string{fmt.Sprintf("templates/book%d/cover.html", book), fmt.Sprintf("templates/book%d/end.html", book)} {
					for _, key := range readerTemplateCopyKeys[path] {
						if !strings.Contains(text, values[key]) {
							t.Fatalf("missing %s: %q", key, values[key])
						}
					}
				}
				titleKey := "growth_metadata_title"
				if book == 2 {
					titleKey = "recipe_metadata_title"
				}
				if !strings.Contains(text, values[titleKey]) {
					t.Fatal("localized title missing")
				}
				if book == 1 {
					for _, key := range []string{"illness_action", "illness_monitor", "illness_warning"} {
						if !strings.Contains(text, values[key]) {
							t.Fatalf("missing %s", key)
						}
					}
					for _, raw := range []string{"What to do source 0.005 g", "Watch for source 007", "Warning: seek medical care source"} {
						if !strings.Contains(text, raw) {
							t.Fatalf("source changed: %s", raw)
						}
					}
				}
				footer, err := readerFooter(meta)
				if err != nil {
					t.Fatal(err)
				}
				for _, s := range []string{values["footer_release"], values["footer_generated"], "R-0008", "2026-10-24", "font-size:10pt"} {
					if !strings.Contains(footer, s) {
						t.Fatalf("footer missing %q", s)
					}
				}
				if b1.Child != child || b2.Child != child || b1.Metadata.Title != readerCatalog.Items["growth_metadata_title"].Canonical || b2.Metadata.Title != readerCatalog.Items["recipe_metadata_title"].Canonical {
					t.Fatal("caller metadata mutated")
				}
				if strings.Contains(output.String(), "<source & identity>") || strings.Contains(output.String(), "COPYVAL") {
					t.Fatal("unescaped identity or unresolved slot")
				}
			})
		}
	}
}

func TestReaderCopyTemplateAnchorAndEscaping(t *testing.T) {
	for path := range readerTemplateCopyKeys {
		source, err := templateFS.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := readerCopyTemplate(path, source); err != nil {
			t.Fatal(err)
		}
		if _, err := readerCopyTemplate(path, []byte("changed template")); err == nil {
			t.Fatalf("changed template accepted: %s", path)
		}
	}
	values := readerTestLocale(t, "bn")
	values["about_copy"] = `<script>bad & text</script>`
	tmpl, err := template.New("copy").Funcs(template.FuncMap{"readerCopy": func(key string) (string, error) { return readerCopyText(values, key) }}).Parse(`<p>{{ readerCopy "about_copy" }}</p>`)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, nil); err != nil {
		t.Fatal(err)
	}
	if out.String() != `<p>&lt;script&gt;bad &amp; text&lt;/script&gt;</p>` {
		t.Fatalf("unsafe copy: %s", out.String())
	}
	if readerTestLocale(t, "bn")["about_copy"] == values["about_copy"] {
		t.Fatal("locale map mutation escaped")
	}
}
