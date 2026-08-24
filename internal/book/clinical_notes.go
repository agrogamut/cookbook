package book

import (
	"context"
	"strings"

	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/engine"
	"github.com/madamgy/recipie/internal/models"
)

// staticModificationNotes holds the fixed, non-AI feeding notes for conditions the provider's
// clinical_rule_master carries zero backing text for at all -- gas/bloating and "child wants
// to eat more". Fixed, pre-approved wording; calling Gemini for it would spend a network call
// and accept model non-determinism on boilerplate that never varies.
//
// Not wired to any ChildProfile input yet. ClinicalFlags is validated against
// clinical_rule_master.trigger_field (clinicalFilter, internal/engine/clinical.go) and these
// two conditions have no row there by definition -- that is the reason they need static text
// in the first place. ClinicalMarker exists on the profile but is documented for a different
// purpose (the operator's NT02-NT12 nutrition-target choice), and repurposing it here would be
// exactly the kind of silent field reuse this project's data rules argue against. Until a real
// input exists for "family reports gas/bloating" or "wants to eat more", this map has no
// caller -- an honest gap, not a wiring bug, and better than matching on a fabricated flag
// that would fire on the wrong recipes.
var staticModificationNotes = map[string]string{
	"gas_bloating": "Offer smaller, more frequent portions and avoid rushing meals. " +
		"Mention ongoing gas or bloating to your child's doctor if it continues.",
	"wants_to_eat_more": "Offer an extra small portion of a recipe already in this book " +
		"rather than a new food. If your child consistently seems hungry after meals, " +
		"mention it to your child's doctor.",
}

// clinicalDomainMatchesTag reports whether a clinical_rule_master domain and a
// recipe_master.clinical_tag are talking about the same condition. The two are independent
// provider vocabularies with no shared key -- clinical_tag is a short single word like
// "Constipation" or "Iron"; clinical_domain is a longer phrase like "Iron/Anemia Risk" -- so
// this is a case-insensitive substring match in both directions rather than a fabricated
// mapping table. A false negative here just means no note is attached; a false positive would
// attach the wrong condition's note, which is why the match has to hold in both directions
// rather than either one alone.
func clinicalDomainMatchesTag(domain, tag string) bool {
	if domain == "" || tag == "" {
		return false
	}
	d, tg := strings.ToLower(domain), strings.ToLower(tag)
	return strings.Contains(d, tg) || strings.Contains(tg, d)
}

// modificationNoteFor finds the one active clinical rule action (if any) whose domain matches
// this recipe's own clinical tag and asks drafter to paraphrase its book2_action/
// required_modification text. Nil, not an error, whenever nothing matches or drafting is
// unavailable -- a missing note is the common case, not a failure.
func modificationNoteFor(ctx context.Context, drafter aidraft.Drafter, p models.ChildProfile,
	actions []engine.ClinicalRuleAction, recipeName, clinicalTag string) *aidraft.DraftedText {

	for _, a := range actions {
		if !clinicalDomainMatchesTag(a.ClinicalDomain, clinicalTag) {
			continue
		}
		note, err := drafter.DraftModificationNote(ctx, aidraft.ModificationRequest{
			RuleID:               a.RuleID,
			ClinicalDomain:       a.ClinicalDomain,
			BookAction:           a.Book2Action,
			RequiredModification: a.RequiredModification,
			RecipeName:           recipeName,
			ChildAgeMonths:       p.AgeMonths,
		})
		if err != nil {
			// Drafting unavailable (no API key) or the call itself failed: no note is the
			// safe fallback, never a half-built one, and never a reason to fail the card.
			return nil
		}
		return &note
	}
	return nil
}
