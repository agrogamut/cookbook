package book

import (
	"context"
	"strings"
	"sync"

	"github.com/madamgy/recipie/internal/aidraft"
	"github.com/madamgy/recipie/internal/engine"
	"github.com/madamgy/recipie/internal/models"
)

// maxDraftConcurrency bounds how many real Gemini network calls run at once. Serial
// drafting -- one clinical modification note per matching recipe, one doctor-approach note
// per eligible Book 1 block -- was enough on its own to push real generation time past even
// the print route's 180s budget the first time GEMINI_API_KEY was actually wired to
// production (a 30-recipe Book 2 with an active clinical condition can trigger a dozen or
// more matching recipes). Bounded rather than unbounded, so a large book does not open
// enough simultaneous connections to look like abuse to the API.
const maxDraftConcurrency = 6

// draftConcurrently runs n independent drafting calls, do(ctx, i) for i in [0, n), with at
// most maxDraftConcurrency in flight at once. Each call is expected to write its own result
// to a distinct location the caller owns (a slice index, or a map key populated only after
// every goroutine has joined) -- draftConcurrently itself holds no shared mutable state
// across calls, so as long as callers respect "one call writes one location, never shared
// with another call," no further synchronization is needed. ctx is passed through to each
// call exactly as a synchronous caller would pass it, so a real Gemini request still
// returns promptly on cancellation.
func draftConcurrently(ctx context.Context, n int, do func(ctx context.Context, i int)) {
	if n == 0 {
		return
	}
	sem := make(chan struct{}, maxDraftConcurrency)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			do(ctx, i)
		}(i)
	}
	wg.Wait()
}

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

// modificationRequestFor finds the one active clinical rule action (if any) whose domain
// matches this recipe's own clinical tag, and builds the request DraftModificationNote would
// need to paraphrase it -- without making the call. Split from the actual draft so the real
// network round trip can be deferred and run concurrently across a whole chapter's recipes
// rather than blocking the row scan one recipe at a time; see draftConcurrently and its call
// site in loadRecipeCards. ok is false whenever nothing matches, the common case.
func modificationRequestFor(p models.ChildProfile, actions []engine.ClinicalRuleAction,
	recipeName, clinicalTag string) (req aidraft.ModificationRequest, ok bool) {

	for _, a := range actions {
		if !clinicalDomainMatchesTag(a.ClinicalDomain, clinicalTag) {
			continue
		}
		return aidraft.ModificationRequest{
			RuleID:               a.RuleID,
			ClinicalDomain:       a.ClinicalDomain,
			BookAction:           a.Book2Action,
			RequiredModification: a.RequiredModification,
			RecipeName:           recipeName,
			ChildAgeMonths:       p.AgeMonths,
		}, true
	}
	return aidraft.ModificationRequest{}, false
}
