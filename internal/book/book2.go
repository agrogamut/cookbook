package book

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/engine"
	"github.com/madamgy/recipie/internal/models"
	"github.com/madamgy/recipie/internal/profile"
)

// ErrBlocked is returned by both assemblers when generation is stopped for this child. A
// blocked result must never become a book: the special-care stop gate exists because the
// feeding decision for those children is a clinician's, and a book issued in the child's
// name is exactly the artifact that would override it.
//
// Both books, not only the recipe book. The provider's own wording is "Condition is a STOP
// GATE, not a simple recipe filter", and a Book 1 carrying general-population milestone
// tables under the name of a child with a STOP-REVIEW diagnosis is the same override in a
// different binding.
var ErrBlocked = errors.New("book: engine blocked generation")

// methodStepPattern splits the provider's numbered preparation text ("1) ... 2) ... 3) ...")
// into individual steps. Whatever the numbering, the words themselves are the provider's own
// -- this only breaks one string into several, it never rewrites a syllable of it.
var methodStepPattern = regexp.MustCompile(`\d+\)\s*`)

// maxRecipesPerSection is a project policy decision, not provider data: every row of
// meal_category_target.default_target_recipes reads 25 (verified live), and every chapter of
// this book prints at most 10 of the real, already-ranked-best-first candidates instead. The
// recipes themselves are unaffected -- still real, still ranked the same way -- this only
// decides how many of them make the printed page. Applied in two places: loadMealCategories
// caps the target itself so topUpInvented never drafts recipes 11-25 that would just be
// discarded, and the per-category loop below truncates the final card list to the same
// number regardless of how many real survivors the engine returned.
const maxRecipesPerSection = 10

// mainMealCategories restricts Book 2 to the three chapters with real recipe_master.meal_type
// coverage -- Breakfast, Lunch, Dinner (MC-01, MC-03, MC-06) -- rather than attempting all
// seven meal_category_target rows. This is a project policy decision, not provider data.
//
// meal_category_recipe_map (migration 0016) deliberately leaves the other four meal_type
// values unmapped pending a provider ruling (GAP-023): Snack, School Tiffin and Recovery Meal
// have no counterpart in meal_category_target at all, so Mid-morning, Tiffin/school snack,
// Evening snack and Supper/bedtime (MC-02, MC-04, MC-05, MC-07) always start with zero real
// candidates, for every child, in every region -- not a symptom of any one region's corpus
// being thin. Left unrestricted, every one of those four chapters needs 100% AI-invented
// content: real, live testing against a Google Form intake (2026-08-26, see CLAUDE.md) measured
// this costing 11m7s end to end on real recipes for a single request, most of it serial
// Gemini calls one invented recipe at a time, some of which hit a transient upstream 503 and
// left a chapter short by design rather than by bug. Restricting to the three mapped chapters
// means every printed recipe traces to a real corpus row and removes that latency and
// reliability cost entirely, at the cost of a shorter book: up to 30 recipes (3 chapters x
// maxRecipesPerSection) rather than up to 70.
//
// This does not touch topUpInvented itself, which still exists and is still exercised (see
// internal/book/invented_test.go) for the narrower, still-real case of a mapped chapter -- one
// of these three -- where a specific child's own age/allergy/diet/clinical filters happen to
// exclude every real candidate. That is a per-child edge case within a real chapter, not a
// whole chapter with structurally zero coverage; the two are different problems and only the
// second one is what mainMealCategories closes off.
//
// Revisit this once the provider rules on the three unmapped meal_type values (GAP-023) and/or
// topUpInvented gets bounded concurrency (see printTimeout's own comment in
// internal/api/router.go) -- either change makes the excluded four chapters cheap and reliable
// enough to reconsider.
var mainMealCategories = map[string]bool{"MC-01": true, "MC-03": true, "MC-06": true}

// mealCategory is one row of meal_category_target, with default_target_recipes parsed to an
// int and capped at maxRecipesPerSection. The column is text in the workbook; a value this
// code cannot parse is treated the same as an over-target value -- capped to
// maxRecipesPerSection rather than left uncapped, per the policy above.
type mealCategory struct {
	ID     string
	Name   string
	Target int
}

// AssembleOption configures optional dependencies AssembleBook2 does not require to run.
// A functional-option tail rather than a new positional parameter, deliberately: every
// existing caller (the API handlers, every prior test) keeps compiling unchanged, and a
// caller that wants Gemini-backed drafting opts in explicitly with WithDrafter.
type AssembleOption func(*assembleOptions)

type assembleOptions struct {
	drafter aidraft.Drafter
}

// WithDrafter supplies the Drafter AssembleBook2 uses for clinical modification notes and the
// invented-recipe fallback. Omitted, AssembleBook2 behaves exactly as it did before either
// feature existed: aidraft.Disabled reports ErrDraftingUnavailable on every call, so no note
// is ever attached and a short chapter is reported the same way GAP-023 always has been.
func WithDrafter(d aidraft.Drafter) AssembleOption {
	return func(o *assembleOptions) { o.drafter = d }
}

// AssembleBook2 builds the recipe book for one child.
//
// A chapter with no recipes is omitted rather than rendered empty, and the omission is
// reported. Four of the seven meal categories have no mapped recipes today (GAP-023), and
// meal_category_target.include_logic marks several of them conditional anyway, so an omitted
// chapter is frequently the correct output rather than a failure.
//
// RotationPlan is never built here: assembling a seven-day rotation from a category that may
// hold as few as one surviving recipe would mean repeating a recipe or inventing a schedule
// this project has no data to construct honestly, so it stays nil (its documented meaning
// in types.go) until a real rotation logic is designed against real diversity data.
func AssembleBook2(ctx context.Context, pool *pgxpool.Pool, s profile.Stored, asOf time.Time, opts ...AssembleOption) (Book2, []string, error) {
	cfg := assembleOptions{drafter: aidraft.Disabled}
	for _, opt := range opts {
		opt(&cfg)
	}

	cp, dropped, err := s.ToChildProfile(asOf)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: derive engine input: %w", err)
	}

	// One preliminary run, with no meal type set, purely for the special-care/clinical block
	// check -- blocking does not depend on meal type, so this is the cheapest way to catch a
	// blocked child before doing any per-category work below.
	res, err := engine.Run(ctx, pool, cp)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: run engine: %w", err)
	}
	if res.Blocked {
		return Book2{}, nil, fmt.Errorf("%w: %s", ErrBlocked, res.BlockReason)
	}

	// Read once, outside the per-category loop: which rules matter is a property of the
	// child's own ClinicalFlags, not of any one chapter, and drafting the same note twice
	// for two recipes sharing a clinical tag would mean two Gemini calls for identical input.
	clinicalActions, err := engine.ActiveClinicalRuleActions(ctx, pool, cp)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: active clinical rule actions: %w", err)
	}

	version, err := recipeMasterVersion(ctx, pool)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: recipe version (GAP-024): %w", err)
	}

	boilerplate, err := boilerplateMethods(ctx, pool)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: boilerplate methods (GAP-001): %w", err)
	}

	ageStages, err := ageStagesByAgeGroup(ctx, pool)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: age stage map: %w", err)
	}

	categories, err := loadMealCategories(ctx, pool)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: load meal categories: %w", err)
	}

	safetySOP, err := loadFoodSafetySOP(ctx, pool)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: load food safety sop: %w", err)
	}

	honeyRule, chokingHazards, err := loadFeedingSafetyGuidance(ctx, pool)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: load feeding safety guidance: %w", err)
	}

	bengaliNames, err := ingredientBengaliNames(ctx, pool)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: load ingredient bengali names: %w", err)
	}

	mapped, err := loadMealCategoryRecipeIDs(ctx, pool)
	if err != nil {
		return Book2{}, nil, fmt.Errorf("book: load meal category recipe map: %w", err)
	}

	skipped := append([]string{}, dropped...)
	sections := []MealSection{}

	for _, cat := range categories {
		if !mainMealCategories[cat.ID] {
			// mainMealCategories's own doc comment has the full reasoning; the short version
			// for an operator reading this list is that it is a scope decision, not a data
			// gap -- unlike every other skip entry, retrying or fixing an input on this child
			// cannot change the outcome, so the wording deliberately does not say GAP-023 or
			// invite a retry.
			skipped = append(skipped, omissionMealCategory+fmt.Sprintf(
				"%s (%s) is out of scope for this book by project policy -- Book 2 covers "+
					"Breakfast, Lunch and Dinner only", cat.ID, cat.Name))
			continue
		}
		candidateIDs := mapped[cat.ID]
		if len(candidateIDs) == 0 {
			// mainMealCategories already keeps this loop off the four categories GAP-023
			// names as structurally unmapped (Mid-morning, Tiffin/school snack, Evening
			// snack, Supper/bedtime), so reaching this branch for one of the three in-scope
			// categories would mean the corpus itself changed underneath this code -- Lunch
			// losing every one of its 199 real recipes, say. Kept as a defensive path rather
			// than an assumed-unreachable one: topUpInvented starts from an empty slice and
			// tries to reach the whole target from nothing but the allow-listed ingredients,
			// exactly like it would for a genuinely unmapped category.
			cards, note := topUpInvented(ctx, pool, cfg.drafter, cp, cat, version, res, nil)
			if len(cards) == 0 {
				skipped = append(skipped, omissionMealCategory+fmt.Sprintf(
					"%s (%s) has no recipes mapped to it at all",
					cat.ID, cat.Name))
				continue
			}
			if note != "" {
				skipped = append(skipped, note)
			}
			sections = append(sections, MealSection{
				MealCategoryID: cat.ID, Title: cat.Name, TargetRecipeCount: cat.Target, Recipes: cards,
			})
			continue
		}

		// Run the engine again, this time scoped to this category's own meal type -- the
		// per-meal-type invocation path internal/api/handlers/search.go already uses. This
		// is what makes each chapter's target independently reachable: capToTarget (step 13)
		// caps whatever candidate pool it is handed, and handing it the whole book's pool
		// once, before any category split, is what silently ceilinged every book at 25
		// recipes total instead of 25 per chapter. cat.Name matches meal_category_target's
		// own meal_category column exactly, which is what both applyMealFilter and
		// capToTarget key their lookups on.
		catCP := cp
		catCP.MealType = cat.Name
		catRes, err := engine.Run(ctx, pool, catCP)
		if err != nil {
			return Book2{}, nil, fmt.Errorf("book: run engine for %s: %w", cat.ID, err)
		}

		// rank orders this category's own candidates, best first. byID recovers the
		// engine's own recorded region and diet fields for a recipe, which is where
		// selection_reasons comes from -- the engine's accounting, not new prose.
		rank := make(map[string]int, len(catRes.Recipes))
		byID := make(map[string]models.RankedRecipe, len(catRes.Recipes))
		for i, r := range catRes.Recipes {
			rank[r.RecipeID] = i
			byID[r.RecipeID] = r
		}

		var survivors []string
		for _, id := range candidateIDs {
			if _, ok := rank[id]; ok {
				survivors = append(survivors, id)
			}
		}
		if len(survivors) == 0 {
			cards, note := topUpInvented(ctx, pool, cfg.drafter, cp, cat, version, res, nil)
			if len(cards) == 0 {
				skipped = append(skipped, omissionMealCategory+fmt.Sprintf(
					"%s (%s) has %d recipes mapped to it, but none survived "+
						"this child's age, allergy, clinical or diet filters",
					cat.ID, cat.Name, len(candidateIDs)))
				continue
			}
			if note != "" {
				skipped = append(skipped, note)
			}
			sections = append(sections, MealSection{
				MealCategoryID: cat.ID, Title: cat.Name, TargetRecipeCount: cat.Target, Recipes: cards,
			})
			continue
		}

		// Ranked best-first, the engine's own ordering. No cap applied here: catRes.Recipes
		// is already capped to this category's own target by capToTarget inside engine.Run,
		// so survivors can never exceed cat.Target.
		sort.Slice(survivors, func(i, j int) bool { return rank[survivors[i]] < rank[survivors[j]] })

		cards, cardSkips, err := loadRecipeCards(ctx, pool, survivors, cat.ID, version, boilerplate, ageStages, bengaliNames, byID, cp, catRes, cfg.drafter, clinicalActions)
		if err != nil {
			return Book2{}, nil, fmt.Errorf("book: load recipe cards for %s: %w", cat.ID, err)
		}
		skipped = append(skipped, cardSkips...)
		if len(cards) == 0 {
			// Every id in survivors came from the engine's own result, so the join to
			// recipe_method_card/recipe_master should never drop one on real data -- reaching
			// this means an id is orphaned somewhere upstream. Still worth a top-up attempt
			// before reporting the omission, on the same footing as the other two empty-start
			// branches above.
			cards, note := topUpInvented(ctx, pool, cfg.drafter, cp, cat, version, res, nil)
			if len(cards) == 0 {
				skipped = append(skipped, omissionMealCategory+fmt.Sprintf(
					"%s (%s) had %d surviving candidates but none could be "+
						"loaded as a recipe card", cat.ID, cat.Name, len(survivors)))
				continue
			}
			if note != "" {
				skipped = append(skipped, note)
			}
			sections = append(sections, MealSection{
				MealCategoryID: cat.ID, Title: cat.Name, TargetRecipeCount: cat.Target, Recipes: cards,
			})
			continue
		}

		// The chapter already has at least one real card at this point, so a shortfall here is
		// a note on a rendered chapter, never a whole-category omission -- topUpInvented's note
		// must never carry the omissionMealCategory prefix, or the conservation tests'
		// "rendered + reported == total" accounting would double-count this category.
		var note string
		cards, note = topUpInvented(ctx, pool, cfg.drafter, cp, cat, version, res, cards)
		if note != "" {
			skipped = append(skipped, note)
		}

		sections = append(sections, MealSection{
			MealCategoryID:    cat.ID,
			Title:             cat.Name,
			TargetRecipeCount: cat.Target,
			Recipes:           cards,
		})
	}

	// Belt and suspenders on maxRecipesPerSection: loadMealCategories caps cat.Target so
	// topUpInvented never drafts past it, but the engine's own capToTarget (internal/engine/
	// rank.go) re-queries meal_category_target independently and can still hand back up to the
	// provider's uncapped 25 real survivors. This is the one place that actually enforces the
	// printed limit regardless of how many candidates arrived. Ranked best-first already, so
	// truncating keeps the strongest matches.
	for i := range sections {
		if len(sections[i].Recipes) > maxRecipesPerSection {
			sections[i].Recipes = sections[i].Recipes[:maxRecipesPerSection]
		}
		if sections[i].TargetRecipeCount > maxRecipesPerSection {
			sections[i].TargetRecipeCount = maxRecipesPerSection
		}
	}

	numberBook(sections)

	b := Book2{
		Metadata: Metadata{
			Title:          "My Child's Personalized Recipe Book",
			BookVersion:    "V1",
			GenerationDate: asOf,
			Language:       "en",
			Logo:           logoDataURI,
		},
		Child: ChildSummary{
			DisplayName:   s.DisplayName,
			AgeMonths:     cp.AgeMonths,
			AgeLabel:      ageLabel(cp.AgeMonths),
			FoodPractice:  cp.DietType,
			AllergyStatus: allergyStatus(cp.Allergens, cp.SuspectedAllergens),
		},
		SafetySOP:      safetySOP,
		HoneyRule:      honeyRule,
		ChokingHazards: chokingHazards,
		MealSections:   sections,
		RotationPlan:   nil,
	}
	return b, skipped, nil
}

// recipeMasterVersion reads the content hash of the most recent import of recipe_master.
// See GAP-024 (migration 0017): the provider ships no version column, so this derived value
// stands in for one and is always labelled as an import hash, never as a provider version.
func recipeMasterVersion(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var hash string
	err := pool.QueryRow(ctx, `
		SELECT content_hash FROM import_table_stat
		WHERE table_name = 'recipe_master'
		  AND run_id = (SELECT max(run_id) FROM import_table_stat WHERE table_name = 'recipe_master')`).
		Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("no import run recorded for recipe_master; run the importer first")
	}
	if err != nil {
		return "", fmt.Errorf("query content hash: %w", err)
	}
	return hash, nil
}

// boilerplateMethods returns the set of preparation_method_full texts that more than one
// recipe shares -- the GAP-001 measure, computed the same way internal/importer/gaps.go
// counts it, so a card's MethodIsProviderBoilerplate flag agrees with the register.
func boilerplateMethods(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	rows, err := pool.Query(ctx, `
		SELECT preparation_method_full FROM recipe_master
		GROUP BY preparation_method_full
		HAVING count(*) > 1`)
	if err != nil {
		return nil, fmt.Errorf("query boilerplate methods: %w", err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var method string
		if err := rows.Scan(&method); err != nil {
			return nil, fmt.Errorf("scan boilerplate method: %w", err)
		}
		out[method] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("boilerplate method rows: %w", err)
	}
	return out, nil
}

// ageStagesByAgeGroup maps recipe_master.age_group to the Age/Feeding Stage master's stage
// codes (AF01..AF09) via age_feeding_stage_master.recipe_master_age_group, the join CLAUDE.md
// already lists as verified clean ("covers all 7 recipe age groups"). Several stage codes can
// share one age_group (AF04/AF05 both read "2-5 years", AF08/AF09 both read "13-18 years"),
// so a recipe legitimately carries more than one age_stage_id.
func ageStagesByAgeGroup(ctx context.Context, pool *pgxpool.Pool) (map[string][]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT recipe_master_age_group, stage_code
		FROM age_feeding_stage_master
		WHERE recipe_master_age_group IS NOT NULL
		ORDER BY stage_code`)
	if err != nil {
		return nil, fmt.Errorf("query age stage map: %w", err)
	}
	defer rows.Close()

	out := map[string][]string{}
	for rows.Next() {
		var ageGroup, stageCode string
		if err := rows.Scan(&ageGroup, &stageCode); err != nil {
			return nil, fmt.Errorf("scan age stage row: %w", err)
		}
		out[ageGroup] = append(out[ageGroup], stageCode)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("age stage rows: %w", err)
	}
	return out, nil
}

// ingredientBengaliNames maps ingredient_master.ingredient_id to its bengali_name, when the
// provider supplied one. 406 of 406 ingredients carry a Bengali name today; an id with none
// (or not found here) renders with the English name alone, never a transliteration this
// project has no source for.
func ingredientBengaliNames(ctx context.Context, pool *pgxpool.Pool) (map[string]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT ingredient_id, coalesce(bengali_name, '')
		FROM ingredient_master`)
	if err != nil {
		return nil, fmt.Errorf("query ingredient bengali names: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var id, bengali string
		if err := rows.Scan(&id, &bengali); err != nil {
			return nil, fmt.Errorf("scan ingredient bengali name: %w", err)
		}
		if bengali != "" {
			out[id] = bengali
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ingredient bengali name rows: %w", err)
	}
	return out, nil
}

// loadFoodSafetySOP reads all 8 rows of the provider's food_safety_sop table, left-joined to
// evidence_reference_master for citation. This is static reference content -- the same 8 rows
// for every child -- confirmed unused anywhere else in this codebase before this call was added.
func loadFoodSafetySOP(ctx context.Context, pool *pgxpool.Pool) ([]SafetyGuideline, error) {
	rows, err := pool.Query(ctx, `
		SELECT s.sop_id, s.area, s.rule, s.status,
		       coalesce(e.title, ''), coalesce(e.authority, ''),
		       coalesce(e.year, ''), coalesce(e.source_url, '')
		FROM food_safety_sop s
		LEFT JOIN evidence_reference_master e ON e.evidence_id = s.evidence_id
		ORDER BY s.sop_id`)
	if err != nil {
		return nil, fmt.Errorf("query food safety sop: %w", err)
	}
	defer rows.Close()

	var out []SafetyGuideline
	for rows.Next() {
		var g SafetyGuideline
		if err := rows.Scan(&g.SOPID, &g.Area, &g.Rule, &g.Status,
			&g.EvidenceTitle, &g.EvidenceAuthority, &g.EvidenceYear, &g.SourceURL); err != nil {
			return nil, fmt.Errorf("scan food safety sop: %w", err)
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("food safety sop rows: %w", err)
	}
	return out, nil
}

// loadFeedingSafetyGuidance derives two static, generic (not per-child) facts from
// age_feeding_stage_master -- real per-stage columns the engine already reads for ranking,
// never rendered as guidance before this. Static across every book: this is general infant-
// feeding safety, not this child's own row.
//
// honeyRule is a documented derivation, not a hardcoded fact: it checks that every one of the
// table's stages agrees on a single threshold (excluded below 12 months, allowed at or after)
// before stating it as one sentence, and returns "" -- an honest gap, never a guessed
// threshold -- if a future row ever disagrees with that pattern. This is the hard rule's third
// category applied literally: the formula is "confirm the real per-stage rows imply one
// threshold, then state the threshold," the source is age_feeding_stage_master, and the check
// is right here rather than assumed.
//
// chokingHazards is not a derivation -- it is the distinct, verbatim key_choking_control text
// from every stage the table itself flags "High" or "Moderate-High" (a plain string match,
// verified live: 4 distinct rows), presented as a list rather than merged into one invented
// sentence, so nothing is stated more precisely than the source data actually says.
func loadFeedingSafetyGuidance(ctx context.Context, pool *pgxpool.Pool) (honeyRule string, chokingHazards []string, err error) {
	rows, err := pool.Query(ctx, `
		SELECT age_from_months, coalesce(honey_rule, '')
		FROM age_feeding_stage_master
		ORDER BY age_from_months`)
	if err != nil {
		return "", nil, fmt.Errorf("query honey rule by stage: %w", err)
	}
	agreesOnTwelveMonths := true
	sawAny := false
	for rows.Next() {
		var ageFrom int
		var rule string
		if err := rows.Scan(&ageFrom, &rule); err != nil {
			rows.Close()
			return "", nil, fmt.Errorf("scan honey rule row: %w", err)
		}
		if rule == "" {
			continue
		}
		sawAny = true
		excluded := strings.Contains(strings.ToUpper(rule), "EXCLUDE")
		allowed := strings.Contains(strings.ToLower(rule), "allow")
		// "Not applicable" appears on the 0-5 month stage (AF00), before any solid food is
		// given at all -- it does not contradict "no honey below 12 months," it is moot for
		// the same reason the rule exists. Treated as agreeing below 12 months; still checked
		// strictly (must be excluded or moot) so a real future disagreement still trips this.
		moot := rule == "Not applicable"
		switch {
		case ageFrom < 12 && !excluded && !moot:
			agreesOnTwelveMonths = false
		case ageFrom >= 12 && excluded:
			agreesOnTwelveMonths = false
		case ageFrom >= 12 && !allowed && !moot:
			agreesOnTwelveMonths = false
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", nil, fmt.Errorf("honey rule rows: %w", err)
	}
	if sawAny && agreesOnTwelveMonths {
		honeyRule = "No honey before 12 months of age."
	}

	hazardRows, err := pool.Query(ctx, `
		SELECT DISTINCT key_choking_control
		FROM age_feeding_stage_master
		WHERE choking_risk_level ILIKE '%high%'
		  AND key_choking_control IS NOT NULL AND key_choking_control <> ''
		ORDER BY key_choking_control`)
	if err != nil {
		return "", nil, fmt.Errorf("query choking hazards: %w", err)
	}
	defer hazardRows.Close()
	for hazardRows.Next() {
		var h string
		if err := hazardRows.Scan(&h); err != nil {
			return "", nil, fmt.Errorf("scan choking hazard: %w", err)
		}
		chokingHazards = append(chokingHazards, h)
	}
	if err := hazardRows.Err(); err != nil {
		return "", nil, fmt.Errorf("choking hazard rows: %w", err)
	}
	return honeyRule, chokingHazards, nil
}

// loadMealCategories reads every row of meal_category_target, real provider data, all seven of
// them -- it does not know about mainMealCategories at all. AssembleBook2's own loop applies
// that policy explicitly, one category at a time, alongside its report-or-render decision for
// every other case a category can end up in -- see the loop for why an excluded category still
// gets an honest skip entry rather than silently vanishing.
func loadMealCategories(ctx context.Context, pool *pgxpool.Pool) ([]mealCategory, error) {
	rows, err := pool.Query(ctx, `
		SELECT meal_category_id, meal_category, coalesce(default_target_recipes, '')
		FROM meal_category_target
		ORDER BY meal_category_id`)
	if err != nil {
		return nil, fmt.Errorf("query meal categories: %w", err)
	}
	defer rows.Close()

	var out []mealCategory
	for rows.Next() {
		var id, name, targetText string
		if err := rows.Scan(&id, &name, &targetText); err != nil {
			return nil, fmt.Errorf("scan meal category: %w", err)
		}
		target, _ := strconv.Atoi(strings.TrimSpace(targetText))
		if target <= 0 || target > maxRecipesPerSection {
			target = maxRecipesPerSection
		}
		out = append(out, mealCategory{ID: id, Name: name, Target: target})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("meal category rows: %w", err)
	}
	return out, nil
}

// loadMealCategoryRecipeIDs reads the whole meal_category_recipe view (Task 1's join of
// meal_category_recipe_map, meal_category_target and recipe_master). A category absent from
// this map has no mapped meal_type at all -- GAP-023 -- and is distinct from a category that
// is mapped but whose recipes were all filtered out by this particular child's profile.
func loadMealCategoryRecipeIDs(ctx context.Context, pool *pgxpool.Pool) (map[string][]string, error) {
	rows, err := pool.Query(ctx, `SELECT meal_category_id, recipe_id FROM meal_category_recipe`)
	if err != nil {
		return nil, fmt.Errorf("query meal category recipe map: %w", err)
	}
	defer rows.Close()

	out := map[string][]string{}
	for rows.Next() {
		var categoryID, recipeID string
		if err := rows.Scan(&categoryID, &recipeID); err != nil {
			return nil, fmt.Errorf("scan meal category recipe row: %w", err)
		}
		out[categoryID] = append(out[categoryID], recipeID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("meal category recipe rows: %w", err)
	}
	return out, nil
}

// loadRecipeCards fetches the full card content for one meal section's chosen recipe ids,
// joining recipe_method_card (provider method text and review status) to recipe_master (the
// fields the card view does not carry) and left-joining recipe_nutrition_recomputed (the
// IFCT-verification band). Following the same shape as
// internal/api/handlers/recipes.go's RecipeDetail: query, scan into a local struct, wrap
// every error at the boundary.
//
// Every id in ids ends up either in the returned cards or named in the returned skip slice --
// never silently absent. The join is 1:1 across all 940 recipes today, so the skip slice is
// empty on current data; it exists so a future join miss is reported rather than rendering a
// chapter one recipe short of what was asked for.
func loadRecipeCards(ctx context.Context, pool *pgxpool.Pool, ids []string, categoryID, version string,
	boilerplate map[string]bool, ageStages map[string][]string, bengaliNames map[string]string,
	byID map[string]models.RankedRecipe, cp models.ChildProfile, res models.EngineResult,
	drafter aidraft.Drafter, clinicalActions []engine.ClinicalRuleAction) ([]RecipeCard, []string, error) {

	rows, err := pool.Query(ctx, `
		SELECT c.recipe_id, c.recipe_name, c.provider_method, c.provider_review_status,
		       r.age_group, r.texture, r.serving_size_g, r.ingredient_ids, r.ingredient_names,
		       r.ingredient_quantities_g,
		       r.safety_rule, r.clinical_tag, r.growth_target, r.prep_time_min, r.cook_time_min,
		       r.budget_band, r.region_culture,
		       n.ingredient_coverage, n.fully_verified,
		       n.energy_kcal, n.protein_g, n.iron_mg, n.calcium_mg, n.total_mass_g, n.formula,
		       coalesce(m.mark_id, ''), coalesce(m.format_label, '')
		FROM recipe_method_card c
		JOIN recipe_master r ON r.recipe_id = c.recipe_id
		LEFT JOIN recipe_nutrition_recomputed n ON n.recipe_id = c.recipe_id
		LEFT JOIN recipe_mark m ON m.recipe_id = c.recipe_id
		WHERE c.recipe_id = ANY($1)`, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("query recipe cards: %w", err)
	}
	defer rows.Close()

	byRecipeID := make(map[string]RecipeCard, len(ids))
	// Modification-note requests are collected here, not drafted inline: a chapter can hold
	// a dozen or more recipes matching an active clinical condition, and one real Gemini
	// call per recipe run serially inside this loop was enough on its own to push real
	// generation time past even the print route's 180s budget once GEMINI_API_KEY was
	// actually wired to production. Drafted concurrently, bounded, after the loop.
	type pendingMod struct {
		recipeID string
		req      aidraft.ModificationRequest
	}
	var pendingMods []pendingMod
	for rows.Next() {
		var (
			recipeID, recipeName, method, reviewStatus    string
			ageGroup, texture, safetyRule                 string
			clinicalTag, growthTarget                     string
			servingSizeG, prepTimeMin, cookTimeMin        int
			budgetBand, regionCulture                     string
			ingredientIDs, ingredientNames, ingredientQty string
			coverage                                      *float64
			fullyVerified                                 *bool
			energyKcal, proteinG, ironMg, calciumMg       *float64
			totalMassG                                    *float64
			formula                                       *string
			markID, formatLabel                           string
		)
		if err := rows.Scan(&recipeID, &recipeName, &method, &reviewStatus,
			&ageGroup, &texture, &servingSizeG, &ingredientIDs, &ingredientNames, &ingredientQty,
			&safetyRule, &clinicalTag, &growthTarget, &prepTimeMin, &cookTimeMin,
			&budgetBand, &regionCulture, &coverage, &fullyVerified,
			&energyKcal, &proteinG, &ironMg, &calciumMg, &totalMassG, &formula,
			&markID, &formatLabel); err != nil {
			return nil, nil, fmt.Errorf("scan recipe card: %w", err)
		}

		prep, cook, band, tex := prepTimeMin, cookTimeMin, budgetBand, texture
		card := RecipeCard{
			RecipeID:                    recipeID,
			RecipeVersion:               version,
			Title:                       recipeName,
			MealCategoryID:              categoryID,
			AgeStageIDs:                 ageStages[ageGroup],
			SelectionReasons:            selectionReasons(res, cp, byID[recipeID]),
			NutritionTags:               nutritionTags(clinicalTag, growthTarget, coverage, fullyVerified),
			PrepTimeMinutes:             &prep,
			CookTimeMinutes:             &cook,
			CostBand:                    &band,
			Ingredients:                 splitIngredients(ingredientIDs, ingredientNames, ingredientQty, bengaliNames),
			MethodSteps:                 splitMethodSteps(method),
			TextureServing:              &tex,
			Serving:                     fmt.Sprintf("%d g", servingSizeG),
			Safety:                      safetyRule,
			ReviewStatus:                reviewStatus,
			MethodIsProviderBoilerplate: boilerplate[method],
			RegionCulture:               regionCulture,
			Meta:                        recipeMeta(prepTimeMin, cookTimeMin, servingSizeG, budgetBand, texture),
			Nutrition: recipeNutrition(energyKcal, proteinG, ironMg, calciumMg,
				totalMassG, coverage, fullyVerified, formula),
			// The drawn mark for this recipe's dish format. Nil when the seed names no mark or
			// this repository carries no artwork for it, in which case the page prints without
			// one rather than with a placeholder -- the same rule the cover portrait follows.
			Mark: Mark(markID, formatLabel),
			// Every card this function builds is a real, provider-authored recipe -- the
			// invented-recipe fallback builds its own card directly (invented.go) and is
			// never routed through here.
			Source: "provider",
		}
		if markID != "" {
			photo, err := RepresentativePhoto(ctx, pool, markID)
			if err != nil {
				return nil, nil, fmt.Errorf("recipe %s: %w", recipeID, err)
			}
			card.Photo = photo
		}
		if req, ok := modificationRequestFor(cp, clinicalActions, recipeName, clinicalTag); ok {
			pendingMods = append(pendingMods, pendingMod{recipeID: recipeID, req: req})
		}
		byRecipeID[recipeID] = card
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("recipe card rows: %w", err)
	}

	// Results land in their own slice, one per pendingMods entry by index -- never written
	// into byRecipeID from inside a goroutine, since concurrent map writes are unsafe even
	// to distinct keys. Merged into byRecipeID single-threaded below, after every goroutine
	// has joined.
	modResults := make([]*aidraft.DraftedText, len(pendingMods))
	draftConcurrently(ctx, len(pendingMods), func(ctx context.Context, i int) {
		note, err := drafter.DraftModificationNote(ctx, pendingMods[i].req)
		if err != nil {
			// Drafting unavailable or the call failed: no note, never a half-built one, and
			// never a reason to fail the card.
			return
		}
		modResults[i] = &note
	})
	for i, p := range pendingMods {
		if modResults[i] == nil {
			continue
		}
		card := byRecipeID[p.recipeID]
		card.ModificationNote = modResults[i]
		byRecipeID[p.recipeID] = card
	}

	// Rebuild in the caller's rank order rather than the query's arbitrary row order, so a
	// chapter's recipes stay best-first. An id with no matching row is named in the skip
	// slice rather than dropped -- a chapter that asked for twelve recipes and got eleven
	// must say so, not just render eleven.
	cards := make([]RecipeCard, 0, len(ids))
	var skipped []string
	for _, id := range ids {
		c, ok := byRecipeID[id]
		if !ok {
			skipped = append(skipped, fmt.Sprintf(
				"recipe %s has no method card row and was not rendered", id))
			continue
		}
		cards = append(cards, c)
	}
	return cards, skipped, nil
}

// selectionReasons draws only from the engine's own recorded decisions -- never invented
// prose. The active nutrition target and why it was chosen come straight from EngineResult;
// the region and diet lines only appear when the ranked recipe's own recorded fields agree
// with what the family declared, so an unstated preference never gets reported as a "match".
func selectionReasons(res models.EngineResult, cp models.ChildProfile, r models.RankedRecipe) []string {
	reasons := []string{
		fmt.Sprintf("Nutrition target %s: %s", res.ActiveTarget, res.TargetReason),
	}
	if cp.RegionCulture != "" && r.RegionCulture == cp.RegionCulture {
		reasons = append(reasons, fmt.Sprintf(
			"Region match: recipe is %s, the family's stated region", r.RegionCulture))
	}
	if cp.DietType != "" && r.DietType == cp.DietType {
		reasons = append(reasons, fmt.Sprintf("Diet practice: %s", r.DietType))
	}
	return reasons
}

// nutritionTags carries only the provider's own tags (clinical_tag, growth_target) plus the
// IFCT-verification band from recipe_nutrition_recomputed -- never a computed nutrition
// figure, only the labels that already exist or that state how verified an existing figure is.
func nutritionTags(clinicalTag, growthTarget string, coverage *float64, fullyVerified *bool) []string {
	var tags []string
	if clinicalTag != "" {
		tags = append(tags, "Clinical tag: "+clinicalTag)
	}
	if growthTarget != "" {
		tags = append(tags, "Growth target: "+growthTarget)
	}
	if fullyVerified != nil {
		if *fullyVerified {
			tags = append(tags, "Nutrition: fully IFCT-verified")
		} else if coverage != nil {
			tags = append(tags, fmt.Sprintf("Nutrition: %.0f%% IFCT-verified by mass", *coverage*100))
		}
	}
	return tags
}

// splitIngredients pairs recipe_master's semicolon-delimited ingredient_ids, ingredient_names
// and ingredient_quantities_g lists, which CLAUDE.md's "Verified clean" section already
// establishes align 1:1 for all 1000 recipes. Names and quantities are transcribed verbatim;
// nothing here estimates a quantity for a name with no matching entry. bengaliNames is keyed
// by ingredient_id rather than by name, because matching on the provider's own id is exact
// where matching on free-text name would not be.
func splitIngredients(ids, names, quantities string, bengaliNames map[string]string) []IngredientLine {
	idParts := strings.Split(ids, ";")
	nameParts := strings.Split(names, ";")
	qtyParts := strings.Split(quantities, ";")
	out := make([]IngredientLine, 0, len(nameParts))
	for i, name := range nameParts {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		line := IngredientLine{Name: name}
		if i < len(qtyParts) {
			if qty := strings.TrimSpace(qtyParts[i]); qty != "" {
				line.Quantity = qty + " g"
			}
		}
		if i < len(idParts) {
			if id := strings.TrimSpace(idParts[i]); id != "" {
				line.Bengali = bengaliNames[id]
			}
		}
		out = append(out, line)
	}
	return out
}

// splitMethodSteps breaks the provider's single preparation_method_full string into the
// numbered steps it already contains ("1) ... 2) ... 3) ..."). This only segments existing
// text; it introduces no new instruction and rewrites no word of the provider's own.
func splitMethodSteps(method string) []string {
	parts := methodStepPattern.Split(method, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		if trimmed := strings.TrimSpace(method); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// recipeMeta builds the production strip a cook reads before starting.
//
// Every value is a recipe_master column that has always been loaded and never printed. A
// zero or empty column yields no cell at all rather than a cell reading "0 min" or a blank:
// prep_time_min snaps to four distinct values across the corpus and cook_time_min to six, so
// a zero there is far more likely to be an unset field than a recipe that takes no time.
func recipeMeta(prepMin, cookMin, servingG int, budgetBand, texture string) []RecipeMeta {
	var out []RecipeMeta
	if prepMin > 0 {
		out = append(out, RecipeMeta{Label: "Prep", Value: fmt.Sprintf("%d min", prepMin), Mono: true})
	}
	if cookMin > 0 {
		out = append(out, RecipeMeta{Label: "Cook", Value: fmt.Sprintf("%d min", cookMin), Mono: true})
	}
	if servingG > 0 {
		out = append(out, RecipeMeta{Label: "Serving", Value: fmt.Sprintf("%d g", servingG), Mono: true})
	}
	if texture != "" {
		out = append(out, RecipeMeta{Label: "Texture", Value: texture})
	}
	if budgetBand != "" {
		out = append(out, RecipeMeta{Label: "Cost band", Value: budgetBand})
	}
	return out
}

// recipeNutrition formats the recomputed figures for the page, or returns nil.
//
// nil rather than a zeroed struct whenever the left join found no row or the view supplied
// no energy: a recipe with no recomputed nutrition prints no panel, because "0 kcal" is a
// claim and an absent panel is the truth. The figures are totals for the listed ingredients
// and are never divided down to a serving -- see RecipeNutrition.BasisG for why.
func recipeNutrition(energy, protein, iron, calcium, mass, coverage *float64,
	fullyVerified *bool, formula *string) *RecipeNutrition {

	if energy == nil || mass == nil || coverage == nil {
		return nil
	}
	n := &RecipeNutrition{
		EnergyKcal:  fmt.Sprintf("%.0f", *energy),
		BasisG:      fmt.Sprintf("%.0f g", *mass),
		Coverage:    *coverage,
		CoveragePct: int(*coverage * 100),
	}
	if protein != nil {
		n.ProteinG = fmt.Sprintf("%.1f", *protein)
	}
	if iron != nil {
		n.IronMg = fmt.Sprintf("%.1f", *iron)
	}
	if calcium != nil {
		n.CalciumMg = fmt.Sprintf("%.0f", *calcium)
	}
	if fullyVerified != nil {
		n.FullyVerified = *fullyVerified
	}
	if formula != nil {
		n.Formula = *formula
	}
	// A coverage that rounds up to 100% on a recipe the view did not mark fully verified
	// would read as measured when it is not. The frontend suite pins the same rule on the
	// console; it holds here for the same reason and on paper it is the harder one to undo.
	if !n.FullyVerified && n.CoveragePct >= 100 {
		n.CoveragePct = 99
	}
	return n
}

// numberBook walks the assembled chapters and stamps the display numbers the pages print:
// chapters from 1 across the book, recipes from 1 across the whole book rather than
// restarting per chapter.
//
// Continuous rather than per-chapter because the number is the book's navigation key -- the
// contents page cannot carry page numbers, since Chromium assigns those at print time and no
// template can learn them -- and "recipe 7" has to name one page, not one page per chapter.
func numberBook(sections []MealSection) {
	recipe := 0
	for i := range sections {
		sections[i].Number = i + 1
		for j := range sections[i].Recipes {
			recipe++
			sections[i].Recipes[j].Number = recipe
		}
	}
}

// ChapterRecipeIndex reads Book 1's "Recipe Link Index" straight off an already-assembled
// Book 2 -- one entry per non-empty chapter, real numbering from numberBook, real RecipeIDs.
//
// Deliberately not recomputed independently: a second, separate call into the engine (or
// worse, a second topUpInvented/Gemini call for a short chapter) could legitimately select
// or invent a different recipe than the one actually printed in the Book 2 sitting next to
// it, making the cross-reference point at content that does not exist in that specific
// generated copy. This only ever reads MealSections that already survived one real run.
func ChapterRecipeIndex(sections []MealSection) []ChapterRange {
	var out []ChapterRange
	for _, sec := range sections {
		if len(sec.Recipes) == 0 {
			continue
		}
		ids := make([]string, len(sec.Recipes))
		for i, r := range sec.Recipes {
			ids[i] = r.RecipeID
		}
		out = append(out, ChapterRange{
			ChapterNumber:     sec.Number,
			Title:             sec.Title,
			FirstRecipeNumber: sec.Recipes[0].Number,
			LastRecipeNumber:  sec.Recipes[len(sec.Recipes)-1].Number,
			RecipeCount:       len(sec.Recipes),
			RecipeIDs:         ids,
		})
	}
	return out
}

// WeeklyMealPlanFromSections builds B1-006's "This week's plan" straight off an
// already-assembled Book 2 -- the same source ChapterRecipeIndex reads, for the same reason:
// a second, independent engine call could legitimately select a different recipe than the one
// actually printed in the Book 2 sitting next to this table, and "Dish (from Book 2)" only
// means something if it names something that specific generated copy actually contains.
// Categories with zero recipes (a mapped chapter emptied by this child's own filters) are left
// out rather than printed as an empty group.
func WeeklyMealPlanFromSections(sections []MealSection) []MealPlanCategory {
	var out []MealPlanCategory
	for _, sec := range sections {
		if len(sec.Recipes) == 0 {
			continue
		}
		cat := MealPlanCategory{Title: sec.Title, Rows: make([]MealPlanRow, len(sec.Recipes))}
		for i, r := range sec.Recipes {
			cat.Rows[i] = MealPlanRow{Dish: r.Title, Serving: r.Serving}
		}
		out = append(out, cat)
	}
	return out
}

// weeklyMealPlanWidthCells flattens every category's rows into ColumnWidths' row-major shape.
// "Usual time" and "Notes" are always blank -- real cells only from Dish and Serving -- so
// their column widths fall to minColumnPct, colwidth.go's floor sized for what a parent can
// write in by hand, which is exactly right for two columns nobody but the family fills.
func weeklyMealPlanWidthCells(plan []MealPlanCategory) [][]string {
	var cells [][]string
	for _, cat := range plan {
		for _, r := range cat.Rows {
			cells = append(cells, []string{r.Dish, "", r.Serving, ""})
		}
	}
	return cells
}
