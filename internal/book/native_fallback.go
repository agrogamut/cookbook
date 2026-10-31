package book

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/madamgy/recipie/internal/bookdocx"
	"github.com/madamgy/recipie/internal/profile"
)

//go:embed native_fallback.json
var nativeFallbackData []byte

const nativeFallbackDataSHA = "93b75983f9088c3bde3e03d545501414ac0c029a9e7ca5c4b741008d55fae5fc"
const nativeFallbackContract = "native-protected-response-v1"
const nativeFallbackBuilder = "native-bindings-v1"

type nativeFallbackOwner struct {
	Source        string `json:"source"`
	Revision      string `json:"revision"`
	RecipeID      string `json:"recipe_id,omitempty"`
	Field         string `json:"field"`
	Ordinal       int    `json:"ordinal,omitempty"`
	EnglishSHA256 string `json:"english_sha256"`
	ArchiveSHA256 string `json:"archive_sha256,omitempty"`
	MemberSHA256  string `json:"member_sha256,omitempty"`
	Sheet         string `json:"sheet,omitempty"`
	Row           int    `json:"row,omitempty"`
}

type nativeFallbackSlot struct {
	Kind   string `json:"kind"`
	Unit   string `json:"unit,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

type nativeFallbackEntry struct {
	Language       string                    `json:"language"`
	Input          string                    `json:"input"`
	InputSHA256    string                    `json:"input_sha256"`
	Output         string                    `json:"output"`
	OutputSHA256   string                    `json:"output_sha256"`
	Owners         []nativeFallbackAllowance `json:"owners"`
	RequestSHA256  string                    `json:"request_sha256"`
	ResponseSHA256 string                    `json:"response_sha256"`
}

type nativeFallbackAllowance struct {
	Owner nativeFallbackOwner  `json:"owner"`
	Slots []nativeFallbackSlot `json:"slots"`
}

type nativeFallbackCatalog struct {
	Version        int                      `json:"version"`
	Contract       string                   `json:"contract"`
	ManifestSHA256 string                   `json:"manifest_sha256"`
	AuditSHA256    string                   `json:"audit_sha256"`
	Supplement     nativeFallbackSupplement `json:"supplement"`
	Entries        []nativeFallbackEntry    `json:"entries"`
	byInput        map[string][]int
}

type nativeFallbackSupplement struct {
	ManifestSHA256   string `json:"manifest_sha256"`
	AuditSHA256      string `json:"audit_sha256"`
	CandidatesSHA256 string `json:"candidates_sha256"`
	FirstEntry       int    `json:"first_entry"`
	EntryCount       int    `json:"entry_count"`
}

type nativeIdentitySpan struct {
	Start, End  int
	Value       string
	Passthrough bool
}

type nativeFallbackBinding struct {
	Owner    nativeFallbackOwner
	Identity *nativeIdentitySpan
}

type nativeFallbackContext struct {
	Bindings map[string]nativeFallbackBinding
	Cards    map[string]RecipeCard
}

type nativeCell struct {
	Path  string
	Value *string
}

var nativeFallbackOnce sync.Once
var nativeFallbackLoaded *nativeFallbackCatalog
var nativeFallbackLoadError error
var nativeSlotNumber = regexp.MustCompile(`^[-+]?(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)(?:[ \t]*(g|kg|mg|µg|μg|mcg|cm|mm|mL|ml|L|kcal|kJ|min|h|mo|IU|%))?$`)

func nativeHash(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }

func nativeObjectHash(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return nativeHash(string(b))
}

func loadNativeFallback() (*nativeFallbackCatalog, error) {
	nativeFallbackOnce.Do(func() {
		nativeFallbackLoaded, nativeFallbackLoadError = parseNativeFallback(nativeFallbackData, nativeFallbackDataSHA)
	})
	return nativeFallbackLoaded, nativeFallbackLoadError
}

func parseNativeFallback(data []byte, expected string) (*nativeFallbackCatalog, error) {
	if nativeHash(string(data)) != expected {
		return nil, fmt.Errorf("native fallback catalog: file hash mismatch")
	}
	var c nativeFallbackCatalog
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return nil, fmt.Errorf("native fallback catalog: %w", err)
	}
	if d.Decode(new(any)) != io.EOF || c.Version != 2 || c.Contract != nativeFallbackContract || len(c.Entries) != 547 ||
		c.ManifestSHA256 != "e72e207c2f1480e4bc86609d603860de41989eafa50b67822c2905ca00c28f9f" ||
		c.AuditSHA256 != "9b9fba6c7c147dfc6fddbc984cd224a8f0816e2eeeca38e49c2468811a2ccbcd" {
		return nil, fmt.Errorf("native fallback catalog: incomplete or unsupported contract")
	}
	if c.Supplement != (nativeFallbackSupplement{
		ManifestSHA256:   "0c8b5b7a723a993a10f8d6f25b2b5aeb0989dba49aebeb2d590ac94283ca1755",
		AuditSHA256:      "d50c3ce7d32b0bf594a938c242e003d00b414c52f079a8c3ef5de3bc55adbaab",
		CandidatesSHA256: "d9a478767e5a4dc11e96fa5e19dfd8ac187985fd213a1ba78d7922303c89c12f",
		FirstEntry:       525, EntryCount: 22,
	}) {
		return nil, fmt.Errorf("native fallback catalog: unsupported supplement provenance")
	}
	c.byInput = map[string][]int{}
	seen := map[string]bool{}
	for i, e := range c.Entries {
		if (e.Language != "bn" && e.Language != "hi") || e.Input == "" || e.InputSHA256 != nativeHash(e.Input) || e.OutputSHA256 != nativeHash(e.Output) || len(e.Owners) == 0 || !nativeDigest(e.RequestSHA256) || !nativeDigest(e.ResponseSHA256) {
			return nil, fmt.Errorf("native fallback catalog: invalid entry %d", i)
		}
		key := e.Language + ":" + e.InputSHA256
		for _, allowance := range e.Owners {
			owner := allowance.Owner
			if err := nativeValidateMasked(e.Input, len(allowance.Slots)); err != nil {
				return nil, fmt.Errorf("native fallback catalog: input %d: %w", i, err)
			}
			if err := nativeValidateMasked(e.Output, len(allowance.Slots)); err != nil {
				return nil, fmt.Errorf("native fallback catalog: output %d: %w", i, err)
			}
			for slotIndex, s := range allowance.Slots {
				if (s.Kind != "number" && s.Kind != "identity" && s.Kind != "literal") || (s.Kind == "literal" && !nativeDigest(s.SHA256)) || (s.Kind != "literal" && s.SHA256 != "") || (s.Kind != "number" && s.Unit != "") {
					return nil, fmt.Errorf("native fallback catalog: invalid slot in entry %d", i)
				}
				if s.Kind == "number" && (strings.TrimSpace(s.Unit) != s.Unit || !nativeSlotNumber.MatchString("1"+s.Unit)) {
					return nil, fmt.Errorf("native fallback catalog: invalid unit in entry %d", i)
				}
				if s.Kind == "identity" && (slotIndex != 0 || owner.Source != "builder" || owner.Field != "B1-02/letterPage") {
					return nil, fmt.Errorf("native fallback catalog: invalid identity site in entry %d", i)
				}
			}
			if owner.Source == "" || owner.Revision == "" || owner.Field == "" || !nativeDigest(owner.EnglishSHA256) || owner.Ordinal < 0 {
				return nil, fmt.Errorf("native fallback catalog: invalid owner in entry %d", i)
			}
			if owner.Source == "case-recipe" && (!nativeDigest(owner.ArchiveSHA256) || !nativeDigest(owner.MemberSHA256) || owner.Sheet == "" || owner.Row < 1 || owner.RecipeID == "") {
				return nil, fmt.Errorf("native fallback catalog: missing source locator in entry %d", i)
			}
			unique := key + ":" + nativeObjectHash(allowance)
			if seen[unique] {
				return nil, fmt.Errorf("native fallback catalog: duplicate owner in entry %d", i)
			}
			seen[unique] = true
		}
		c.byInput[key] = append(c.byInput[key], i)
	}
	return &c, nil
}

func nativeDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func nativeValidateMasked(text string, count int) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("blank content")
	}
	for i := 0; i < count; i++ {
		if strings.Count(text, fmt.Sprintf("ZXQ%dQXZ", i)) != 1 {
			return fmt.Errorf("changed a protected value")
		}
	}
	rest := nativeMarker.ReplaceAllString(text, "")
	if len(nativeMarker.FindAllString(text, -1)) != count || strings.Contains(rest, "ZXQ") {
		return fmt.Errorf("unknown protected value")
	}
	for _, r := range rest {
		if unicode.IsDigit(r) {
			return fmt.Errorf("unprotected numeric value")
		}
	}
	return nil
}

func nativeProtect(text string, protected *regexp.Regexp, identity *nativeIdentitySpan) (string, []string, []nativeFallbackSlot, error) {
	if strings.Contains(text, "ZXQ") {
		return "", nil, nil, fmt.Errorf("reserved protected marker in source")
	}
	var tokens []string
	var slots []nativeFallbackSlot
	add := func(value, kind string) string {
		marker := fmt.Sprintf("ZXQ%dQXZ", len(tokens))
		tokens = append(tokens, value)
		slot := nativeFallbackSlot{Kind: kind}
		if kind != "identity" {
			if match := nativeSlotNumber.FindStringSubmatch(value); match != nil {
				slot.Kind, slot.Unit = "number", match[1]
			} else {
				slot.Kind, slot.SHA256 = "literal", nativeHash(value)
			}
		}
		slots = append(slots, slot)
		return marker
	}
	mask := func(s string) string {
		return protected.ReplaceAllStringFunc(s, func(v string) string { return add(v, "literal") })
	}
	if identity == nil {
		masked := mask(text)
		return masked, tokens, slots, nil
	}
	if identity.Start < 0 || identity.End < identity.Start || identity.End > len(text) || text[identity.Start:identity.End] != identity.Value {
		return "", nil, nil, fmt.Errorf("identity source span mismatch")
	}
	before := mask(text[:identity.Start])
	name := add(identity.Value, "identity")
	after := mask(text[identity.End:])
	return before + name + after, tokens, slots, nil
}

func nativeRestore(masked string, tokens []string) (string, error) {
	if err := nativeValidateMasked(masked, len(tokens)); err != nil {
		return "", err
	}
	return nativeMarker.ReplaceAllStringFunc(masked, func(marker string) string {
		i, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(marker, "ZXQ"), "QXZ"))
		return tokens[i]
	}), nil
}

func (c *nativeFallbackCatalog) lookup(ctx context.Context, language, masked string, slots []nativeFallbackSlot, owner nativeFallbackOwner) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if owner.Source == "" {
		return "", false, nil
	}
	if owner.EnglishSHA256 == "" {
		owner.EnglishSHA256 = nativeHash(masked)
	}
	for _, i := range c.byInput[language+":"+nativeHash(masked)] {
		e := &c.Entries[i]
		if e.Input != masked {
			continue
		}
		for _, allowed := range e.Owners {
			if allowed.Owner == owner && nativeObjectHash(allowed.Slots) == nativeObjectHash(slots) {
				return e.Output, true, nil
			}
		}
	}
	return "", false, nil
}

func nativeBlockCells(blocks []bookdocx.Block, prefix string) []nativeCell {
	var cells []nativeCell
	for i := range blocks {
		base := fmt.Sprintf("%s/Blocks/%d", prefix, i)
		b := &blocks[i]
		allowed := map[*string]bool{}
		for _, p := range nativeTextCells(blocks[i : i+1]) {
			allowed[p] = true
		}
		if b.Paragraph != nil {
			for j := range b.Paragraph.Runs {
				p := &b.Paragraph.Runs[j].Text
				if allowed[p] {
					cells = append(cells, nativeCell{fmt.Sprintf("%s/Paragraph/Runs/%d/Text", base, j), p})
				}
			}
		}
		if b.Table != nil {
			for j := range b.Table.Rows {
				for k := range b.Table.Rows[j] {
					p := &b.Table.Rows[j][k]
					if allowed[p] {
						cells = append(cells, nativeCell{fmt.Sprintf("%s/Table/Rows/%d/%d", base, j, k), p})
					}
				}
			}
		}
	}
	return cells
}

func nativeModulePath(book int, key string) string {
	return fmt.Sprintf("/Books/%d/Modules/%s", book-1, key)
}

func nativeNewContext(s profile.Stored, set Set) *nativeFallbackContext {
	c := &nativeFallbackContext{Bindings: map[string]nativeFallbackBinding{}, Cards: map[string]RecipeCard{}}
	for _, meal := range set.Book2.MealSections {
		for _, r := range meal.Recipes {
			c.Cards[r.RecipeID] = r
		}
	}
	base := nativeModulePath(1, "B1-02")
	c.Bindings[base+"/Blocks/0/Paragraph/Runs/0/Text"] = nativeFallbackBinding{Identity: &nativeIdentitySpan{0, len(s.DisplayName), s.DisplayName, true}}
	if len(set.Book1.Letter.Body) > 0 && set.Book1.Letter.Body[0] == "Dear "+s.DisplayName+"'s family," {
		c.Bindings[base+"/Blocks/1/Paragraph/Runs/0/Text"] = nativeFallbackBinding{
			Owner:    nativeFallbackOwner{Source: "builder", Revision: nativeFallbackBuilder, Field: "B1-02/letterPage"},
			Identity: &nativeIdentitySpan{5, 5 + len(s.DisplayName), s.DisplayName, false},
		}
	}
	return c
}

func (n *NativeSet) nativeOwnBlocks(prefix string, blocks []bookdocx.Block, offset int, owner nativeFallbackOwner, exact bool) {
	if n.fallback == nil {
		return
	}
	for _, cell := range nativeBlockCells(blocks, prefix) {
		suffix := strings.TrimPrefix(cell.Path, prefix+"/Blocks/")
		index, rest, _ := strings.Cut(suffix, "/")
		i, _ := strconv.Atoi(index)
		path := fmt.Sprintf("%s/Blocks/%d/%s", prefix, i+offset, rest)
		o := owner
		o.Field += "/" + index + "/" + rest
		if exact && o.EnglishSHA256 == "" {
			o.EnglishSHA256 = nativeHash(*cell.Value)
		}
		n.fallback.Bindings[path] = nativeFallbackBinding{Owner: o}
	}
}

func (n *NativeSet) nativeOwnSection(module int, s Section, blocks []bookdocx.Block, offset int) {
	n.nativeOwnBlocks(nativeModulePath(1, fmt.Sprintf("B1-%02d", module)), blocks, offset,
		nativeFallbackOwner{Source: "provider-section/" + s.BlockID, Revision: nativeObjectHash(blocks), Field: s.BlockID}, true)
}

func nativeCardOwner(r RecipeCard, field string, ordinal int, english string) nativeFallbackOwner {
	o := nativeFallbackOwner{Source: "provider-recipe", Revision: r.RecipeVersion, RecipeID: r.RecipeID, Field: field, Ordinal: ordinal, EnglishSHA256: nativeHash(english)}
	if r.SourceRevision != nil {
		o.Source = "case-recipe"
		o.Revision = nativeObjectHash(r.SourceRevision)
		o.ArchiveSHA256, o.MemberSHA256 = r.SourceRevision.ArchiveSHA256, r.SourceRevision.MemberSHA256
		o.Sheet, o.Row = r.SourceRevision.Sheet, r.SourceRevision.Row
	}
	return o
}

func (n *NativeSet) nativeOwnRecipe(key string, entry int, r RecipeCard, meal string) {
	original := n.fallback.Cards[r.RecipeID]
	prefix := fmt.Sprintf("%s/Entries/%d", nativeModulePath(2, key), entry)
	offset := 0
	for part := 43; part <= 48; part++ {
		blocks := nativeRecipe(r, meal, part)
		if part != 43 {
			blocks = blocks[1:]
		}
		for i := range blocks {
			field, ordinal, raw := fmt.Sprintf("card-part-%d", part), 0, nativeCardPartSource(original, part)
			if part == 44 && i < len(r.Ingredients) {
				field, ordinal = "Ingredient_Names", r.Ingredients[i].SourceOrdinal
				if ordinal == 0 {
					ordinal = i + 1
				}
				raw = nativeObjectHash(original.Ingredients[i])
			} else if part == 45 && i < len(r.MethodSteps) {
				field, ordinal, raw = "Preparation_Method_FULL", i+1, strings.Join(original.MethodSteps, "\n")
				if v := original.SourceFields[field]; v != "" {
					raw = v
				}
			}
			n.nativeOwnBlocks(prefix, blocks[i:i+1], offset+i, nativeCardOwner(original, field, ordinal, raw), false)
		}
		offset += len(blocks)
	}
}

func nativeCardPartSource(r RecipeCard, part int) string {
	switch part {
	case 43:
		return nativeObjectHash([]string{r.Title, r.RegionCulture, r.Source, r.ReviewStatus})
	case 44:
		return nativeObjectHash(r.Ingredients) + r.Serving
	case 45:
		return nativeObjectHash([]any{r.MethodSteps, r.PrepTimeMinutes, r.CookTimeMinutes})
	case 46:
		return nativeObjectHash(r.Nutrition) + r.SourceFields["Nutrient_Gap"]
	case 47:
		return r.Safety + nativeObjectHash(r.SourceFields)
	default:
		return r.Serving + "\n" + r.StorageRule
	}
}

func (n *NativeSet) nativeCaptureBindings(_ profile.Stored, set Set) {
	for _, pair := range []struct{ book, module int }{{1, 57}, {2, 7}, {2, 8}} {
		key := fmt.Sprintf("B%d-%02d", pair.book, pair.module)
		blocks := n.Books[pair.book-1].Modules[key].Blocks
		source := nativeObjectHash(set.Book2.SafetySOP)
		if pair.module == 8 {
			source = nativeObjectHash([]any{set.Book2.HoneyRule, set.Book2.ChokingHazards})
		}
		n.nativeOwnBlocks(nativeModulePath(pair.book, key), blocks, 0, nativeFallbackOwner{Source: "provider-food-safety", Revision: source, Field: key}, true)
	}
	for _, pair := range []struct{ book, module int }{{1, 3}, {1, 22}, {2, 3}, {2, 5}, {2, 28}} {
		block := 0
		for _, meal := range set.Book2.MealSections {
			if pair.module == 22 && !strings.EqualFold(meal.Title, "School Tiffin") {
				continue
			}
			for _, card := range meal.Recipes {
				r := n.fallback.Cards[card.RecipeID]
				key := fmt.Sprintf("B%d-%02d", pair.book, pair.module)
				m := n.Books[pair.book-1].Modules[key]
				if block < len(m.Blocks) {
					field := "selected-recipe-index"
					if pair.module == 5 || pair.module == 28 {
						field = "Serving_Size_g/Texture"
					}
					n.nativeOwnBlocks(nativeModulePath(pair.book, key), m.Blocks[block:block+1], block, nativeCardOwner(r, field, 0, r.Title+"\n"+r.Serving+"\n"+meal.Title), false)
				}
				block++
			}
		}
	}
	for bi, req := range n.Books {
		for key, m := range req.Modules {
			prefix := nativeModulePath(bi+1, key)
			cells := nativeBlockCells(m.Blocks, prefix)
			if m.Gap != "" {
				cells = append(cells, nativeCell{prefix + "/Gap", &m.Gap})
			}
			for _, cell := range cells {
				binding := n.fallback.Bindings[cell.Path]
				if binding.Owner.Source == "" && binding.Identity == nil {
					binding.Owner = nativeFallbackOwner{Source: "builder", Revision: nativeFallbackBuilder, Field: strings.TrimPrefix(cell.Path, fmt.Sprintf("/Books/%d/Modules/", bi))}
					n.fallback.Bindings[cell.Path] = binding
				}
			}
		}
	}
}

func (n *NativeSet) nativeOwnIndex(key string, blocks []bookdocx.Block, values [][]string) {
	if n.fallback == nil {
		return
	}
	prefix := nativeModulePath(2, key)
	n.nativeOwnBlocks(prefix, blocks, 0, nativeFallbackOwner{Source: "builder", Revision: nativeFallbackBuilder, Field: key + "/index-context"}, false)
	for i, row := range values {
		if len(row) != 4 {
			continue
		}
		r, ok := n.fallback.Cards[row[0]]
		if !ok {
			continue
		}
		for col, value := range row {
			field := row[2]
			if col == 1 {
				field = "Recipe_Name"
				value = r.Title
			}
			path := fmt.Sprintf("%s/Blocks/1/Table/Rows/%d/%d", prefix, i+1, col)
			n.fallback.Bindings[path] = nativeFallbackBinding{Owner: nativeCardOwner(r, "index/"+field+fmt.Sprintf("/column/%d", col), 0, value)}
		}
	}
}

func (n *NativeSet) nativeOwnIngredientIndex(b Book2) {
	type group struct {
		id     string
		owners []nativeFallbackOwner
	}
	groups := map[string]*group{}
	for _, meal := range b.MealSections {
		for _, card := range meal.Recipes {
			r := n.fallback.Cards[card.RecipeID]
			for i, v := range card.Ingredients {
				key := v.SourceID + "\x00" + v.Name
				if groups[key] == nil {
					groups[key] = &group{id: v.SourceID}
				}
				ordinal := v.SourceOrdinal
				if ordinal == 0 {
					ordinal = i + 1
				}
				groups[key].owners = append(groups[key].owners, nativeCardOwner(r, "Ingredient_Names", ordinal, nativeObjectHash(r.Ingredients[i])))
			}
		}
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	m := n.Books[1].Modules["B2-54"]
	for i, key := range keys {
		g := groups[key]
		// Every occurrence contributes provenance, including repeated ingredients in one recipe.
		n.nativeOwnBlocks(nativeModulePath(2, "B2-54"), m.Blocks[i:i+1], i, nativeFallbackOwner{Source: "ingredient-index", Revision: nativeObjectHash(g.owners), Field: "ingredient/" + g.id, EnglishSHA256: nativeObjectHash(g.owners)}, false)
	}
}
