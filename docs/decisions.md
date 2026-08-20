# Decisions

Rulings made, with the reasoning and what each costs if wrong. Newest first.

A decision belongs here when it constrains future work and is not obvious from the code.
Decisions that are visible in the code (a function signature, a migration) do not need an
entry; decisions about *why* the code is shaped that way do.

---

## 2026-08-18 - Declared diet is nested, not categorical

**Decided.** Implemented in `internal/engine/diet.go` (`dietPermits`), commit
`Treat declared diet as nested rather than categorical`.

`recipe_master.diet_type` states what a dish **requires of whoever eats it**, not which
kind of eater it belongs to. Diet is a nested permission chain:

```
vegan  ⊂  vegetarian  ⊂  eggetarian  ⊂  non-vegetarian
```

The filter previously matched `diet_type` for equality, which reads the column as a
category and withholds food a family is entitled to eat:

| Declared | Was | Now |
|---|---|---|
| Vegetarian | 828 | 828 |
| Eggetarian | **1** | 829 |
| Non-vegetarian | **111** | 940 |
| Vegan | correct | unchanged |

A non-vegetarian child was seeing 12% of the corpus.

**Why it survived every review:** the failure was conservative. Equality never served a
family food their practice forbids - it only withheld food they could have eaten. The
persona suite asserts `minResults >= 1` because it was written to catch *filter collapse*,
the original product blocker. A short list is not a collapse, so nothing tripped.

An unrecognized diet type now returns `ErrInvalidProfile` (HTTP 400) rather than silently
matching nothing, which was the equality match's other failure mode: a typo looked like a
legitimately narrow corpus.

The Phase-1 persona query in `internal/db/persona_test.go` carried the same equality match
and was updated to the same nesting, so the two paths cannot drift.

**Cost if wrong:** a family sees dishes outside their declared practice. Mitigated by the
nesting being one-directional and hand-written - a vegetarian declaration still permits
only `Vegetarian`, and no path widens a narrower practice.

**Deferred, deliberately:** a diet *ranker*. A family declaring non-vegetarian probably
wants non-vegetarian dishes ranked up rather than merely permitted, or page one of their
recipe book is dal. The SRS's "preference match, weight 10" is the right home for it.
Chosen not to build it in the same change because filtering correctness and ranking
preference are separable, and the filter fix is the one that was actively wrong.

---

## 2026-08-18 - No generated guidance prose in the unreviewed path

**Decided.** No code yet - constrains Phase 3.

Book personalization comes from four levers: conditional section firing, slot filling into
provider-authored templates, selection among provider-authored variants, and ordering by
the child's priority goals. No model-generated guidance prose reaches a parent through a
path that has not been through human review.

This is **narrower than the provider's own SRS**, deliberately. SRS section 18.1 permits
"natural-language drafting from approved structured facts" and "editorial variation and
parent-friendly phrasing". Their model allows it because five human review gates -
clinical, nutrition, safety, language, editorial - sit between generation and the parent.
This project has no such gate today.

The rule is about **gate placement, not technology**. When the review portal exists,
phrasing assistance inside approved semantic boundaries becomes legitimate and this ruling
should be revisited rather than treated as permanent.

**Cost if wrong:** books read more templated than they could have. Cheap to reverse in
that direction; expensive in the other, because the reverse means unreviewed clinical
prose reached a parent.

**Related finding:** the lever that actually carries the feeling of personalization is not
prose at all - it is the child's own measured data on the page. The provider's own Book 1
prototype demonstrates it (`16.2 kg`, `103 cm`, `Irregular breakfast`,
`Review after 6 weeks`), and that page contains zero generated sentences. Personalization
density is a data-on-page metric.

---

## 2026-08-17 - Phase 2 integration by pull request, not local merge

**Decided.** PR #1 against `main`.

The Phase 2 branch was pushed and opened as a pull request rather than merged locally,
because the working session was isolated to a git worktree and could not run git
operations against the shared checkout. The worktree is kept in place for PR-feedback
iteration.

---

## 2026-08-20 - Book layout and imagery

| Decision | Where |
|---|---|
| No external photographs on recipe pages: wrong dish, unstated licence, remote fetch, blocked network | `internal/book/marks.go`, spec 3.2 |
| Recipe illustration is line art of the provider-recorded dish format, captioned with it | `internal/book/marks.go` |
| `recipe_photo` exists empty, and GAP-025 is counted against it rather than seeded | migration `0021`, `internal/importer/gaps.go` |
| The composition band is computed and not printed; one recipe on one page wins | migration `0020`'s own note |
| Book 1 breaks once per provider part, not once per block; single-block parts run on | `internal/book/book1.go`, `pagepolicy.go` |
| Printed page fill, near-blank sheets and orphaned openers are budgeted guards | `internal/book/pagefit_test.go` |
| Table column widths are computed from content; `table-layout: fixed` stays | `internal/book/colwidth.go` |
| A blank form protects only its last four rows, not every group of four | `internal/book/types.go` |
| The console embeds the printed PDF; the HTML preview is the no-Chromium fallback | `web/src/components/book-generator.tsx` |

## Earlier decisions

These predate this file and are recorded in `CLAUDE.md` rather than here. Summarised so
this file is a complete index.

| Decision | Where |
|---|---|
| Steps 3 and 6 demoted from hard filters to rankers with graceful degradation | `CLAUDE.md`, "Deviation from the spec" |
| Steps 1 (age) and 2 (allergy) stay hard filters, never relaxed, no operator override | `CLAUDE.md`, "Safety, unchanged" |
| `Clinical_Tag` is a display badge and is never filtered on | `CLAUDE.md`, blocker 1 |
| Cuisine is a ranker; the dropdown is built from `COUNT(*) > 0` | `CLAUDE.md`, "Cuisine filter" |
| Three of ten NT axes are omitted from numerator and denominator, visibly | `CLAUDE.md`, "The nutrition ranker" |
| Cost is inverted before scoring, so cheaper ranks higher | `CLAUDE.md`, "The nutrition ranker" |
| Tuber excluded from the fruit/veg measure | `CLAUDE.md`, "The nutrition ranker" |
| `ingredient_master` is never modified; correction lives in views | `CLAUDE.md`, "The corrected nutrition layer" |
| Scope is one row in `region_focus`; workbooks are never edited | `CLAUDE.md`, "Region focus" |
| A stated region beats the West-Bengal-first default | `CLAUDE.md`, "Region focus" |
| Format matched before ingredients for external method text | `CLAUDE.md`, "What the external join achieved" |
| A format with no mapping gets no method suggestion, ever | `CLAUDE.md`, "What the external join achieved" |
