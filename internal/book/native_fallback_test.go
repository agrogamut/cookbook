package book

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/bookdocx"
	"github.com/madamgy/recipie/internal/casesource"
	"github.com/madamgy/recipie/internal/profile"
)

func TestNativeFallbackCatalogContract(t *testing.T) {
	c, err := loadNativeFallback()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Entries) != 547 {
		t.Fatal("incomplete accepted response set")
	}
}

func TestNativeFallbackCatalogReviewedExtension(t *testing.T) {
	c, err := loadNativeFallback()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Entries) != 547 {
		t.Fatalf("reviewed response entries=%d, want 547", len(c.Entries))
	}
	owners := 0
	for _, entry := range c.Entries {
		owners += len(entry.Owners)
	}
	if owners != 1206 {
		t.Fatalf("reviewed owner mappings=%d, want 1206", owners)
	}
	var raw struct {
		Entries []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(nativeFallbackData, &raw); err != nil {
		t.Fatal(err)
	}
	var original []string
	for _, entry := range raw.Entries[:525] {
		original = append(original, string(entry))
	}
	if nativeHash(strings.Join(original, "\n")) != "8e351d369eb80d2c023447afc73978636ad5aecdb944aa6970500a86d6dbf1f7" {
		t.Fatal("original complete response entries changed")
	}
}

func TestNativeFallbackReviewedNormalPath(t *testing.T) {
	catalog, err := loadNativeFallback()
	if err != nil {
		t.Fatal(err)
	}
	for _, language := range []string{"bn", "hi"} {
		for _, databaseID := range []string{"fresh-import-one", "fresh-import-two"} {
			t.Run(language+"/"+databaseID, func(t *testing.T) {
				var b Book2
				if err := json.Unmarshal([]byte(nativeFallbackReviewedRecipes), &b); err != nil {
					t.Fatal(err)
				}
				b.Metadata.Language = language
				set := Set{ChildID: databaseID, Book1: Book1{Metadata: Metadata{Language: language}}, Book2: b}
				before := nativeObjectHash(set)
				n := NativeBindings(profile.Stored{ChildID: databaseID, DisplayName: "Fresh 7", RegionCulture: "West Bengal / East India"}, set)
				r := n.fallback.Cards["MG-R-00111"]
				values := [][]string{{r.RecipeID, r.Title, "ingredient_master", "Recorded source group"}}
				blocks := []bookdocx.Block{nativeText("Index context"), nativeTable(append([][]string{{"Original recipe ID", "Recipe", "Source field", "Source value"}}, values...)...)}
				n.Books[1].Modules["B2-16"] = bookdocx.Module{Blocks: blocks}
				n.nativeOwnIndex("B2-16", blocks, values)
				matched := map[int]bool{}
				owners := map[string]bool{}
				wants := map[string]string{}
				// Keep real builder paths and owners, isolating the reviewed cells from unrelated translations.
				for _, cell := range nativeFallbackTestCells(&n) {
					if identity := n.fallback.Bindings[cell.Path].Identity; identity != nil && identity.Passthrough {
						continue
					}
					input, tokens, slots, err := nativeProtect(*cell.Value, nativeProtected, nil)
					if err != nil {
						t.Fatal(err)
					}
					binding := n.fallback.Bindings[cell.Path]
					owner := binding.Owner
					if owner.EnglishSHA256 == "" {
						owner.EnglishSHA256 = nativeHash(input)
					}
					for i := 525; i < len(catalog.Entries); i++ {
						e := catalog.Entries[i]
						if e.Language != language || e.Input != input {
							continue
						}
						for _, a := range e.Owners {
							if a.Owner != owner || nativeObjectHash(a.Slots) != nativeObjectHash(slots) {
								continue
							}
							want, err := nativeRestore(e.Output, tokens)
							if err != nil {
								t.Fatal(err)
							}
							wants[cell.Path], matched[i] = want, true
							owners[nativeObjectHash(a)] = true
						}
					}
					if _, ok := wants[cell.Path]; !ok {
						*cell.Value = ""
					}
				}
				if len(matched) != 11 || len(owners) != 14 {
					t.Fatalf("normal builder matches=%d owners=%d, want11/14; entries=%v", len(matched), len(owners), matched)
				}
				d := &translateFakeDrafter{fail: true}
				if err := LocalizeNative(context.Background(), &n, d); err != nil {
					t.Fatal(err)
				}
				if d.calls != 0 || nativeObjectHash(set) != before {
					t.Fatalf("known responses provider calls=%d source unchanged=%v omissions=%v", d.calls, nativeObjectHash(set) == before, n.Omissions)
				}
				for _, cell := range nativeFallbackTestCells(&n) {
					if want, ok := wants[cell.Path]; ok && *cell.Value != want {
						t.Fatalf("changed accepted response at %s", cell.Path)
					}
				}
			})
		}
	}
}

func nativeFallbackTestCells(n *NativeSet) []nativeCell {
	var cells []nativeCell
	add := func(blocks []bookdocx.Block, prefix string) {
		for i, block := range blocks {
			base := fmt.Sprintf("%s/Blocks/%d", prefix, i)
			if block.Paragraph != nil {
				for j := range block.Paragraph.Runs {
					cells = append(cells, nativeCell{fmt.Sprintf("%s/Paragraph/Runs/%d/Text", base, j), &block.Paragraph.Runs[j].Text})
				}
			}
			if block.Table != nil {
				for j := range block.Table.Rows {
					for k := range block.Table.Rows[j] {
						cells = append(cells, nativeCell{fmt.Sprintf("%s/Table/Rows/%d/%d", base, j, k), &block.Table.Rows[j][k]})
					}
				}
			}
		}
	}
	for bi := range n.Books {
		for key, module := range n.Books[bi].Modules {
			prefix := nativeModulePath(bi+1, key)
			add(module.Blocks, prefix)
			for i := range module.Entries {
				add(module.Entries[i].Blocks, fmt.Sprintf("%s/Entries/%d", prefix, i))
			}
			module.Gap = ""
			n.Books[bi].Modules[key] = module
		}
	}
	return cells
}

func TestNativeFallbackReviewedOwnerAndSlotMisses(t *testing.T) {
	c, err := loadNativeFallback()
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range c.Entries[525:] {
		for ai, a := range e.Owners {
			t.Run(fmt.Sprintf("%d/%d", i, ai), func(t *testing.T) {
				out, hit, err := c.lookup(context.Background(), e.Language, e.Input, a.Slots, a.Owner)
				if err != nil || !hit || out != e.Output {
					t.Fatal("reviewed owner missed")
				}
				for _, field := range []string{"revision", "owner", "input", "unit"} {
					owner, input := a.Owner, e.Input
					slots := append([]nativeFallbackSlot(nil), a.Slots...)
					switch field {
					case "revision":
						owner.Revision += "/different"
					case "owner":
						owner.Field += "/different"
					case "input":
						input += " different"
					case "unit":
						if len(slots) == 0 {
							slots = []nativeFallbackSlot{{Kind: "number", Unit: "kg"}}
						} else {
							slots[0].Unit += "kg"
						}
					}
					if _, hit, err := c.lookup(context.Background(), e.Language, input, slots, owner); err != nil || hit {
						t.Fatalf("%s mismatch reused response", field)
					}
				}
			})
		}
	}
}

func TestNativeFallbackForewordFreshRequests(t *testing.T) {
	for _, language := range []string{"bn", "hi"} {
		for _, name := range []string{"a", "May", "পরীক্ষা ১২", "नाम7"} {
			t.Run(language+"/"+name, func(t *testing.T) {
				s := profile.Stored{DisplayName: name}
				set := Set{ChildID: "fresh-database-child", Book1: Book1{Metadata: Metadata{Language: language}, Letter: letterPage(name)}}
				n := NativeBindings(s, set)
				n.Books[0].Modules = map[string]bookdocx.Module{"B1-02": n.Books[0].Modules["B1-02"]}
				original := strings.Join(set.Book1.Letter.Body, "\n\n")
				path := nativeModulePath(1, "B1-02") + "/Blocks/1/Paragraph/Runs/0/Text"
				binding := n.fallback.Bindings[path]
				input, tokens, slots, err := nativeProtect(original, nativeProtected, binding.Identity)
				if err != nil {
					t.Fatal(err)
				}
				catalog, err := loadNativeFallback()
				if err != nil {
					t.Fatal(err)
				}
				masked, hit, err := catalog.lookup(context.Background(), language, input, slots, binding.Owner)
				if err != nil || !hit {
					t.Fatalf("known foreword hit=%v err=%v", hit, err)
				}
				want, err := nativeRestore(masked, tokens)
				if err != nil {
					t.Fatal(err)
				}
				d := &translateFakeDrafter{fail: true}
				omissions := nativeObjectHash(n.Omissions)
				if err := LocalizeNative(context.Background(), &n, d); err != nil {
					t.Fatal(err)
				}
				got := n.Books[0].Modules["B1-02"].Blocks
				if d.calls != 0 || nativeObjectHash(n.Omissions) != omissions || got[0].Paragraph.Runs[0].Text != name || got[1].Paragraph.Runs[0].Text != want {
					t.Fatal("known foreword changed or reached provider")
				}
				if strings.Join(set.Book1.Letter.Body, "\n\n") != original {
					t.Fatal("source letter mutated")
				}
			})
		}
	}
}

func testNativeCatalog(t *testing.T, language, source, output string, owner nativeFallbackOwner) *nativeFallbackCatalog {
	t.Helper()
	input, _, slots, err := nativeProtect(source, nativeProtected, nil)
	if err != nil {
		t.Fatal(err)
	}
	if owner.EnglishSHA256 == "" {
		owner.EnglishSHA256 = nativeHash(input)
	}
	return &nativeFallbackCatalog{Entries: []nativeFallbackEntry{{Language: language, Input: input, Output: output, Owners: []nativeFallbackAllowance{{Owner: owner, Slots: slots}}}}, byInput: map[string][]int{language + ":" + nativeHash(input): {0}}}
}

func TestNativeFallbackOwnershipBeforeDedupe(t *testing.T) {
	n := NativeBindings(profile.Stored{}, Set{Book1: Book1{Metadata: Metadata{Language: "bn"}}})
	source := n.Books[0].Modules["B1-61"].Gap
	n.Books[0].Modules = map[string]bookdocx.Module{"B1-61": {Gap: source}, "B1-62": {Gap: source}}
	owner := n.fallback.Bindings[nativeModulePath(1, "B1-61")+"/Gap"].Owner
	c := testNativeCatalog(t, "bn", source, "accepted response", owner)
	d := &translateFakeDrafter{transform: func(string) string { return "live response" }}
	if err := localizeNativeWithCatalog(context.Background(), &n, d, c); err != nil {
		t.Fatal(err)
	}
	if n.Books[0].Modules["B1-61"].Gap != "accepted response" || n.Books[0].Modules["B1-62"].Gap != "live response" || d.calls != 1 {
		t.Fatal("same English crossed occurrence ownership")
	}
}

func TestNativeFallbackImmutableSourceAndMismatches(t *testing.T) {
	citation := casesource.Citation{ArchiveSHA256: strings.Repeat("a", 64), MemberSHA256: strings.Repeat("b", 64), RecipeID: "MG-WB-C26-001", Case: 26, Sheet: "Recipe_Master", Row: 5}
	r := RecipeCard{RecipeID: citation.RecipeID, SourceRevision: &citation, SourceCandidateID: casesource.CandidateID(citation.ArchiveSHA256, citation.RecipeID), Title: "Recorded dish", Ingredients: []IngredientLine{{Name: "Recorded ingredient", Quantity: "10 g", SourceID: "ING0070", SourceOrdinal: 8}}, MethodSteps: []string{"Use 10 g."}}
	makeNative := func(databaseID string) NativeSet {
		copy := r
		return NativeBindings(profile.Stored{ChildID: databaseID}, Set{ChildID: databaseID, Book2: Book2{Metadata: Metadata{Language: "hi", ReleaseID: databaseID}, MealSections: []MealSection{{Title: "Breakfast", Recipes: []RecipeCard{copy}}}}})
	}
	first, second := makeNative("database-one"), makeNative("database-two")
	path := nativeModulePath(2, "B2-09") + "/Entries/0/Blocks/5/Paragraph/Runs/0/Text"
	owner := first.fallback.Bindings[path].Owner
	if owner.Source != "case-recipe" || owner.Ordinal != 8 || owner != second.fallback.Bindings[path].Owner {
		t.Fatal("immutable occurrence owner depends on database identity")
	}
	source := first.Books[1].Modules["B2-09"].Entries[0].Blocks[5].Paragraph.Runs[0].Text
	input, tokens, slots, err := nativeProtect(source, nativeProtected, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := testNativeCatalog(t, "hi", source, input, owner)
	if _, ok, err := c.lookup(context.Background(), "hi", input, slots, owner); err != nil || !ok {
		t.Fatal("same imported source missed")
	}
	for name, change := range map[string]func(*nativeFallbackOwner){
		"revision":       func(o *nativeFallbackOwner) { o.Revision = "different" },
		"archive":        func(o *nativeFallbackOwner) { o.ArchiveSHA256 = strings.Repeat("c", 64) },
		"member":         func(o *nativeFallbackOwner) { o.MemberSHA256 = strings.Repeat("d", 64) },
		"recipe":         func(o *nativeFallbackOwner) { o.RecipeID = "MG-WB-C01-001" },
		"field":          func(o *nativeFallbackOwner) { o.Field = "another-field" },
		"ordinal":        func(o *nativeFallbackOwner) { o.Ordinal++ },
		"paired-English": func(o *nativeFallbackOwner) { o.EnglishSHA256 = strings.Repeat("e", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			wrong := owner
			change(&wrong)
			if _, ok, err := c.lookup(context.Background(), "hi", input, slots, wrong); err != nil || ok {
				t.Fatal("source mismatch hit")
			}
		})
	}
	if got, err := nativeRestore(input, tokens); err != nil || got != source {
		t.Fatal("source values changed")
	}
}

func TestNativeFallbackTypedValuesAndUnits(t *testing.T) {
	owner := nativeFallbackOwner{Source: "builder", Revision: nativeFallbackBuilder, Field: "test/recorded-quantity"}
	c := testNativeCatalog(t, "hi", "Recorded 10 g for MG-R-001.", "Recorded ZXQ0QXZ for ZXQ1QXZ.", owner)
	for _, tc := range []struct {
		source string
		hit    bool
	}{{"Recorded 20 g for MG-R-001.", true}, {"Recorded 20 kg for MG-R-001.", false}, {"Recorded 20 g for MG-R-002.", false}, {"Different 20 g for MG-R-001.", false}} {
		input, tokens, slots, err := nativeProtect(tc.source, nativeProtected, nil)
		if err != nil {
			t.Fatal(err)
		}
		out, hit, err := c.lookup(context.Background(), "hi", input, slots, owner)
		if err != nil || hit != tc.hit {
			t.Fatalf("typed contract hit=%v expected=%v err=%v", hit, tc.hit, err)
		}
		if hit {
			got, err := nativeRestore(out, tokens)
			if err != nil || got != tc.source {
				t.Fatal("current numeric value not restored")
			}
		}
	}
}

func TestNativeFallbackSuppliedBengaliPrecedesCatalog(t *testing.T) {
	r := finalLocaleCard()
	n := NativeBindings(profile.Stored{}, Set{Book2: Book2{Metadata: Metadata{Language: "bn"}, MealSections: []MealSection{{Title: "Breakfast", Recipes: []RecipeCard{r}}}}})
	n.Books[1].Modules = map[string]bookdocx.Module{"B2-09": n.Books[1].Modules["B2-09"]}
	before := nativeObjectHash(n.SourceLocales)
	supplied := r.SourceLocale.Texts[3].Text
	var method nativeCell
	for _, cell := range nativeBlockCells(n.Books[1].Modules["B2-09"].Entries[0].Blocks, nativeModulePath(2, "B2-09")+"/Entries/0") {
		if *cell.Value == supplied {
			method = cell
		}
	}
	if method.Value == nil {
		t.Fatal("supplied method missing before localization")
	}
	input, _, slots, err := nativeProtect(supplied, nativeProtectedFor(&n, "bn"), nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := n.fallback.Bindings[method.Path].Owner
	c := &nativeFallbackCatalog{Entries: []nativeFallbackEntry{{Input: input, Output: "unexpected " + input, Owners: []nativeFallbackAllowance{{Owner: owner, Slots: slots}}}}, byInput: map[string][]int{"bn:" + nativeHash(input): {0}}}
	d := &translateFakeDrafter{transform: func(text string) string {
		if strings.Contains(text, supplied) {
			t.Error("supplied Bengali reached provider")
		}
		return strings.ToUpper(text)
	}}
	if err := localizeNativeWithCatalog(context.Background(), &n, d, c); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, cell := range nativeBlockCells(n.Books[1].Modules["B2-09"].Entries[0].Blocks, nativeModulePath(2, "B2-09")+"/Entries/0") {
		if cell.Path == method.Path {
			found = *cell.Value == supplied
		}
	}
	if !found || nativeObjectHash(n.SourceLocales) != before || d.calls == 0 {
		t.Fatal("supplied field precedence or unknown fallback changed")
	}
}

func TestNativeFallbackCatalogCorruption(t *testing.T) {
	if _, err := parseNativeFallback(append(append([]byte(nil), nativeFallbackData...), 32), nativeFallbackDataSHA); err == nil {
		t.Fatal("file corruption accepted")
	}
	for name, change := range map[string]func(*nativeFallbackCatalog){
		"version":               func(c *nativeFallbackCatalog) { c.Version++ },
		"incomplete":            func(c *nativeFallbackCatalog) { c.Entries = c.Entries[:524] },
		"source-audit":          func(c *nativeFallbackCatalog) { c.AuditSHA256 = strings.Repeat("a", 64) },
		"supplement-audit":      func(c *nativeFallbackCatalog) { c.Supplement.AuditSHA256 = strings.Repeat("a", 64) },
		"supplement-manifest":   func(c *nativeFallbackCatalog) { c.Supplement.ManifestSHA256 = strings.Repeat("a", 64) },
		"supplement-candidates": func(c *nativeFallbackCatalog) { c.Supplement.CandidatesSHA256 = strings.Repeat("a", 64) },
		"supplement-range":      func(c *nativeFallbackCatalog) { c.Supplement.FirstEntry-- },
		"supplement-count":      func(c *nativeFallbackCatalog) { c.Supplement.EntryCount-- },
		"conflicting-supplement-owner": func(c *nativeFallbackCatalog) {
			c.Entries[525] = c.Entries[0]
			c.Entries[525].Output = "changed " + c.Entries[0].Output
			c.Entries[525].OutputSHA256 = nativeHash(c.Entries[525].Output)
		},
		"output-hash": func(c *nativeFallbackCatalog) { c.Entries[0].Output += " changed" },
		"blank":       func(c *nativeFallbackCatalog) { c.Entries[0].Output = ""; c.Entries[0].OutputSHA256 = nativeHash("") },
		"unknown-slot": func(c *nativeFallbackCatalog) {
			c.Entries[0].Output += " ZXQ999QXZ"
			c.Entries[0].OutputSHA256 = nativeHash(c.Entries[0].Output)
		},
		"duplicate-owner": func(c *nativeFallbackCatalog) {
			c.Entries[0].Owners = append(c.Entries[0].Owners, c.Entries[0].Owners[0])
		},
		"missing-owner": func(c *nativeFallbackCatalog) { c.Entries[0].Owners = nil },
		"invalid-owner": func(c *nativeFallbackCatalog) { c.Entries[0].Owners[0].Owner.Source = "" },
		"slot-contract": func(c *nativeFallbackCatalog) { c.Entries[0].Owners[0].Slots = nil },
		"slot-kind":     func(c *nativeFallbackCatalog) { c.Entries[0].Owners[0].Slots[0].Kind = "untyped" },
		"unit": func(c *nativeFallbackCatalog) {
			c.Entries[0].Owners[0].Slots[0] = nativeFallbackSlot{Kind: "number", Unit: "invalid"}
		},
		"identity-site": func(c *nativeFallbackCatalog) { c.Entries[0].Owners[0].Slots[0] = nativeFallbackSlot{Kind: "identity"} },
	} {
		t.Run(name, func(t *testing.T) {
			var c nativeFallbackCatalog
			if err := json.Unmarshal(nativeFallbackData, &c); err != nil {
				t.Fatal(err)
			}
			change(&c)
			data, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseNativeFallback(data, nativeHash(string(data))); err == nil {
				t.Fatal("catalog corruption accepted")
			}
		})
	}
}

type nativeFailingTranslation struct {
	fakeDrafter
	calls  int
	cancel context.CancelFunc
}

func (d *nativeFailingTranslation) TranslateTexts(_ context.Context, r aidraft.TranslateRequest) (aidraft.TranslatedTexts, error) {
	d.calls++
	if d.cancel != nil {
		d.cancel()
	}
	if d.calls == 2 {
		return aidraft.TranslatedTexts{}, errors.New("translation failed")
	}
	texts := make([]string, len(r.Texts))
	for i, s := range r.Texts {
		texts[i] = "translated " + s
	}
	return aidraft.TranslatedTexts{Texts: texts}, nil
}

func TestNativeFallbackCancellationAndAtomicity(t *testing.T) {
	c, err := loadNativeFallback()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := c.lookup(ctx, c.Entries[0].Language, c.Entries[0].Input, c.Entries[0].Owners[0].Slots, c.Entries[0].Owners[0].Owner); !errors.Is(err, context.Canceled) {
		t.Fatal("catalog ignored cancellation")
	}
	makeNative := func() NativeSet {
		return NativeSet{Books: [2]bookdocx.Request{{Book: 1, Language: "bn", Modules: map[string]bookdocx.Module{"B1-01": {Blocks: []bookdocx.Block{nativeText("First source sentence.")}}}}, {Book: 2, Language: "hi", Modules: map[string]bookdocx.Module{"B2-01": {Blocks: []bookdocx.Block{nativeText("Second source sentence.")}}}}}}
	}
	n := makeNative()
	before := nativeObjectHash(n.Books)
	d := &nativeFailingTranslation{}
	if err := LocalizeNative(context.Background(), &n, d); err == nil || d.calls != 2 || nativeObjectHash(n.Books) != before {
		t.Fatal("failed second book changed caller state")
	}
	n = makeNative()
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	d = &nativeFailingTranslation{cancel: cancel}
	if err := LocalizeNative(ctx, &n, d); !errors.Is(err, context.Canceled) || nativeObjectHash(n.Books) != before {
		t.Fatal("live cancellation changed caller state")
	}
}

func TestNativeFallbackReservedIdentityCollision(t *testing.T) {
	for _, source := range []string{"Dear ZXQ0QXZ's family,", "Ordinary ZXQ999QXZ source"} {
		if _, _, _, err := nativeProtect(source, nativeProtected, nil); err == nil {
			t.Fatal("reserved marker accepted")
		}
	}
	if _, _, _, err := nativeProtect("Dear May's family,", nativeProtected, &nativeIdentitySpan{4, 8, "May", false}); err == nil {
		t.Fatal("invalid builder span accepted")
	}
	n := NativeBindings(profile.Stored{DisplayName: "May"}, Set{Book1: Book1{Metadata: Metadata{Language: "bn"}, Letter: letterPage("May")}})
	m := n.Books[0].Modules["B1-02"]
	m.Blocks[0] = nativeText("different identity")
	n.Books[0].Modules = map[string]bookdocx.Module{"B1-02": m}
	d := &translateFakeDrafter{transform: upper}
	if err := LocalizeNative(context.Background(), &n, d); err == nil || d.calls != 0 {
		t.Fatal("identity mutation reached provider")
	}
}

func TestNativeFallbackConcurrentRequests(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("পরীক্ষা %d", i)
			n := NativeBindings(profile.Stored{DisplayName: name}, Set{Book1: Book1{Metadata: Metadata{Language: "bn"}, Letter: letterPage(name)}})
			n.Books[0].Modules = map[string]bookdocx.Module{"B1-02": n.Books[0].Modules["B1-02"]}
			d := &translateFakeDrafter{fail: true}
			if err := LocalizeNative(context.Background(), &n, d); err != nil || d.calls != 0 {
				t.Errorf("concurrent catalog lookup error=%v calls=%d", err, d.calls)
			}
			if n.Books[0].Modules["B1-02"].Blocks[0].Paragraph.Runs[0].Text != name {
				t.Error("identity crossed requests")
			}
		}(i)
	}
	wg.Wait()
}

// Nonpersonal recipe fields from the reviewed actual-input diagnostic; no saved bindings.
const nativeFallbackReviewedRecipes = `{"meal_sections":[{"title":"Breakfast","recipes":[{"recipe_id":"MG-R-00136","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"West Bengal Wheat flour (atta) & Masoor dal Soft khichdi","ingredients":[{"name":"Wheat flour (atta)","bengali_name":"গম","quantity":"25 g"},{"name":"Masoor dal","bengali_name":"মসুর","quantity":"25 g"},{"name":"Onion","bengali_name":"পেঁয়াজ","quantity":"30 g"},{"name":"Groundnut oil","bengali_name":"চিনাবাদাম তেল","quantity":"3 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"124 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":8,"region_culture":"West Bengal / East India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00064","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"West Bengal Semolina (suji) & Peanut Soft protein rice","ingredients":[{"name":"Semolina (suji)","bengali_name":"সুজি","quantity":"15 g"},{"name":"Peanut","bengali_name":"চিনাবাদাম","quantity":"25 g"},{"name":"Brinjal","bengali_name":"বেগুন","quantity":"30 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"105 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":9,"region_culture":"West Bengal / East India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00577","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"South India Ragi & Chickpea Soft protein rice","ingredients":[{"name":"Ragi","bengali_name":"রাগি","quantity":"30 g"},{"name":"Chickpea","bengali_name":"ছোলা","quantity":"20 g"},{"name":"Raw banana","bengali_name":"কাঁচকলা","quantity":"30 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"120 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":10,"region_culture":"South India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00590","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"South India Foxtail millet & Moong dal Soft khichdi","ingredients":[{"name":"Foxtail millet","bengali_name":"BN local: Foxtail millet","quantity":"20 g"},{"name":"Moong dal","bengali_name":"মুগ","quantity":"25 g"},{"name":"Raw banana","bengali_name":"কাঁচকলা","quantity":"30 g"},{"name":"Groundnut oil","bengali_name":"চিনাবাদাম তেল","quantity":"3 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"117 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":11,"region_culture":"South India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00708","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"Central Semolina (suji) & Green peas Soft finger bites","ingredients":[{"name":"Semolina (suji)","bengali_name":"সুজি","quantity":"30 g"},{"name":"Green peas","bengali_name":"মটরশুঁটি","quantity":"20 g"},{"name":"Moong dal","bengali_name":"মুগ","quantity":"25 g"}],"method_steps":["1) Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age. 2) Cook components with about 120 mL liquid until soft. 3) Mash and combine. 4) Shape into soft age-safe pieces and cook gently until set without creating a hard crust. 4) Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"112 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":12,"region_culture":"Central / Tribal India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00730","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"Central Flattened rice (poha & Masoor dal Soft protein rice","ingredients":[{"name":"Flattened rice (poha","bengali_name":"চিঁড়া","quantity":"30 g"},{"name":"Masoor dal","bengali_name":"মসুর","quantity":"20 g"},{"name":"Spinach","bengali_name":"পালং শাক","quantity":"40 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"135 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":13,"region_culture":"Central / Tribal India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00753","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"Central Ragi & Carrot Soft finger bites","ingredients":[{"name":"Ragi","bengali_name":"রাগি","quantity":"15 g"},{"name":"Carrot","bengali_name":"গাজর","quantity":"30 g"},{"name":"Soybean","bengali_name":"সয়াবিন","quantity":"25 g"}],"method_steps":["1) Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age. 2) Cook components with about 120 mL liquid until soft. 3) Mash and combine. 4) Shape into soft age-safe pieces and cook gently until set without creating a hard crust. 4) Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"105 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":14,"region_culture":"Central / Tribal India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00665","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"Northeast India Ragi & Brinjal Savory porridge","ingredients":[{"name":"Ragi","bengali_name":"রাগি","quantity":"30 g"},{"name":"Brinjal","bengali_name":"বেগুন","quantity":"20 g"},{"name":"Sesame oil","bengali_name":"তিলের তেল","quantity":"3 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"80 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":15,"region_culture":"Northeast India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00591","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"South India Foxtail millet & Besan Soft protein rice","ingredients":[{"name":"Foxtail millet","bengali_name":"BN local: Foxtail millet","quantity":"25 g"},{"name":"Besan","bengali_name":"বেসন","quantity":"20 g"},{"name":"Green peas","bengali_name":"মটরশুঁটি","quantity":"25 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"105 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":16,"region_culture":"South India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00111","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"West Bengal Oats & Spinach Soft finger bites","ingredients":[{"name":"Oats","bengali_name":"ওটস","quantity":"20 g"},{"name":"Spinach","bengali_name":"পালং শাক","quantity":"30 g"},{"name":"Bhetki","bengali_name":"ভেটকি","quantity":"15 g"}],"method_steps":["1) Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age. 2) Cook components with about 120 mL liquid until soft. 3) Mash and combine. 4) Shape into soft age-safe pieces and cook gently until set without creating a hard crust. 4) Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"98 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":17,"region_culture":"West Bengal / East India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"}]},{"title":"Lunch","recipes":[{"recipe_id":"MG-R-00039","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"West Bengal Oats & Peanut Soft khichdi","ingredients":[{"name":"Oats","bengali_name":"ওটস","quantity":"30 g"},{"name":"Peanut","bengali_name":"চিনাবাদাম","quantity":"25 g"},{"name":"Raw papaya","bengali_name":"পেঁপে","quantity":"20 g"},{"name":"Groundnut oil","bengali_name":"চিনাবাদাম তেল","quantity":"3 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"117 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":28,"region_culture":"West Bengal / East India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00146","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"West Bengal Ragi & Green peas Soft finger bites","ingredients":[{"name":"Ragi","bengali_name":"রাগি","quantity":"25 g"},{"name":"Green peas","bengali_name":"মটরশুঁটি","quantity":"15 g"},{"name":"Moong dal","bengali_name":"মুগ","quantity":"20 g"}],"method_steps":["1) Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age. 2) Cook components with about 120 mL liquid until soft. 3) Mash and combine. 4) Shape into soft age-safe pieces and cook gently until set without creating a hard crust. 4) Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"90 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":29,"region_culture":"West Bengal / East India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00121","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"West Bengal Flattened rice (poha & Besan Soft khichdi","ingredients":[{"name":"Flattened rice (poha","bengali_name":"চিঁড়া","quantity":"30 g"},{"name":"Besan","bengali_name":"বেসন","quantity":"20 g"},{"name":"Green peas","bengali_name":"মটরশুঁটি","quantity":"20 g"},{"name":"Groundnut oil","bengali_name":"চিনাবাদাম তেল","quantity":"3 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"110 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":30,"region_culture":"West Bengal / East India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00600","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"South India Ragi & Soybean Soft khichdi","ingredients":[{"name":"Ragi","bengali_name":"রাগি","quantity":"15 g"},{"name":"Soybean","bengali_name":"সয়াবিন","quantity":"20 g"},{"name":"Spinach","bengali_name":"পালং শাক","quantity":"40 g"},{"name":"Mustard oil","bengali_name":"সরিষার তেল","quantity":"3 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"117 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":31,"region_culture":"South India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00357","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"West India Oats & Chickpea Soft khichdi","ingredients":[{"name":"Oats","bengali_name":"ওটস","quantity":"25 g"},{"name":"Chickpea","bengali_name":"ছোলা","quantity":"20 g"},{"name":"Raw papaya","bengali_name":"পেঁপে","quantity":"40 g"},{"name":"Sesame oil","bengali_name":"তিলের তেল","quantity":"3 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"132 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":32,"region_culture":"West India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00123","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"West Bengal Gobindobhog rice & Raw papaya Soft finger bites","ingredients":[{"name":"Gobindobhog rice","bengali_name":"চাল","quantity":"30 g"},{"name":"Raw papaya","bengali_name":"পেঁপে","quantity":"40 g"},{"name":"Soybean","bengali_name":"সয়াবিন","quantity":"20 g"}],"method_steps":["1) Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age. 2) Cook components with about 120 mL liquid until soft. 3) Mash and combine. 4) Shape into soft age-safe pieces and cook gently until set without creating a hard crust. 4) Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"135 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":33,"region_culture":"West Bengal / East India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00576","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"South India Idli rice & Peanut Soft protein rice","ingredients":[{"name":"Idli rice","bengali_name":"চাল","quantity":"30 g"},{"name":"Peanut","bengali_name":"চিনাবাদাম","quantity":"25 g"},{"name":"Spinach","bengali_name":"পালং শাক","quantity":"30 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"128 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":34,"region_culture":"South India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00296","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"North India Bajra & Chickpea Soft protein rice","ingredients":[{"name":"Bajra","bengali_name":"বাজরা","quantity":"25 g"},{"name":"Chickpea","bengali_name":"ছোলা","quantity":"20 g"},{"name":"Onion","bengali_name":"পেঁয়াজ","quantity":"30 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"112 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":35,"region_culture":"North India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00504","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"South India Wheat flour (atta) & Brinjal Soft finger bites","ingredients":[{"name":"Wheat flour (atta)","bengali_name":"গম","quantity":"20 g"},{"name":"Brinjal","bengali_name":"বেগুন","quantity":"40 g"},{"name":"Chicken","bengali_name":"মুরগি","quantity":"25 g"}],"method_steps":["1) Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age. 2) Cook components with about 120 mL liquid until soft. 3) Mash and combine. 4) Shape into soft age-safe pieces and cook gently until set without creating a hard crust. 4) Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"128 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":36,"region_culture":"South India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00775","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"Central Little millet & Amaranth leaves Soft finger bites","ingredients":[{"name":"Little millet","bengali_name":"BN local: Little millet","quantity":"15 g"},{"name":"Amaranth leaves","bengali_name":"নটে/আমরান্থ শাক","quantity":"40 g"},{"name":"Peanut","bengali_name":"চিনাবাদাম","quantity":"15 g"}],"method_steps":["1) Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age. 2) Cook components with about 120 mL liquid until soft. 3) Mash and combine. 4) Shape into soft age-safe pieces and cook gently until set without creating a hard crust. 4) Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"105 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":37,"region_culture":"Central / Tribal India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"}]},{"title":"Dinner","recipes":[{"recipe_id":"MG-R-00813","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"Himalayan India Ragi & Moong dal Soft khichdi","ingredients":[{"name":"Ragi","bengali_name":"রাগি","quantity":"20 g"},{"name":"Moong dal","bengali_name":"মুগ","quantity":"15 g"},{"name":"Brinjal","bengali_name":"বেগুন","quantity":"30 g"},{"name":"Groundnut oil","bengali_name":"চিনাবাদাম তেল","quantity":"3 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"102 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":48,"region_culture":"Himalayan India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00532","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"South India Flattened rice (poha & Chickpea Soft khichdi","ingredients":[{"name":"Flattened rice (poha","bengali_name":"চিঁড়া","quantity":"30 g"},{"name":"Chickpea","bengali_name":"ছোলা","quantity":"25 g"},{"name":"Spinach","bengali_name":"পালং শাক","quantity":"30 g"},{"name":"Sesame oil","bengali_name":"তিলের তেল","quantity":"3 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"132 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":49,"region_culture":"South India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00247","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"North India Ragi & Peanut Soft khichdi","ingredients":[{"name":"Ragi","bengali_name":"রাগি","quantity":"15 g"},{"name":"Peanut","bengali_name":"চিনাবাদাম","quantity":"25 g"},{"name":"Green peas","bengali_name":"মটরশুঁটি","quantity":"20 g"},{"name":"Groundnut oil","bengali_name":"চিনাবাদাম তেল","quantity":"3 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"94 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":50,"region_culture":"North India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00229","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"North India Wheat flour (atta) & Soybean Soft khichdi","ingredients":[{"name":"Wheat flour (atta)","bengali_name":"গম","quantity":"15 g"},{"name":"Soybean","bengali_name":"সয়াবিন","quantity":"25 g"},{"name":"Brinjal","bengali_name":"বেগুন","quantity":"20 g"},{"name":"Groundnut oil","bengali_name":"চিনাবাদাম তেল","quantity":"3 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"94 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":51,"region_culture":"North India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00791","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"Himalayan India Flattened rice (poha & Chickpea Soft khichdi","ingredients":[{"name":"Flattened rice (poha","bengali_name":"চিঁড়া","quantity":"15 g"},{"name":"Chickpea","bengali_name":"ছোলা","quantity":"25 g"},{"name":"Green peas","bengali_name":"মটরশুঁটি","quantity":"25 g"},{"name":"Sesame oil","bengali_name":"তিলের তেল","quantity":"3 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"102 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":52,"region_culture":"Himalayan India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00938","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"Bangladesh Wheat flour (atta) & Soybean Soft protein rice","ingredients":[{"name":"Wheat flour (atta)","bengali_name":"গম","quantity":"25 g"},{"name":"Soybean","bengali_name":"সয়াবিন","quantity":"25 g"},{"name":"Potato","bengali_name":"আলু","quantity":"40 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"135 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":53,"region_culture":"Bangladesh","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00549","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"South India Oats & Green peas Soft khichdi","ingredients":[{"name":"Oats","bengali_name":"ওটস","quantity":"20 g"},{"name":"Green peas","bengali_name":"মটরশুঁটি","quantity":"15 g"},{"name":"Brinjal","bengali_name":"বেগুন","quantity":"20 g"},{"name":"Groundnut oil","bengali_name":"চিনাবাদাম তেল","quantity":"3 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"87 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":54,"region_culture":"South India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00809","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"Himalayan India Jhangora & Green peas Soft finger bites","ingredients":[{"name":"Jhangora","bengali_name":"BN local: Barnyard millet Himalayan","quantity":"30 g"},{"name":"Green peas","bengali_name":"মটরশুঁটি","quantity":"20 g"},{"name":"Chickpea","bengali_name":"ছোলা","quantity":"20 g"}],"method_steps":["1) Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age. 2) Cook components with about 120 mL liquid until soft. 3) Mash and combine. 4) Shape into soft age-safe pieces and cook gently until set without creating a hard crust. 4) Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"105 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":55,"region_culture":"Himalayan India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00680","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"Northeast India Oats & Green peas Soft protein rice","ingredients":[{"name":"Oats","bengali_name":"ওটস","quantity":"25 g"},{"name":"Green peas","bengali_name":"মটরশুঁটি","quantity":"15 g"},{"name":"Onion","bengali_name":"পেঁয়াজ","quantity":"40 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"120 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":56,"region_culture":"Northeast India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"},{"recipe_id":"MG-R-00293","recipe_version":"d94f6da3b53bb989340fe316a74deb32a4c6be23020cd9fb3fb396743a2b8513","title":"North India Cornmeal & Green peas Soft khichdi","ingredients":[{"name":"Cornmeal","bengali_name":"BN local: Makki atta","quantity":"20 g"},{"name":"Green peas","bengali_name":"মটরশুঁটি","quantity":"25 g"},{"name":"Raw papaya","bengali_name":"পেঁপে","quantity":"40 g"},{"name":"Sesame oil","bengali_name":"তিলের তেল","quantity":"3 g"}],"method_steps":["Wash/clean all ingredients; peel, deseed, debone, chop, mash or grind as appropriate for age.","Cook the main mixture with about 120 mL potable water/liquid over low-medium heat until all ingredients are fully cooked and soft.","Adjust texture by mashing or adding a small amount of boiled water if required.","Cool to a comfortable serving temperature and serve the prescribed portion under adult supervision."],"serving":"132 g","texture_serving":"Mashed/Lumpy/Soft finger food","number":57,"region_culture":"North India","source":"provider","review_status":"Draft — Culinary/Nutrition/Clinical Review Required"}]}]}`
