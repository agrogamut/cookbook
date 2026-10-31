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
	if len(c.Entries) != 525 {
		t.Fatal("incomplete accepted response set")
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
		"version":      func(c *nativeFallbackCatalog) { c.Version++ },
		"incomplete":   func(c *nativeFallbackCatalog) { c.Entries = c.Entries[:524] },
		"source-audit": func(c *nativeFallbackCatalog) { c.AuditSHA256 = strings.Repeat("a", 64) },
		"output-hash":  func(c *nativeFallbackCatalog) { c.Entries[0].Output += " changed" },
		"blank":        func(c *nativeFallbackCatalog) { c.Entries[0].Output = ""; c.Entries[0].OutputSHA256 = nativeHash("") },
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
