package enrich

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// kJPerKcal converts the IFCT energy column, which is stored in kilojoules.
const kJPerKcal = 4.184

// gToMg converts the IFCT mineral columns, which are stored in grams per 100 g.
const gToMg = 1000.0

// panIndia is the sentinel region for external recipes labelled only "Indian". They are
// usable for any region but rank below a region-specific match.
const panIndia = "Pan-India"

// loadExternalRecipes reads the Indian recipe corpus and stores only the rows whose
// cuisine maps to a region we serve. Out-of-scope cuisines are counted and dropped: a
// Continental recipe is not a source of pediatric South Asian method text no matter how
// well its ingredients happen to overlap.
func loadExternalRecipes(ctx context.Context, tx pgx.Tx, dataDir string) (int, int, error) {
	cuisineRegion, err := cuisineMap(ctx, tx)
	if err != nil {
		return 0, 0, err
	}
	inScope, err := scopeRegions(ctx, tx)
	if err != nil {
		return 0, 0, err
	}

	path := filepath.Join(dataDir, "indian_food_dataset.csv")
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("enrich: open recipe corpus: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	head, err := r.Read()
	if err != nil {
		return 0, 0, fmt.Errorf("enrich: read recipe corpus header: %w", err)
	}
	col := map[string]int{}
	for i, h := range head {
		col[strings.TrimSpace(strings.TrimPrefix(h, "\ufeff"))] = i
	}
	for _, need := range []string{"TranslatedRecipeName", "Cuisine", "TranslatedInstructions", "Cleaned-Ingredients"} {
		if _, ok := col[need]; !ok {
			return 0, 0, fmt.Errorf("enrich: recipe corpus is missing column %q; the dataset layout changed", need)
		}
	}

	if _, err := tx.Exec(ctx, `DELETE FROM external_recipe`); err != nil {
		return 0, 0, fmt.Errorf("enrich: clear external_recipe: %w", err)
	}

	var (
		batch          = &pgx.Batch{}
		rowNo, kept    int
		unknownCuisine = map[string]int{}
	)
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, 0, fmt.Errorf("enrich: read recipe corpus row %d: %w", rowNo+2, err)
		}
		rowNo++

		get := func(name string) string {
			i, ok := col[name]
			if !ok || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}

		cuisine := normaliseCuisine(get("Cuisine"))
		region, known := cuisineRegion[cuisine]
		if !known {
			unknownCuisine[cuisine]++
			continue
		}
		if region == "" {
			continue // mapped, but deliberately out of scope
		}
		if region != panIndia && !inScope[region] {
			continue
		}

		instructions := get("TranslatedInstructions")
		name := get("TranslatedRecipeName")
		if instructions == "" || name == "" {
			continue // no method text is the whole point of the join; skip silently useless rows
		}

		cleaned := get("Cleaned-Ingredients")
		raw := get("TranslatedIngredients")
		tokens := Tokenise(cleaned + " " + raw)
		if len(tokens) == 0 {
			continue
		}

		var mins any
		if v, err := strconv.Atoi(get("TotalTimeInMins")); err == nil {
			mins = v
		}

		batch.Queue(`
			INSERT INTO external_recipe
			    (external_recipe_id, source_key, recipe_name, cuisine, region_culture,
			     ingredients_raw, ingredients_cleaned, ingredient_tokens, instructions,
			     total_time_min, url)
			VALUES ($1,'INDIAN-RECIPES',$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			rowNo, name, cuisine, region, nullIfEmpty(raw), nullIfEmpty(cleaned),
			tokens, instructions, mins, nullIfEmpty(get("URL")))
		kept++
	}

	if err := sendBatch(ctx, tx, batch, "external_recipe"); err != nil {
		return 0, 0, err
	}

	// An unmapped cuisine is a real gap: it means the corpus grew a label the seed does
	// not know about, and its recipes are silently unavailable until someone classifies
	// it. Fail rather than quietly under-enrich.
	if len(unknownCuisine) > 0 {
		var names []string
		for c := range unknownCuisine {
			names = append(names, c)
		}
		return rowNo, kept, fmt.Errorf(
			"enrich: %d cuisine labels are not in external_cuisine_region_map: %v; "+
				"add them to migration 0005 (with a region, or NULL for out of scope) and re-run",
			len(names), names)
	}
	return rowNo, kept, nil
}

// loadFoodComposition reads IFCT 2017 and applies the two unit conversions the source
// requires. Both are exact; nothing here is estimated.
func loadFoodComposition(ctx context.Context, tx pgx.Tx, dataDir string) (int, error) {
	path := filepath.Join(dataDir, "ifct2017_index.csv")
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("enrich: open composition table: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	head, err := r.Read()
	if err != nil {
		return 0, fmt.Errorf("enrich: read composition header: %w", err)
	}

	// Headers look like "Iron (Fe); fe" -- the short code after the semicolon is the
	// stable identifier, so index on that rather than on the display name.
	idx := map[string]int{}
	for i, h := range head {
		if p := strings.LastIndex(h, ";"); p >= 0 {
			idx[strings.TrimSpace(h[p+1:])] = i
		}
	}
	for _, need := range []string{"code", "name", "enerc", "protcnt", "fatce", "choavldf", "fibtg", "ca", "fe"} {
		if _, ok := idx[need]; !ok {
			return 0, fmt.Errorf("enrich: composition table is missing column %q; the dataset layout changed", need)
		}
	}

	if _, err := tx.Exec(ctx, `DELETE FROM external_food_composition`); err != nil {
		return 0, fmt.Errorf("enrich: clear external_food_composition: %w", err)
	}

	batch := &pgx.Batch{}
	n := 0
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("enrich: read composition row %d: %w", n+2, err)
		}

		cell := func(key string) string {
			i, ok := idx[key]
			if !ok || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}
		num := func(key string, scale float64) any {
			v := cell(key)
			if v == "" {
				return nil
			}
			x, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil
			}
			return x * scale
		}

		code, name := cell("code"), cell("name")
		if code == "" || name == "" {
			continue
		}

		var kcal any
		if kj := num("enerc", 1); kj != nil {
			kcal = kj.(float64) / kJPerKcal
		}

		batch.Queue(`
			INSERT INTO external_food_composition
			    (food_code, source_key, food_name, scientific_name, local_names, food_group,
			     energy_kj_100g, energy_kcal_100g, protein_g_100g, fat_g_100g, carb_g_100g,
			     fibre_g_100g, calcium_mg_100g, iron_mg_100g, zinc_mg_100g, vitc_mg_100g,
			     name_tokens)
			VALUES ($1,'IFCT-2017',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
			code, name, nullIfEmpty(cell("scie")), nullIfEmpty(cell("lang")), nullIfEmpty(cell("grup")),
			num("enerc", 1), kcal, num("protcnt", 1), num("fatce", 1), num("choavldf", 1),
			num("fibtg", 1), num("ca", gToMg), num("fe", gToMg), num("zn", gToMg),
			num("vitc", gToMg), Tokenise(name))
		n++
	}

	if err := sendBatch(ctx, tx, batch, "external_food_composition"); err != nil {
		return 0, err
	}
	return n, nil
}

func cuisineMap(ctx context.Context, tx pgx.Tx) (map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT external_cuisine, coalesce(region_culture, '') FROM external_cuisine_region_map`)
	if err != nil {
		return nil, fmt.Errorf("enrich: read cuisine map: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var c, r string
		if err := rows.Scan(&c, &r); err != nil {
			return nil, fmt.Errorf("enrich: read cuisine map: %w", err)
		}
		out[normaliseCuisine(c)] = r
	}
	return out, rows.Err()
}

func scopeRegions(ctx context.Context, tx pgx.Tx) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `SELECT region_culture FROM region_focus WHERE enrichment_scope`)
	if err != nil {
		return nil, fmt.Errorf("enrich: read region scope: %w", err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, fmt.Errorf("enrich: read region scope: %w", err)
		}
		out[r] = true
	}
	return out, rows.Err()
}

// normaliseCuisine strips the byte-order marks and stray whitespace the corpus carries
// on some labels ("Gujarati Recipes" carries a
// trailing U+FEFF in the source CSV).
func normaliseCuisine(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\ufeff' || r == '\u200b' {
			return -1
		}
		return r
	}, s))
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func sendBatch(ctx context.Context, tx pgx.Tx, batch *pgx.Batch, what string) error {
	if batch.Len() == 0 {
		return nil
	}
	br := tx.SendBatch(ctx, batch)
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			br.Close() //nolint:errcheck // the batch error is the one that matters
			return fmt.Errorf("enrich: write %s row %d: %w", what, i+1, err)
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("enrich: write %s: %w", what, err)
	}
	return nil
}
