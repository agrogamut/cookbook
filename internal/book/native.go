package book

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/madamgy/recipie/internal/bookdocx"
	"github.com/madamgy/recipie/internal/profile"
)

type NativeSet struct {
	Books     [2]bookdocx.Request
	Omissions [2][]string
}

func nativeText(text string) bookdocx.Block {
	return bookdocx.Block{Paragraph: &bookdocx.Paragraph{Runs: []bookdocx.Run{{Text: text}}}}
}

func nativeTable(rows ...[]string) bookdocx.Block {
	return bookdocx.Block{Table: &bookdocx.Table{Rows: rows}}
}

// NativeBindings reads one assembled set; no module performs recipe selection.
func NativeBindings(s profile.Stored, set Set) NativeSet {
	n := NativeSet{Books: [2]bookdocx.Request{
		{Book: 1, Language: set.Book1.Metadata.Language, Version: "v1", Modules: map[string]bookdocx.Module{}},
		{Book: 2, Language: set.Book2.Metadata.Language, Version: "v1", Modules: map[string]bookdocx.Module{}},
	}}
	put := func(book, module int, blocks ...bookdocx.Block) {
		n.Books[book-1].Modules[fmt.Sprintf("B%d-%02d", book, module)] = bookdocx.Module{Blocks: blocks}
	}
	gap := func(book, module int, reason string) {
		key := fmt.Sprintf("B%d-%02d", book, module)
		n.Books[book-1].Modules[key] = bookdocx.Module{Gap: reason}
	}
	partial := func(book, module int, reason string) {
		key := fmt.Sprintf("B%d-%02d", book, module)
		m := n.Books[book-1].Modules[key]
		if m.Gap != "" {
			n.Omissions[book-1] = append(n.Omissions[book-1], key+": "+m.Gap)
			m.Blocks = append(m.Blocks, nativeText(m.Gap))
			m.Gap = ""
		}
		m.Blocks = append(m.Blocks, nativeText(reason))
		n.Books[book-1].Modules[key] = m
		n.Omissions[book-1] = append(n.Omissions[book-1], key+": "+reason)
	}
	sections := map[string]Section{}
	for _, sec := range set.Book1.Sections {
		sections[sec.BlockID] = sec
	}
	provider := func(module int, ids ...string) {
		var blocks []bookdocx.Block
		for _, id := range ids {
			if sec, ok := sections[id]; ok {
				blocks = append(blocks, nativeSection(sec)...)
			}
		}
		if len(blocks) == 0 {
			gap(1, module, "No applicable provider content was assembled from "+strings.Join(ids, ", ")+" for this child's recorded age and profile.")
		} else {
			put(1, module, blocks...)
		}
	}
	identity := nativeTable([]string{"Name", s.DisplayName}, []string{"Date of birth", set.Book1.Child.DateOfBirth}, []string{"Age in completed months", strconv.Itoa(set.Book1.Child.AgeMonths)}, []string{"Recorded language", s.LanguageID}, []string{"Region", s.RegionCulture}, []string{"Generated", set.AsOf.UTC().Format("2006-01-02")})
	put(1, 1, identity)
	put(2, 1, identity)
	put(1, 2, nativeText(s.DisplayName), nativeText(strings.Join(set.Book1.Letter.Body, "\n\n")))
	index := nativeRecipeIndex(set.Book2.MealSections, nil)
	put(1, 3, index...)
	put(1, 4, nativeText("Book1 uses modules B1-01 through B1-63. Book2 uses modules B2-01 through B2-56. Its selected-recipe index is in B2-03; the same recipe IDs are listed in B1-03."))
	partial(1, 4, "Final module page numbers are not recorded; use the fixed module codes and recipe IDs for cross-references.")
	clinical := [][]string{{"Recorded field", "Recorded value", "Class"}}
	for _, c := range s.Conditions {
		clinical = append(clinical, []string{c.TriggerField, c.FlagValue, c.Class})
	}
	if len(clinical) > 1 {
		put(1, 5, nativeTable(clinical...))
		put(1, 52, nativeTable(clinical...))
		put(1, 60, nativeTable(clinical...))
	} else {
		gap(1, 5, "No birth or medical history was recorded in this profile.")
		gap(1, 52, "No chronic-condition or specialist treatment plan was recorded.")
		gap(1, 60, "No structured clinical-history, medicine, laboratory-result or visit-record fields were supplied.")
	}
	if len(clinical) > 1 {
		partial(1, 5, "Birth details, medical reports and medicine records are not collected by this profile.")
		partial(1, 52, "Recorded flags are not a specialist plan. No structured clinician-authored diet, fluid or treatment plan was recorded.")
		partial(1, 60, "Structured medicine doses, laboratory results and appointment dates are not recorded.")
	}
	vegan := "No"
	if s.Vegan {
		vegan = "Yes"
	}
	food := nativeTable([]string{"Diet", s.DietType}, []string{"Vegan declaration", vegan}, []string{"Region", s.RegionCulture}, []string{"Cuisine", s.CuisineCode}, []string{"Religious restriction", s.ReligiousRestriction}, []string{"Budget band", s.BudgetBand})
	put(1, 6, food)
	put(2, 2, food)
	provider(7, "B1-018")
	provider(8, "B1-008")
	partial(1, 8, "No swallowing assessment, aspiration clearance or prescribed IDDSI level is recorded.")
	provider(9, "B1-003")
	gap(1, 10, "WHO reference curves and validated percentile calculations are not installed. No chart or calculated z-score is produced.")
	provider(11, "B1-003")
	partial(1, 11, "Only recorded measurements, z-scores and interpretations are shown. No new percentile or growth-trend interpretation is calculated.")
	gap(1, 12, "No child-specific growth target, target date or next-measurement plan was recorded.")
	provider(13, "B1-003")
	provider(14, "B1-NUTRITION-01")
	partial(1, 14, "Provider target actions are guidance. No individual calorie or macronutrient prescription is recorded.")
	provider(15, "B1-NUTRITION-01")
	partial(1, 15, "No child-specific micronutrient dose or calculated deficiency is recorded.")
	provider(16, "B1-005")
	partial(1, 16, "No individualized daily fluid volume or fibre target is recorded.")
	put(1, 17, nativeText("Selected recipes retain their source serving grams in Book2's meal entries and B2-05. Household-measure conversions, cooked yields and child-specific portion prescriptions are unavailable."))
	gap(1, 18, "No measured food-intake record and approved numerical nutrient targets are available for an intake-gap calculation.")
	var meals []bookdocx.Block
	for _, section := range set.Book2.MealSections {
		meals = append(meals, nativeText(section.Title+": "+strconv.Itoa(len(section.Recipes))+" selected recipes; original IDs in B1-03 and B2-03."))
	}
	put(1, 19, meals...)
	partial(1, 19, "These are selected meal categories, not a timed daily feeding schedule. No clock times were recorded.")
	gap(1, 20, "No approved seven-day assignment of these selected recipes to dates or meals was supplied.")
	gap(1, 21, "No four-week rotation, repetition limits or dated meal assignments were supplied.")
	put(1, 22, nativeRecipeIndex(set.Book2.MealSections, []string{"School Tiffin"})...)
	partial(1, 22, "Travel and festival plans are not recorded; school-tiffin entries appear only when selected from their source meal category.")
	gap(1, 23, "No child-specific water volume or drinking schedule was recorded.")
	gap(1, 24, "No approved beverage schedule or beverage-master collection was supplied.")
	provider(25, "B1-005")
	partial(1, 25, "No individual milk-volume prescription or validated milk-substitution mapping was supplied.")
	provider(26, "B1-008")
	provider(27, "B1-008", "B1-027")
	provider(28, "B1-007")
	provider(29, "B1-007", "B1-019")
	provider(30, "B1-007")
	provider(31, "B1-014")
	provider(32, "B1-011")
	provider(33, "B1-012")
	provider(34, "B1-029")
	provider(35, "B1-029")
	provider(36, "B1-028")
	provider(37, "B1-023")
	provider(38, "B1-023")
	gap(1, 39, "No dated stool, urine or toilet observations were recorded. Observation cells remain blank.")
	provider(40, "B1-017", "B1-024")
	provider(41, "B1-023")
	gap(1, 42, "No tooth-eruption observations or dental chart were recorded.")
	provider(43, "B1-026")
	provider(44, "B1-026")
	gap(1, 45, "No dental visit date, examination result or next appointment was supplied.")
	provider(46, "B1-026")
	provider(47, "B1-025")
	provider(48, "B1-025")
	gap(1, 49, "No agreed daily schedule for sleep, food, activity, therapy or medicines was recorded.")
	provider(50, "B1-015", "B1-016")
	provider(51, "B1-NUTRITION-01")
	provider(53, "B1-016")
	provider(54, "B1-012", "B1-018")
	provider(55, "B1-009")
	partial(1, 55, "This is the provider's reference schedule. No administered vaccination dates, doses or clinician catch-up plan were recorded.")
	provider(56, "B1-010")
	safety := nativeSafety(set.Book2)
	put(1, 57, safety...)
	provider(58, "B1-018")
	provider(59, "B1-019", "B1-020", "B1-021", "B1-031")
	gap(1, 61, "The supplied fixed FAQ questions have no approved answer master. Answer cells remain blank.")
	gap(1, 62, "No parent questions or consultation notes were recorded. Writing areas remain blank.")
	provider(63, "B1-022")
	partial(1, 63, "Template version v1; generation date "+set.AsOf.UTC().Format("2006-01-02")+". Printed names are not signatures. Human approval fields remain blank.")
	put(2, 3, index...)
	gap(2, 4, "Source quantities are in grams. No validated household-measure conversion, cooked-yield or serving-count table was supplied.")
	put(2, 5, nativeServing(set.Book2)...)
	providerSafety := nativeSection(sections["B1-018"])
	if len(providerSafety) > 0 {
		put(2, 6, providerSafety...)
	} else {
		gap(2, 6, "No applicable provider allergen guide was assembled.")
	}
	partial(2, 6, "Source allergen tags are retained per recipe. No validated ingredient-substitution crosswalk was supplied.")
	put(2, 7, safety...)
	put(2, 8, nativeText(set.Book2.HoneyRule), nativeText(strings.Join(set.Book2.ChokingHazards, "\n")))
	for _, pair := range []struct {
		module int
		meals  []string
	}{{9, []string{"Breakfast"}}, {10, []string{"Mid-morning"}}, {11, []string{"Lunch"}}, {12, []string{"School Tiffin"}}, {13, []string{"Evening snack", "Snack"}}, {14, []string{"Dinner"}}} {
		var entries []bookdocx.Entry
		for _, meal := range set.Book2.MealSections {
			match := false
			for _, title := range pair.meals {
				if strings.EqualFold(title, meal.Title) {
					match = true
				}
			}
			if !match {
				continue
			}
			for _, card := range meal.Recipes {
				blocks := nativeRecipe(card, meal.Title, 43)
				for module := 44; module <= 48; module++ {
					blocks = append(blocks, nativeRecipe(card, meal.Title, module)[1:]...)
				}
				entries = append(entries, bookdocx.Entry{ID: card.RecipeID, Blocks: blocks})
			}
		}
		if len(entries) == 0 {
			gap(2, pair.module, "No eligible recipe was selected from the source meal categories "+strings.Join(pair.meals, ", ")+" for this child.")
		} else {
			n.Books[1].Modules[fmt.Sprintf("B2-%02d", pair.module)] = bookdocx.Module{Entries: entries}
		}
	}
	gap(2, 15, "No selected recipe has a mapped dessert, festival or travel-use category. Meal names alone do not establish those uses.")
	for module, category := range map[int]string{16: "rice, cereal or millet", 17: "dal, pulse or soy", 18: "vegetable or fruit", 19: "milk or milk alternative", 20: "egg", 21: "fish", 22: "chicken or mutton"} {
		gap(2, module, "Selected source ingredients have no verified "+category+" group crosswalk. Their original names and occurrence IDs remain in modules B2-44 and B2-54.")
	}
	for module, target := range map[int]string{23: "growth or energy", 24: "protein", 25: "iron", 26: "calcium or bone", 27: "fibre or bowel", 29: "fever or poor intake", 30: "diarrhoea or vomiting", 31: "constipation"} {
		gap(2, module, "No explicit selected-recipe category mapping for "+target+" has been recorded. Ingredient names and unverified nutrient estimates are not used to infer that category.")
	}
	put(2, 28, nativeServing(set.Book2)...)
	partial(2, 28, "A source texture is not confirmation of this child's self-feeding or swallowing readiness.")
	gap(2, 32, "Post-vaccination source cases require previously tolerated foods and a recorded feeding assessment; these prerequisites are not present in the current profile.")
	var modifications []bookdocx.Block
	for _, meal := range set.Book2.MealSections {
		for _, card := range meal.Recipes {
			if card.ModificationNote != nil {
				modifications = append(modifications, nativeText(card.RecipeID), nativeText(card.ModificationNote.Text))
			}
		}
	}
	if len(modifications) > 0 {
		put(2, 33, modifications...)
	} else {
		gap(2, 33, "No applicable provider-backed recipe modification was assembled. No specialist plan is inferred from source case numbers.")
	}
	gap(2, 34, "No validated allergy-specific ingredient substitutions or replacement quantities were supplied.")
	for module, subject := range map[int]string{35: "beverage collection and child eligibility", 36: "water and hydration recipes", 37: "milk-based beverages", 38: "fruit and vegetable beverages", 39: "grain and pulse beverages", 40: "age-stage beverage adaptations", 41: "illness-specific beverages", 42: "beverage preparation and storage"} {
		gap(2, module, "No eligible selected beverage records or dedicated source master support "+subject+". No 400-record collection is claimed.")
	}
	put(2, 43, nativeText("Each complete recipe entry in B2-09 through B2-14 carries its original ID, source, meal category, region and source review status. Immutable revision locators are in B2-56; B2-03 indexes exactly the same selected IDs."))
	put(2, 44, nativeText("Ingredient occurrence ordinals, raw source IDs, names and decimal gram quantities are printed once in each complete meal entry. B2-54 indexes those ingredients by the same recipe IDs. No household measures or cooked yields are inferred."))
	put(2, 45, nativeText("The complete source preparation method and recorded preparation/cooking minutes appear in each meal entry. Steps are not shortened or replaced. Recipe IDs and meal locations are indexed in B2-03."))
	put(2, 46, nativeText("Each meal entry includes recorded derived nutrition where available, with its formula, ingredient-mass basis and verified coverage. Source case estimates have no verified ingredient crosswalk and retain an explicit nutrition gap. No nutrient values are inferred from ingredient names."))
	put(2, 47, nativeText("Source safety, age, diet, allergen, clinical and review fields are retained with each complete meal entry. Selection does not turn a source review flag into approval. Unsupported conditional prerequisites exclude affected source recipes before ranking; their precise gaps are reported with this generation."))
	put(2, 48, nativeText("Each complete meal entry carries its source serving and recipe-specific storage rule when recorded. An absent per-recipe storage rule is identified there; general provider handling guidance is in B2-07."))
	gap(2, 49, "No validated substitution table or equivalent quantities were supplied for the selected recipes.")
	gap(2, 50, "No approved seven-day recipe schedule was recorded. The selected-recipe index does not assign days.")
	gap(2, 51, "No approved four-week recipe rotation or repetition policy was supplied.")
	gap(2, 52, "No approved beverage rotation or dated beverage schedule was supplied.")
	gap(2, 53, "No dated acceptance, exposure or reaction observations were supplied. Tracking cells remain blank.")
	put(2, 54, nativeIngredientIndex(set.Book2)...)
	gap(2, 55, "The supplied fixed FAQ questions have no approved answer master. Answer cells remain blank.")
	put(2, 56, nativeSources(set.Book2)...)
	if set.Book1.Metadata.PrescriptionPhoto != nil {
		partial(1, 60, "The supplied prescription scan follows the template pages as a separately numbered annex in this Book1. Its clinical content is retained as uploaded; the template's prescription and signature blanks remain blank.")
	}
	return n
}

func nativeRecipeIndex(meals []MealSection, titles []string) []bookdocx.Block {
	var blocks []bookdocx.Block
	for _, meal := range meals {
		include := titles == nil
		for _, title := range titles {
			if strings.EqualFold(meal.Title, title) {
				include = true
			}
		}
		if !include {
			continue
		}
		for _, r := range meal.Recipes {
			blocks = append(blocks, nativeText(fmt.Sprintf("%s | %d. %s | %s | %s", r.RecipeID, r.Number, r.Title, meal.Title, r.Serving)))
		}
	}
	if len(blocks) == 0 {
		return []bookdocx.Block{nativeText("No eligible recipe was selected in this meal category for this child.")}
	}
	return blocks
}

func nativeSection(s Section) []bookdocx.Block {
	var b []bookdocx.Block
	if s.BlockID == "" {
		return b
	}
	b = append(b, nativeText(s.BlockID+": "+s.Title))
	for _, r := range s.Rows {
		b = append(b, nativeTable([]string{r.Label, r.Reference, r.Note}))
	}
	for _, r := range s.Growth {
		b = append(b, nativeTable([]string{"Measurement date", "Weight kg", "Height cm", "Head circumference cm", "Recorded z-scores", "Recorded interpretation"}, []string{r.MeasuredOn, r.WeightKg, r.HeightCm, r.HeadCircumCm, r.ZScores, r.Interpretation}), nativeText(r.MeasuredBy))
	}
	for _, r := range s.Domains {
		b = append(b, nativeText(r.ID+": "+r.Domain), nativeTable([]string{r.Reference, r.Goal, r.RedFlag, r.Referral, r.AILimit}))
	}
	for _, r := range s.Illness {
		b = append(b, nativeText(r.ID+": "+r.Situation), nativeTable([]string{r.SupportiveMessage, r.WhatToMonitor, r.RedFlags, r.EngineLimit}))
	}
	if s.Stage != nil && s.Stage.Current != nil {
		r := s.Stage.Current
		b = append(b, nativeText(r.StageCode+": "+r.StageName), nativeTable([]string{"Milk", r.MilkContext}, []string{"Food", r.ComplementaryFood}, []string{"Texture", r.TextureMinimum}, []string{"Progression", r.TextureProgression}, []string{"Self-feeding", r.SelfFeeding}, []string{"Responsive feeding", r.ResponsiveFeeding}, []string{"Quantity rule", r.QuantityRule}, []string{"Safety", r.HardExclusion}, []string{"Source status", r.ReviewStatus}))
	}
	if s.Safety != nil {
		b = append(b, nativeText("Confirmed: "+strings.Join(s.Safety.Confirmed, ", ")), nativeText("Suspected: "+strings.Join(s.Safety.Suspected, ", ")))
		for _, r := range s.Safety.Rules {
			b = append(b, nativeTable([]string{r.Label, r.Reference, r.Note}))
		}
		for _, r := range s.Safety.Choking {
			b = append(b, nativeTable([]string{r.Food, r.Risk, r.Rule, r.AgeFor}))
		}
	}
	for _, r := range s.Refs {
		b = append(b, nativeTable([]string{r.SourceID, r.Authority, r.Reference, r.URL, r.Limitation, r.Status}))
	}
	for _, t := range s.Trackers {
		b = append(b, nativeText(t.Title+": "+t.Parameter), nativeText(t.Reference), nativeText(t.Frequency), nativeText(strings.Join(t.Columns, " | ")), nativeText("Recorded observations: ____________________"))
	}
	if s.Callout != nil {
		b = append(b, nativeText(s.Callout.Heading), nativeText(s.Callout.Body))
	}
	return b
}

func nativeSafety(b Book2) []bookdocx.Block {
	var out []bookdocx.Block
	for _, s := range b.SafetySOP {
		out = append(out, nativeTable([]string{s.SOPID, s.Area, s.Rule, s.Status}, []string{s.EvidenceTitle, s.EvidenceAuthority, s.EvidenceYear, s.SourceURL}))
	}
	if len(out) == 0 {
		out = append(out, nativeText("No applicable food-safety SOP was assembled from the provider corpus."))
	}
	return out
}

func nativeServing(b Book2) []bookdocx.Block {
	var blocks []bookdocx.Block
	for _, m := range b.MealSections {
		for _, r := range m.Recipes {
			texture := ""
			if r.TextureServing != nil {
				texture = *r.TextureServing
			}
			blocks = append(blocks, nativeText(r.RecipeID+": "+r.Serving+"; source texture: "+texture))
		}
	}
	if len(blocks) == 0 {
		blocks = append(blocks, nativeText("No eligible recipe serving or texture was selected for this child."))
	}
	return blocks
}

func nativeRecipe(r RecipeCard, meal string, module int) []bookdocx.Block {
	b := []bookdocx.Block{nativeText(fmt.Sprintf("%d. %s [%s]", r.Number, r.Title, r.RecipeID))}
	switch module {
	case 43:
		b = append(b, nativeText("Meal: "+meal+"; region: "+r.RegionCulture), nativeText("Source: "+r.Source), nativeText("Source review status: "+r.ReviewStatus), nativeText("Immutable revision and source locator: B2-56, "+r.RecipeID))
	case 44:
		for i, v := range r.Ingredients {
			ordinal := v.SourceOrdinal
			if ordinal == 0 {
				ordinal = i + 1
			}
			id := ""
			if v.SourceID != "" {
				id = " [" + v.SourceID + "]"
			}
			b = append(b, nativeText(fmt.Sprintf("%d. %s%s: %s", ordinal, v.Name, id, v.Quantity)))
		}
		b = append(b, nativeText("Source serving: "+r.Serving), nativeText("Cooked yield and household measures are not recorded."))
	case 45:
		for _, step := range r.MethodSteps {
			b = append(b, nativeText(step))
		}
		if r.PrepTimeMinutes != nil {
			b = append(b, nativeText("Preparation minutes: "+strconv.Itoa(*r.PrepTimeMinutes)))
		}
		if r.CookTimeMinutes != nil {
			b = append(b, nativeText("Cooking minutes: "+strconv.Itoa(*r.CookTimeMinutes)))
		}
	case 46:
		if r.Nutrition == nil {
			b = append(b, nativeText("Verified recipe nutrition is unavailable. Source estimates have no validated ingredient equivalence and are not presented as verified nutrition."))
		} else {
			n := r.Nutrition
			b = append(b, nativeTable([]string{"Energy kcal", n.EnergyKcal}, []string{"Protein g", n.ProteinG}, []string{"Iron mg", n.IronMg}, []string{"Calcium mg", n.CalciumMg}, []string{"Ingredient mass basis", n.BasisG}, []string{"Verified ingredient mass coverage", strconv.FormatFloat(n.Coverage, 'f', -1, 64)}, []string{"Derived formula", n.Formula}))
		}
	case 47:
		b = append(b, nativeText(r.Safety), nativeText("Source review status: "+r.ReviewStatus))
		for _, key := range []string{"Min_Age_Months", "Max_Age_Months", "Diet_Type", "Allergen_Tags", "Clinical_Tag", "Growth_Target", "Texture", "Book1_Use", "Book2_Use", "AI_Adaptation_Rule"} {
			if v := r.SourceFields[key]; v != "" {
				b = append(b, nativeText(key+": "+v))
			}
		}
	case 48:
		b = append(b, nativeText("Source serving: "+r.Serving))
		if r.StorageRule != "" {
			b = append(b, nativeText(r.StorageRule))
		} else {
			b = append(b, nativeText("No recipe-specific storage or reheating rule was recorded; general provider SOPs are listed in B2-07."))
		}
	}
	return b
}

func nativeIngredientIndex(b Book2) []bookdocx.Block {
	var blocks []bookdocx.Block
	type occurrenceIndex struct {
		name, id string
		refs     []string
	}
	index := map[string]*occurrenceIndex{}
	var keys []string
	for _, meal := range b.MealSections {
		for _, r := range meal.Recipes {
			for i, v := range r.Ingredients {
				ordinal := v.SourceOrdinal
				if ordinal == 0 {
					ordinal = i + 1
				}
				key := v.SourceID + "\x00" + v.Name
				if index[key] == nil {
					index[key] = &occurrenceIndex{name: v.Name, id: v.SourceID}
					keys = append(keys, key)
				}
				index[key].refs = append(index[key].refs, fmt.Sprintf("%s (%d)", r.RecipeID, ordinal))
			}
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		entry := index[key]
		blocks = append(blocks, nativeText(entry.name+" ["+entry.id+"]: "+strings.Join(entry.refs, ", ")))
	}
	if len(blocks) == 0 {
		blocks = append(blocks, nativeText("No eligible recipes were selected; the ingredient index is empty."))
	}
	return blocks
}

func nativeSources(b Book2) []bookdocx.Block {
	var out []bookdocx.Block
	seen := map[string]bool{}
	archives, members := map[string]bool{}, map[string]bool{}
	for _, meal := range b.MealSections {
		for _, r := range meal.Recipes {
			if seen[r.RecipeID] {
				continue
			}
			seen[r.RecipeID] = true
			out = append(out, nativeText(r.RecipeID), nativeText("Source: "+r.Source), nativeText("Source review status: "+r.ReviewStatus))
			if c := r.SourceRevision; c != nil {
				if !archives[c.ArchiveSHA256] {
					out = append(out, nativeText("Archive SHA256: "+c.ArchiveSHA256))
					archives[c.ArchiveSHA256] = true
				}
				if !members[c.MemberSHA256] {
					out = append(out, nativeText("Workbook: "+c.Workbook), nativeText("Member SHA256: "+c.MemberSHA256))
					members[c.MemberSHA256] = true
				}
				out = append(out, nativeText(fmt.Sprintf("Case %d; %s row %d; workbook %s", c.Case, c.Sheet, c.Row, c.Workbook)))
			} else {
				out = append(out, nativeText("Revision: "+r.RecipeVersion))
			}
			keys := make([]string, 0, len(r.SourceFields))
			for k := range r.SourceFields {
				if strings.Contains(k, "Status") || strings.Contains(k, "Quality") {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			for _, k := range keys {
				out = append(out, nativeTable([]string{k, r.SourceFields[k]}))
			}
		}
	}
	if len(out) == 0 {
		out = append(out, nativeText("No recipes were selected. No recipe-source approval is implied."))
	}
	return out
}
