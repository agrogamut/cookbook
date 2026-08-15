package enrich

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Matching a provider recipe to an external method happens in three stages, in this
// order. The first two are gates; only the third is a score.
//
//  1. Dish format. The provider encodes the format in the recipe name ("... Soft
//     khichdi", "... Mini cutlet/patty"). external_format_map says which external dish
//     names may supply method text for it, and external_method_exclusion removes
//     deep-fried dishes outright. A format with no mapping gets no suggestion at all.
//  2. Ingredient coverage. The match must account for most of what the provider lists.
//  3. Jaccard, which ranks the survivors and penalises an external recipe that piles on
//     ingredients the provider recipe does not have.
//
// These values were calibrated by reading samples, not chosen in advance. The history is
// worth keeping because it shows what the failure looked like:
//
//	0.75*containment + 0.25*jaccard >= 0.60, no format gate   ~10 of 12 pairs wrong
//	full coverage + jaccard >= 0.34, no format gate           0 matches
//	full coverage + jaccard >= 0.15, no format gate           ~9 of 10 pairs wrong
//	format gate + 0.6 coverage + jaccard >= 0.10              0 of 12 clearly wrong
//
// Ingredient overlap alone never worked at any threshold: a rice-and-peanut infant mash
// scored well against peanut bhel chaat, and a lemon rice porridge against paneer
// biryani. All of those share ingredients and none shares a preparation. Dish format is
// what identifies a dish.
//
// What this yields is a method for the same FORMAT, not the same dish -- a khichdi
// method for a khichdi with different vegetables. The card view says so, and it must
// keep saying so.
var MethodThreshold = 0.10

// MethodCoverRequired is the fraction of the provider's ingredients the external recipe
// must account for.
var MethodCoverRequired = 0.6

// NutritionThreshold is the minimum Jaccard score for an ingredient to be considered the
// same food as an IFCT entry, on top of a full-coverage gate. IFCT names carry qualifiers
// the provider name does not ("Rice, raw, milled" against "Rice"), so a correct match
// still scores well below 1. The full-coverage gate is what rules out matching "Pearl
// spot" to "Pearl millet".
var NutritionThreshold = 0.30

// NutritionCoverRequired: every word of the provider's ingredient name must appear in
// the IFCT food name.
var NutritionCoverRequired = 1.0

// DiscrepancyPct is how far a provider value may sit from the IFCT value before it is
// worth a human look.
const DiscrepancyPct = 20.0

// ing is one provider ingredient plus how many recipes actually use it, so the audit
// report can lead with the rows a user can see.
type ing struct {
	id                        string
	name                      string
	energy, protein, iron, ca float64
	used                      int
}

// formatRule is one row of external_format_map: which external dish names may supply
// method text for a provider dish format.
type formatRule struct {
	pattern  *regexp.Regexp
	keywords []string
}

type candidate struct {
	id       int64
	name     string
	tokens   []string
	url      *string
	region   string
	instr    string
	sourceID string
}

// matchMethods joins every in-scope provider recipe to the best external recipe from the
// same region, falling back to pan-Indian rows when no regional match clears the bar.
//
// The region constraint is not an optimisation. A Punjabi method on a Bengali dish is a
// wrong match even at high ingredient overlap, because the two cuisines cook the same
// vegetables differently.
func matchMethods(ctx context.Context, tx pgx.Tx) (matched, total int, err error) {
	formats, err := loadFormatRules(ctx, tx)
	if err != nil {
		return 0, 0, err
	}
	excluded, err := loadExclusions(ctx, tx)
	if err != nil {
		return 0, 0, err
	}

	byRegion := map[string][]candidate{}
	rows, err := tx.Query(ctx, `
		SELECT external_recipe_id, recipe_name, ingredient_tokens, url, region_culture, instructions
		FROM external_recipe`)
	if err != nil {
		return 0, 0, fmt.Errorf("enrich: read external recipes: %w", err)
	}
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.name, &c.tokens, &c.url, &c.region, &c.instr); err != nil {
			rows.Close()
			return 0, 0, fmt.Errorf("enrich: read external recipes: %w", err)
		}
		byRegion[c.region] = append(byRegion[c.region], c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("enrich: read external recipes: %w", err)
	}

	type target struct {
		id     string
		name   string
		region string
		tokens []string
	}
	var targets []target
	rows, err = tx.Query(ctx, `
		SELECT r.recipe_id, r.recipe_name, r.region_culture,
		       (SELECT array_agg(DISTINCT lower(m.ingredient_name_en))
		        FROM recipe_ingredient_mapping m WHERE m.recipe_id = r.recipe_id)
		FROM recipe_master r`)
	if err != nil {
		return 0, 0, fmt.Errorf("enrich: read provider recipes: %w", err)
	}
	for rows.Next() {
		var t target
		var names []string
		if err := rows.Scan(&t.id, &t.name, &t.region, &names); err != nil {
			rows.Close()
			return 0, 0, fmt.Errorf("enrich: read provider recipes: %w", err)
		}
		for _, n := range names {
			t.tokens = append(t.tokens, Tokenise(n)...)
		}
		t.tokens = dedupe(t.tokens)
		targets = append(targets, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("enrich: read provider recipes: %w", err)
	}
	total = len(targets)

	if _, err := tx.Exec(ctx, `DELETE FROM recipe_method_external`); err != nil {
		return 0, 0, fmt.Errorf("enrich: clear recipe_method_external: %w", err)
	}

	batch := &pgx.Batch{}
	for _, t := range targets {
		keywords := formatKeywords(t.name, formats)
		if len(keywords) == 0 {
			// The dish format has no counterpart in the external corpus. No suggestion.
			continue
		}

		best, score, shared, kind := bestMatch(t.tokens, byRegion[t.region], "same-region", MethodCoverRequired, keywords, excluded)
		if score < MethodThreshold {
			// Fall back to unlabelled Indian rows, recorded as a weaker region match so
			// a reader can tell a Bengali-sourced suggestion from a generic one.
			if fb, fbScore, fbShared, fbKind := bestMatch(t.tokens, byRegion[panIndia], "pan-india", MethodCoverRequired, keywords, excluded); fbScore > score {
				best, score, shared, kind = fb, fbScore, fbShared, fbKind
			}
		}
		if best == nil || score < MethodThreshold {
			continue // no suggestion is the honest outcome
		}
		batch.Queue(`
			INSERT INTO recipe_method_external
			    (recipe_id, external_recipe_id, source_key, source_url, match_confidence,
			     match_method, matched_tokens, region_match, suggested_method)
			VALUES ($1,$2,'INDIAN-RECIPES',$3,$4,$5,$6,$7,$8)`,
			t.id, best.id, best.url, round4(score),
			"dish-format keyword match, then full ingredient coverage, then jaccard rank, within region",
			shared, kind, best.instr)
		matched++
	}
	if err := sendBatch(ctx, tx, batch, "recipe_method_external"); err != nil {
		return 0, 0, err
	}
	return matched, total, nil
}

// bestMatch returns the highest-scoring candidate that names the right dish format and
// covers every token in tokens.
//
// Both constraints are gates, not score terms. A candidate of the wrong format is not a
// weaker match, it is a method for a different dish; the same goes for one that omits an
// ingredient the provider lists. Only among candidates that pass both does jaccard rank.
//
// A nil keywords slice disables the format gate, which is what the nutrition audit wants:
// it matches food names, not dishes.
func bestMatch(tokens []string, pool []candidate, kind string, requireCover float64, keywords, excluded []string) (*candidate, float64, []string, string) {
	var (
		best   *candidate
		bestSc float64
		shared []string
	)
	for i := range pool {
		if keywords != nil && !nameHasKeyword(pool[i].name, keywords) {
			continue
		}
		if excluded != nil && nameHasKeyword(pool[i].name, excluded) {
			continue
		}
		if Cover(tokens, pool[i].tokens) < requireCover {
			continue
		}
		j, sh := Jaccard(tokens, pool[i].tokens)
		if j > bestSc {
			best, bestSc, shared = &pool[i], j, sh
		}
	}
	if shared == nil {
		shared = []string{}
	}
	return best, bestSc, shared, kind
}

// auditNutrition compares every provider ingredient to IFCT 2017. It writes a comparison
// and never a correction: the provider's numbers stay exactly as shipped, and a
// disagreement is reported for a human rather than silently resolved.
func auditNutrition(ctx context.Context, tx pgx.Tx) (matched, total int, err error) {
	qualifiers, err := loadQualifiers(ctx, tx)
	if err != nil {
		return 0, 0, err
	}

	var foods []candidate
	rows, err := tx.Query(ctx, `SELECT food_code, food_name, name_tokens FROM external_food_composition`)
	if err != nil {
		return 0, 0, fmt.Errorf("enrich: read composition: %w", err)
	}
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.sourceID, &c.name, &c.tokens); err != nil {
			rows.Close()
			return 0, 0, fmt.Errorf("enrich: read composition: %w", err)
		}
		foods = append(foods, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("enrich: read composition: %w", err)
	}

	var ings []ing
	rows, err = tx.Query(ctx, `
		SELECT i.ingredient_id, i.english_name,
		       i.energy_kcal_100g, i.protein_g_100g, i.iron_mg_100g, i.calcium_mg_100g,
		       (SELECT count(DISTINCT m.recipe_id) FROM recipe_ingredient_mapping m
		        WHERE m.ingredient_id = i.ingredient_id)
		FROM ingredient_master i`)
	if err != nil {
		return 0, 0, fmt.Errorf("enrich: read ingredients: %w", err)
	}
	for rows.Next() {
		var x ing
		if err := rows.Scan(&x.id, &x.name, &x.energy, &x.protein, &x.iron, &x.ca, &x.used); err != nil {
			rows.Close()
			return 0, 0, fmt.Errorf("enrich: read ingredients: %w", err)
		}
		ings = append(ings, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("enrich: read ingredients: %w", err)
	}
	total = len(ings)

	if _, err := tx.Exec(ctx, `DELETE FROM ingredient_nutrition_audit`); err != nil {
		return 0, 0, fmt.Errorf("enrich: clear ingredient_nutrition_audit: %w", err)
	}

	batch := &pgx.Batch{}
	for _, x := range ings {
		tokens := Tokenise(x.name)
		best, score, _, _ := bestMatchFood(tokens, foods, qualifiers)

		if best == nil || score < NutritionThreshold {
			batch.Queue(`
				INSERT INTO ingredient_nutrition_audit
				    (ingredient_id, verdict, used_in_recipes,
				     provider_energy, provider_protein, provider_iron, provider_calcium)
				VALUES ($1,'unmatched',$2,$3,$4,$5,$6)`,
				x.id, x.used, x.energy, x.protein, x.iron, x.ca)
			continue
		}

		var ext struct{ energy, protein, iron, ca *float64 }
		if err := tx.QueryRow(ctx, `
			SELECT energy_kcal_100g, protein_g_100g, iron_mg_100g, calcium_mg_100g
			FROM external_food_composition WHERE food_code = $1`, best.sourceID,
		).Scan(&ext.energy, &ext.protein, &ext.iron, &ext.ca); err != nil {
			return 0, 0, fmt.Errorf("enrich: read composition for %s: %w", best.sourceID, err)
		}

		eD := pctDiff(x.energy, ext.energy)
		pD := pctDiff(x.protein, ext.protein)
		iD := pctDiff(x.iron, ext.iron)
		cD := pctDiff(x.ca, ext.ca)

		verdict := "agrees"
		for _, d := range []any{eD, pD, iD, cD} {
			if f, ok := d.(float64); ok && (f > DiscrepancyPct || f < -DiscrepancyPct) {
				verdict = "discrepancy"
				break
			}
		}

		batch.Queue(`
			INSERT INTO ingredient_nutrition_audit
			    (ingredient_id, food_code, source_key, match_confidence, match_method,
			     provider_energy, external_energy, energy_pct_diff,
			     provider_protein, external_protein, protein_pct_diff,
			     provider_iron, external_iron, iron_pct_diff,
			     provider_calcium, external_calcium, calcium_pct_diff,
			     verdict, used_in_recipes, match_certainty)
			VALUES ($1,$2,'IFCT-2017',$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
			x.id, best.sourceID, round4(score),
			"jaccard over normalised food-name tokens, gated on full coverage of the provider name",
			x.energy, ext.energy, eD,
			x.protein, ext.protein, pD,
			x.iron, ext.iron, iD,
			x.ca, ext.ca, cD,
			verdict, x.used, certainty(score))
		matched++
	}
	if err := sendBatch(ctx, tx, batch, "ingredient_nutrition_audit"); err != nil {
		return 0, 0, err
	}
	return matched, total, nil
}

// certainty separates "is this the same food?" from "do the numbers agree?". A match on
// every word of the provider's name is exact; anything looser names a variety or cut the
// provider did not specify and needs a human to confirm the food first.
func certainty(score float64) string {
	if score >= 0.999 {
		return "exact"
	}
	return "probable"
}

// pctDiff is (provider - external) / external * 100. It returns nil when the external
// value is missing or zero, because a percentage against nothing is not a number.
func pctDiff(provider float64, external *float64) any {
	if external == nil || *external == 0 {
		return nil
	}
	return round4((provider - *external) / *external * 100)
}

func round4(f float64) float64 {
	return float64(int64(f*10000+sign(f)*0.5)) / 10000
}

func sign(f float64) float64 {
	if f < 0 {
		return -1
	}
	return 1
}

// bestMatchFood is bestMatch with the food-form guard: a composition entry carrying a
// qualifier the provider name lacks is a different food, not a weaker match.
func bestMatchFood(tokens []string, pool []candidate, qualifiers map[string]bool) (*candidate, float64, []string, string) {
	have := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		have[t] = true
	}

	var (
		best   *candidate
		bestSc float64
		shared []string
	)
	for i := range pool {
		if Cover(tokens, pool[i].tokens) < NutritionCoverRequired {
			continue
		}
		if hasUnmatchedQualifier(pool[i].tokens, have, qualifiers) {
			continue
		}
		j, sh := Jaccard(tokens, pool[i].tokens)
		// Prefer the entry with the fewest extra qualifier words. Jaccard already leans
		// that way; the tie-break makes "Rice, raw, milled" beat an equally scoring but
		// longer name rather than depending on row order.
		if j > bestSc || (j == bestSc && best != nil && len(pool[i].tokens) < len(best.tokens)) {
			best, bestSc, shared = &pool[i], j, sh
		}
	}
	if shared == nil {
		shared = []string{}
	}
	return best, bestSc, shared, ""
}

func hasUnmatchedQualifier(candidateTokens []string, providerHas, qualifiers map[string]bool) bool {
	for _, t := range candidateTokens {
		if qualifiers[t] && !providerHas[t] {
			return true
		}
	}
	return false
}

func loadQualifiers(ctx context.Context, tx pgx.Tx) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `SELECT qualifier FROM food_form_qualifier`)
	if err != nil {
		return nil, fmt.Errorf("enrich: read food form qualifiers: %w", err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var q string
		if err := rows.Scan(&q); err != nil {
			return nil, fmt.Errorf("enrich: read food form qualifiers: %w", err)
		}
		// Store the singularised form so it lines up with tokenised names.
		out[singular(q)] = true
	}
	return out, rows.Err()
}

// loadFormatRules reads the hand-written format map.
func loadFormatRules(ctx context.Context, tx pgx.Tx) ([]formatRule, error) {
	rows, err := tx.Query(ctx, `SELECT format_pattern, name_keywords FROM external_format_map`)
	if err != nil {
		return nil, fmt.Errorf("enrich: read format map: %w", err)
	}
	defer rows.Close()

	var out []formatRule
	for rows.Next() {
		var pattern string
		var kw []string
		if err := rows.Scan(&pattern, &kw); err != nil {
			return nil, fmt.Errorf("enrich: read format map: %w", err)
		}
		re, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			return nil, fmt.Errorf("enrich: format pattern %q does not compile: %w", pattern, err)
		}
		out = append(out, formatRule{pattern: re, keywords: kw})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("enrich: read format map: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("enrich: external_format_map is empty; no recipe could be matched")
	}
	return out, nil
}

// formatKeywords returns the external dish names allowed for a provider recipe, or nil
// when its format is not mapped.
func formatKeywords(recipeName string, rules []formatRule) []string {
	var out []string
	for _, r := range rules {
		if r.pattern.MatchString(recipeName) {
			out = append(out, r.keywords...)
		}
	}
	return out
}

// nameHasKeyword matches on word boundaries rather than raw substrings. A plain
// strings.Contains matched "adai" inside "Vadai" and offered a deep-fried lentil snack
// as the method for an infant pancake.
func nameHasKeyword(name string, keywords []string) bool {
	lower := " " + nonWordRun.ReplaceAllString(strings.ToLower(name), " ") + " "
	for _, k := range keywords {
		if strings.Contains(lower, " "+k+" ") || strings.Contains(lower, " "+k+"s ") {
			return true
		}
	}
	return false
}

var nonWordRun = regexp.MustCompile(`[^a-z0-9]+`)

// loadExclusions reads the dish names that are never a valid pediatric method source.
func loadExclusions(ctx context.Context, tx pgx.Tx) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT keyword FROM external_method_exclusion`)
	if err != nil {
		return nil, fmt.Errorf("enrich: read method exclusions: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("enrich: read method exclusions: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
