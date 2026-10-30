package casesource

import (
	"sort"
	"strings"
)

func (i *Intake) finding(f Finding) {
	i.Report.Findings = append(i.Report.Findings, f)
	i.Report.FindingCounts[f.Code]++
}

func (i *Intake) compareBaseline() {
	i.Report.FieldChanges, i.Report.AllergenRemovals = map[string]int{}, map[string]int{}
	i.finding(Finding{Code: "activation_unreconciled", Evidence: map[string]string{"reason": ErrLocaleActivation.Error(), "baseline_sha256": i.Baseline.SHA256}})
	old := map[int]Workbook{}
	for _, w := range i.Baseline.Workbooks {
		old[w.Case] = w
	}
	for _, w := range i.Archive.Workbooks {
		previous := sheetByName(old[w.Case], "Recipe_Master")
		rows := map[string]Row{}
		for _, r := range previous.Rows {
			if r.RecipeID != "" {
				rows[r.RecipeID] = r
			}
		}
		audits := map[string]map[string]string{}
		for _, r := range sheetByName(old[w.Case], "Uniqueness_Audit").Rows {
			if r.RecipeID != "" {
				audits[r.RecipeID] = Fields(sheetByName(old[w.Case], "Uniqueness_Audit"), r)
			}
		}
		master := sheetByName(w, "Recipe_Master")
		for _, r := range master.Rows {
			if r.RecipeID == "" {
				continue
			}
			base := Finding{Case: w.Case, RecipeID: r.RecipeID, Sheet: master.Name, Row: r.Number}
			prior, ok := rows[r.RecipeID]
			if !ok {
				base.Code, base.Evidence = "baseline_recipe_missing", map[string]string{"reason": "No matching source row exists in the comparison archive."}
				i.finding(base)
				continue
			}
			before, after := Fields(previous, prior), Fields(master, r)
			changed := map[string]string{}
			for _, field := range master.Headers {
				if before[field] != after[field] {
					changed[field] = hash([]byte(before[field])) + ":" + hash([]byte(after[field]))
					i.Report.FieldChanges[field]++
				}
			}
			if len(changed) > 0 {
				base.Code, base.Evidence = "source_fields_changed", changed
				i.finding(base)
			}
			if RuleFingerprint(before) != RuleFingerprint(after) {
				base.Code, base.Evidence = "rule_fingerprint_changed", map[string]string{"before": RuleFingerprint(before), "after": RuleFingerprint(after)}
				i.finding(base)
			}
			oldTags, oldUnknown := comparisonAllergens(before["Allergen_Tags"])
			newTags, newUnknown := comparisonAllergens(after["Allergen_Tags"])
			if len(oldUnknown)+len(newUnknown) > 0 {
				base.Code, base.Evidence = "allergen_vocabulary_unknown", map[string]string{"baseline": strings.Join(oldUnknown, ";"), "incoming": strings.Join(newUnknown, ";")}
				i.finding(base)
			}
			var removed, added []string
			for tag := range oldTags {
				if !newTags[tag] {
					removed = append(removed, tag)
					i.Report.AllergenRemovals[tag]++
				}
			}
			for tag := range newTags {
				if !oldTags[tag] {
					added = append(added, tag)
				}
			}
			sort.Strings(removed)
			sort.Strings(added)
			if len(removed) > 0 || len(added) > 0 {
				base.Code = "allergen_associations_changed"
				if len(removed) > 0 {
					base.Code = "allergen_associations_removed"
				}
				base.Evidence = map[string]string{"removed": strings.Join(removed, ";"), "added": strings.Join(added, ";"), "baseline_raw": before["Allergen_Tags"], "incoming_raw": after["Allergen_Tags"]}
				i.finding(base)
			}
		}
		for _, r := range sheetByName(w, "Uniqueness_Audit").Rows {
			if r.RecipeID == "" {
				continue
			}
			f := Fields(sheetByName(w, "Uniqueness_Audit"), r)
			if previous := audits[r.RecipeID]; previous != nil && f["Process_Fingerprint_SHA256"] == previous["Process_Fingerprint_SHA256"] {
				i.finding(Finding{Code: "audit_claim_not_revalidated", Case: w.Case, RecipeID: r.RecipeID, Sheet: "Uniqueness_Audit", Row: r.Number, Evidence: map[string]string{"reason": "Source process fingerprint is unchanged from the comparison archive; retained audit text is not independent validation.", "source_fingerprint": f["Process_Fingerprint_SHA256"]}})
			}
		}
	}
}

// This vocabulary compares literal source associations only; it never supplies active tags.
func comparisonAllergens(raw string) (map[string]bool, []string) {
	aliases := map[string]string{"Milk": "ALG-MILK", "Egg": "ALG-EGG", "Peanut": "ALG-PEANUT", "Tree nut": "ALG-TREENUT", "Wheat": "ALG-WHEAT", "Soy": "ALG-SOY", "Sesame": "ALG-SESAME", "Fish": "ALG-FISH", "Shellfish": "ALG-SHELLFISH", "Mustard": "ALG-MUSTARD", "Sulphite": "ALG-SULPHITE"}
	for _, code := range aliases {
		aliases[code] = code
	}
	out := map[string]bool{}
	var unknown []string
	for _, token := range strings.Split(raw, ";") {
		token = strings.TrimSpace(token)
		if token == "None of the 11 coded allergens in ingredient list" || token == "None identified in starter tagging" {
			continue
		}
		if code, ok := aliases[token]; ok {
			out[code] = true
		} else {
			unknown = append(unknown, token)
		}
	}
	return out, unknown
}
