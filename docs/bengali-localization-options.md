# Bengali localization - options and cost

Written in response to a question about generating a full book in Bengali, before a
decision had been made. Superseded below: a decision was made the same day, and it did
not take the path this document scoped.

## Update (2026-09-08) - built, and not the way this document proposed

`LanguageID` on intake (`bn`/`bengali`/`bangla`, any casing, via `bookLanguage` in
`internal/book/translate.go`) now actually drives `Metadata.Language`, and a Bengali
book is real: fonts, translated prose, correctly shaped conjuncts, verified against the
live API on a real child's two books (see `sample-books/` locally - gitignored, not in
this repo).

The path taken is architecturally different from "What doesn't: three separate layers"
below, and cheaper than the 7-11 day estimate that section priced: rather than
extracting ~600-800 static template strings into a string table (layer 1) and
separately switching four Gemini prompt builders to draft in Bengali (layer 2), a
single mechanism covers both. `book.TranslateHTML` walks the **already fully-rendered**
HTML - after every real value (a recipe name, a quantity, an AI-drafted note) is already
substituted in - and sends every visible text node to Gemini for translation in place,
batched (`translateBatchSize`, `internal/book/translate.go`) and retried once
(`aidraft.geminiClient.TranslateTexts`). Nothing about the document's structure, ids, or
numbers is touched; only prose.

This sidesteps layer 1 and layer 2 as separate engineering efforts entirely - there is
no string table, and the four prompt builders in `internal/aidraft/draft.go` still only
draft in English, because their output reaches the page before translation runs, not
after. It also means every provider-sourced string (a clinical rule's required
modification text, a recipe's boilerplate safety line) gets carried into Bengali for
free, which the original three-layer plan would have needed layer 3's `_bn`-column
machinery for. **What that trade costs**: layer 3's own point - a human bilingual
clinician reviewing translated *clinical* text before it prints - is not something this
mechanism does. Every visible string is translated indiscriminately, clinical source
text included, on the same footing as a template label. That was an explicit choice,
not an oversight: made per direct instruction from the project owner ("no warnings, no
labels of drafts... keep everything like it is"), overriding the review-gate posture
this document's layer 3 section had recommended.

Two real findings from building it, worth keeping for the next language:

- **Quota, not accuracy, was the binding constraint.** Gemini 3.1 Pro's Tier 1 daily
  quota for this project's key sat at 250 requests/day regardless of the account's paid
  status - a preview-model ceiling the tier ladder doesn't lift the way it does for GA
  models. One 41-page book's translation alone costs dozens of batched calls at this
  granularity (DOM text nodes, not whole pages), which burns that ceiling fast.
  `translateModelName` (`internal/aidraft/client.go`) routes translation specifically to
  `gemini-3.6-flash` (10,000 RPD on the same key) rather than `modelName`, while the
  other three Drafter methods stay on Pro - translation is this package's only
  high-call-volume caller, and the "Flash spends real thinking tokens regardless of
  prompt simplicity" cost this file's own client.go doc comment already warned about is
  worth accepting for headroom on that one caller, not the others.
- **A real side-by-side (Pro vs Flash, same recipe, same run) showed a small, real
  fidelity gap, not a legibility break**: Flash dropped a parenthetical clarification in
  one recipe title that Pro kept, and ran one table cell a line longer. Nothing
  safety-relevant, nothing garbled - Pro reads as marginally more complete when quota
  allows using it.

The per-book Gemini cost table and the general shape of "quota headroom differs sharply
by model, and preview models don't follow the tier ladder GA models do" below remain
useful background. Everything under "What doesn't: three separate layers" describes a
plan that was not the one built and should not be read as the current architecture.

## What already works

- `internal/book/templates/tokens.css` already declares `--font-indic` /
  `--font-indic-sans` (Noto Serif/Sans Bengali + Devanagari). Chromium was
  chosen as the renderer specifically because Bengali needs conjunct
  formation and matra repositioning no Go PDF library shapes - this was
  solved before the localization question ever came up.
- `base.html` already templates `lang="{{ .Metadata.Language }}"`.
- `ingredient_master` already carries Bengali names for all 406 ingredients.

None of this needed new work. What's below is what does.

## What doesn't: three separate layers

`Metadata.Language` is captured on the profile but hardcoded to `"en"`
(`internal/book/types.go:85`) and never branches template output. Three
layers need separate treatment, because they come from different sources and
carry different risk:

### 1. Static template prose (headings, labels, blank-form captions)

~30 templates across Book 1 and Book 2, roughly 20+ hardcoded English text
nodes per template (~600-800 strings total). Three ways to do it:

| Approach | What it means | Effort | Ongoing cost |
|---|---|---|---|
| Duplicate templates | A second `templates/book1-bn/`, `book2-bn/` tree, hand-translated | Lower upfront, ~1-2 days per book | Every future template edit has to be made twice, forever - this is the option most likely to silently drift out of sync |
| Extract to a string table | Pull every literal into `strings/en.json` + `strings/bn.json`, templates call a lookup function | Higher upfront (refactor every template), 4-6 days | One template, any language, forever - the only approach that doesn't rot |
| Machine-translate the extracted strings | Same extraction as above, then Gemini/DeepL fills `bn.json` | Extraction cost is the same 4-6 days; translation itself is near-free (a few thousand tokens, one-time, not per-book) | Needs one human bilingual pass over ~600-800 short strings before trusting them on a document a family reads - these are form labels and medical section headers, and a wrong one (e.g. a mistranslated allergy warning label) is a wrong-instruction risk, not a typo |

Recommended: extraction + machine-translate draft + human review. The
extraction is the expensive part and buys the maintainability; the
translation itself is cheap either way.

### 2. Gemini-drafted prose (recipe modification notes, doctor-approach notes, food-group priorities, invented recipes)

Cheapest layer by far. `internal/aidraft/draft.go`'s four prompt builders
(`buildModificationPrompt`, `buildDoctorApproachPrompt`,
`buildFoodGroupPriorityPrompt`, `buildInventedRecipePrompt`) already call
Gemini per-book; switching the instruction from "write in English" to "write
in Bengali" is a same-shape prompt edit in four places, not a new pipeline.

Effort: half a day to a day, mostly spent re-verifying page-fit (Bengali
text runs longer per idea than English, and `colwidth.go` / tracker row
heights were tuned against English glyph widths per the page-fit section of
`CLAUDE.md`).

### 3. Provider's own clinical source text

The sensitive layer. `clinical_rule_master`'s rule text, the five illness
feeding blocks, `nutrition_target_master`'s `*_action` columns, evidence
`important_limitation` text, and the (already-boilerplate) prep/safety/
storage rules are all English in the provider's workbooks. This project's
hard rule is that a value reaching a user has to trace to a verified source
- machine-translating a clinical instruction and shipping it silently is the
same category of problem as inventing a nutrition figure, just in a
different column.

Two honest options, not one:

- **Add `_bn` columns next to the source, human-reviewed** - same pattern
  this project already uses for `_External` columns (never overwrite the
  original, add a parallel column with its own provenance). A bilingual
  clinician/dietitian translates and the column is populated once it's
  checked, same posture as the `ingredient_ifct_alias` review workflow.
  Volume is small: 31 clinical rules, 5 illness blocks, 13 nutrition
  targets' action columns, ~166 method-suggestion captions. A day or two of
  translation-review work, not engineering.
- **Machine-translate as a draft, and extend the existing per-book physical
  sign-off to explicitly cover translation accuracy** - reuses the director
  + dietitian signature process this project already has, at the cost of
  making that signature vouch for one more thing than it currently states.
  Needs an explicit line added to the sign-off page saying so, or the
  signature is being stretched past what the signer thinks they're
  attesting to.

The first is more work and more honest about what's been checked. The
second is close to free engineering-wise but needs a real decision from
whoever owns the sign-off process, not a code change.

### 4. Page-fit re-verification

`pagefit_test.go`'s guards (fill above 62%, no near-blank pages, no orphaned
openers) were tuned by printing English books and measuring ink. Bengali
conjuncts and matras change line length and row height, so every page-fit
constant in `colwidth.go` and the tracker CSS needs re-measurement against
real Bengali output, the same way the project already had to re-tune column
widths for long English identifiers (`HEAD CIRCUMFER/ENCE` breaking wrong).
This is print-and-look work - the project's own docs are explicit that page
fit was never solved by argument, only by producing a PDF and reading it.

Effort: 1-2 days, gated on layers 1-3 actually existing so there's Bengali
text to print.

## Per-book Gemini cost breakdown

**Everything below is an estimate, not a sourced figure.** I don't have a
current, verified price for `gemini-3.1-pro-preview` - it's a live-API model
this project only discovered by calling it, and I have no committed pricing
data in this repo to check it against. Treat the dollar figures as rough
shape, not budget. The call counts and token-size shape, by contrast, come
from this project's own measured behavior (`CLAUDE.md`'s 2026-08-26 /
2026-08-27 amendments).

| Call type | Typical calls / book | Output size (rough) | Notes |
|---|---|---|---|
| `DraftDoctorApproachNote` (Book 1) | up to 8 | short paragraph, ~100-200 tokens each | one per Book 1 block with no provider text, per the 26 August amendment |
| `DraftModificationNote` (Book 2) | 0-12+ | short paragraph, ~100-200 tokens each | one per matching recipe; a 30-recipe book with an active clinical condition can hit a dozen or more, per the same amendment |
| `DraftFoodGroupPriorities` (Book 1) | 1 | structured list, ~300-500 tokens | one call, one page |
| `DraftInventedRecipe` (Book 2 fallback) | 0-6, situational | full recipe incl. a large ingredient-id enum in the schema, ~800-1500 tokens | only runs when a chapter falls short of its recipe-count target after real corpus recipes are exhausted |

Rough total per book: **10-25 calls**, **1,500-6,000 output tokens**,
depending on how many clinical matches and corpus shortfalls that specific
child hits. That is the *existing* English-generation cost - switching
output language to Bengali does not add calls and does not obviously add
much token volume (Bengali is not verbose relative to English token-for-
token, though this hasn't been measured against this specific model).

**Switching to Bengali output adds ~$0 in recurring per-book API cost.**
The real cost is one-time: the static-string translation (layer 1) and the
clinical-text translation-review (layer 3) are both done once and reused
across every future book, not repeated per generation.

## Total cost shape

| Item | Type | Rough effort |
|---|---|---|
| Static template string extraction + i18n plumbing | one-time engineering | 4-6 days |
| Static string translation (machine draft + human review) | one-time, human review needed | ~600-800 short strings, a day or two of review |
| Gemini prompt language switch (4 prompt builders) | one-time engineering | 0.5-1 day |
| Clinical source text translation (`_bn` columns, reviewed) | one-time, human clinician/translator | 1-2 days of review work, small volume |
| Page-fit re-verification | one-time engineering + print-and-look QA | 1-2 days |
| Per-book Gemini API cost delta | recurring | ~$0 marginal (unverified pricing, but call count/volume doesn't change) |

**Rough total: 7-11 engineering days plus 2-4 days of human bilingual
review**, before any per-book recurring cost - which itself doesn't
meaningfully change from what the project already pays for English
generation.

## Open decision, not an engineering one

How far the clinical-text translation goes (layer 3, option A vs B) is a
sign-off scope question, not a code question - same category as the
project's earlier "who signs what" decisions in `CLAUDE.md`. Worth
deciding before layer 1/2 work starts, since it changes what "a Bengali
book" is allowed to claim about itself.
