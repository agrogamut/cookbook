# Phase 3 - the Book Engine

**Status: not built. Nothing in this document exists in code.**

Recorded 18 August 2026, after the provider delivered eight new files into the repository
root on 17 August. This document describes what those files ask for, what follows from
them, and what has been decided. It is a specification of absent work, not a description
of the system.

Phase 1 (make the data usable) and Phase 2 (Go API + Next.js operator console) are built.
Phase 3 is everything below.

---

## 1. What arrived

Eight files, dropped into the repository root, untracked in git at time of writing:

| File | What it is |
|------|-----------|
| `MadamGY_Knowledge_Book_Engine_SRS_V1 (1).docx` | Full software requirements spec, 27 sections + 2 appendices |
| `MadamGY_Book1_Book2_JSON_PDF_Template_Contract_V1.docx` | Design and UX contract, 19 sections |
| `MadamGY_Full_Master_TOC_Page_Component_Map_V1.docx` | Page-by-page table of contents for both books |
| `MadamGY_Full_Master_TOC_Page_Component_Map_V1.json` | Same, machine-readable |
| `MadamGY_Book1_JSON_Schema_V1.json` | JSON Schema draft 2020-12 for the Book 1 document |
| `MadamGY_Book2_JSON_Schema_V1.json` | JSON Schema draft 2020-12 for the Book 2 document |
| `MadamGY_PDF_Template_Contract_V1.json` | Machine-readable template ids, palettes, page tokens |
| `MadamGY_Book1_Visual_Prototype_V1.pdf` | 9-page rendered sample, Book 1 |
| `MadamGY_Book2_Visual_Prototype_V1.pdf` | 7-page rendered sample, Book 2 |

These files are the provider's answer to "what is this product". They should be committed
so they cannot be lost. They supersede nothing in `CLAUDE.md` - they extend it.

---

## 2. The output contract

**Every input produces exactly two books. This is fixed and is the product.**

| Book | Role | Indicative length | Boundary |
|------|------|-------------------|----------|
| Book 1 | Nutrition, growth, development, vaccination, feeding and monitoring companion | ~70-130+ pages | Food names, types, amounts, frequency and guidance. **No recipe methods.** |
| Book 2 | Personalized recipe companion across relevant current and future age bands | ~160-350+ pages | Detailed recipes, ingredients, quantities, preparation, texture, serving, safety, substitutions, trackers |

The two books are not variants of one document. Book 1 answers *what should I offer and
what should I watch*; Book 2 answers *how do I actually make it*. A recipe method never
appears in Book 1; a growth chart never appears in Book 2.

Page count is an output of personalization, never a target. The provider states this
explicitly: do not generate a section to increase page count, do not force 25 recipes when
fewer remain eligible, do not create a supper chapter to make the book longer.

### The template library is closed

The `component_type` enum in the Book 1 schema and the template ids in
`MadamGY_PDF_Template_Contract_V1.json` are a **fixed, finite set**. Personalization
decides which components fire and what data fills them. It never invents a new component
for a particular child.

Book 1 components: `profile_card`, `goal_cards`, `comparison_table`, `monitoring_table`,
`guidance_cards`, `vaccination_tracker`, `development_tracker`, `daily_life_dashboard`,
`red_flag_box`, `timeline`, `checklist`, `meal_plan_table`, `text_block`,
`reference_page`.

Book 2 templates: `B2-COVER-01`, `B2-PROFILE-01`, `B2-SECTION-01`, `B2-RECIPE-01`,
`B2-RECIPE-02`, `B2-SPECIAL-01`, `B2-SWAP-01`, `B2-ROTATE-01`, `B2-TRACK-01`,
`B2-FAV-01`.

---

## 3. How a book becomes personal

Both books carry general content and specialized content. Making the general content feel
personal is a product requirement, and there is exactly one honest way to do it.

### The four levers

1. **Conditional firing** - which sections exist at all. No toilet-training module for an
   eight-month-old. The Book 1 schema carries `conditional_reason` per section for this
   purpose. A book missing six sections another child's book has *is* personalized.
2. **Slot filling** - the provider authors a sentence once with holes; the engine fills
   name, age, stage, region, goal. Provider text, this child's facts.
3. **Variant selection** - the provider authors N variants of a block per age band, per
   diet, per region; the engine picks one. Still entirely provider-authored.
4. **Ordering and emphasis** - the child's priority goals decide what appears on page two
   rather than page forty.

### The lever that actually carries it

None of the four. **The child's own measured data on the page.**

The provider's own Book 1 prototype demonstrates this on page 3: a table reading
`16.2 kg`, `103 cm`, `Irregular breakfast`, `Target: 5 days/week`,
`Review after 6 weeks`. That page feels intensely personal and contains zero generated
sentences. Reference-versus-actual, side by side, is the pattern. The vaccination tracker,
the milestone table and the acceptance tracker all repeat it.

**Personalization density is a data-on-page metric, not a prose-volume metric.** Build for
that and the general content stops reading as general.

### Ruling: no generated guidance prose in the unreviewed path

**Decided 18 August 2026.** See `docs/decisions.md`.

Personalization comes from slotting, conditional selection and provider-authored variants.
No LLM-generated guidance prose reaches a parent through a path that has not been through
human review.

This is narrower than the provider's own SRS, and the difference is deliberate rather than
a contradiction. SRS section 18.1 permits "natural-language drafting from approved
structured facts" and "editorial variation and parent-friendly phrasing". Their model
allows AI prose **because five human review gates** - clinical, nutrition, safety,
language, editorial - sit between generation and the parent. This project currently has no
such gate. The rule is about gate placement, not about the technology.

If and when the review portal is built (section 8 below), phrasing assistance inside
approved semantic boundaries becomes legitimate and this ruling should be revisited.

### The constraint nobody has solved

The general content pool is currently too thin to carry personalization at all:

- **Book 1 Content Master is not imported.** `internal/importer/spec.go` binds 21 tables
  across 10 workbooks; `MadamGY_Book1_Content_Master_V1_1_DailyLife.xlsx` contributes
  **zero**. The 32 guidance blocks, 44 vaccine rows and 33 milestone rows exist in the
  spreadsheet and nowhere in Postgres. This is the entire Book 1 general layer.
- **Book 2 method text is boilerplate.** `preparation_method_full` holds 6 unique texts
  across 1000 recipes, differing only in liquid volume. `safety_rule`, `storage_rule` and
  `ai_adaptation_rule` hold **one unique value each**. Two different children currently
  receive byte-identical method paragraphs.

Neither is fixed by engine work. Both are authoring problems, and lever 3 is only as good
as the number of variants somebody writes.

**Open question, unassigned: how many variants per block, and who writes them?** This
single number decides whether the books feel personal, and no one has committed to it.

---

## 4. Architecture the SRS asks for

Service-oriented, and deliberately **not** merged with the existing consultation software.
The consultation platform keeps registration, payment, questionnaire intake, the clinical
encounter and the customer record. The Book Engine owns knowledge, rules, personalization,
publishing and release history. They talk over a REST API.

Layers, in order:

```
Consultation software
      -> Integration gateway      (maps to canonical Child_Profile)
      -> Master database          (versioned, approved knowledge)
      -> Clinical & safety rules  (hard exclusions, red flags, escalation)
      -> Nutrition target engine  (target ids + ranking weights)
      -> Recipe engine            (filter, then rank)          <- Phase 2 built this
      -> Book 1 assembler         (content JSON)
      -> Book 2 assembler         (content JSON)
      -> Language layer           (localization, locked tokens)
      -> Design / PDF layer       (human-designed templates)
      -> Governance layer         (approval, versioning, audit, release)
```

**Appendix B of the SRS states the ordering rule directly:** safety, then clinical rules,
then nutrition targets, then recipe eligibility, then ranking, then content assembly, then
AI language assistance, then human review, then release. This order must not be reversed.
Generated prose or recipe creativity must never become the authority for safety,
diagnosis, therapeutic restriction, vaccination logic or release eligibility.

### Where Phase 2 fits

The `/api/search` engine built in Phase 2 is the **Recipe Engine** box. It is reusable
rather than superseded. Everything upstream (integration gateway, canonical profile,
clinical approval) and everything downstream (book assembly, language, PDF, governance)
does not exist.

---

## 5. Generation state machine

From SRS Appendix A. Nothing in the current codebase models any of it.

| State | Meaning | Next |
|-------|---------|------|
| `DRAFT_PROFILE` | Profile exists, may be incomplete | `READY_FOR_EVALUATION` / `VALIDATION_FAILED` |
| `VALIDATION_FAILED` | Critical data missing or invalid | `DRAFT_PROFILE` |
| `READY_FOR_EVALUATION` | Critical data present | `CLINICAL_REVIEW_REQUIRED` / `READY_FOR_CLINICAL_APPROVAL` |
| `CLINICAL_REVIEW_REQUIRED` | Human decision needed | `READY_FOR_CLINICAL_APPROVAL` / `ON_HOLD` |
| `CLINICALLY_APPROVED` | Clinical snapshot locked | `GENERATING` |
| `GENERATING` | Assembly in progress | `DRAFT_READY` / `GENERATION_ERROR` |
| `DRAFT_READY` | Books and shortlist available | `REVIEW_IN_PROGRESS` |
| `REVIEW_IN_PROGRESS` | Mandatory QA active | `APPROVED_FOR_RELEASE` / `DRAFT_READY` |
| `APPROVED_FOR_RELEASE` | All gates pass | `RELEASED` |
| `RELEASED` | Immutable bundle delivered | `FOLLOW_UP` / `WITHDRAWN` |
| `WITHDRAWN` | Release withdrawn after incident | Corrected new release |

Generation is asynchronous and job-based. The Phase 2 operator console is synchronous
request-response and cannot express any of this.

---

## 6. API surface the SRS specifies

None of these endpoints exist.

| Method | Endpoint | Purpose |
|--------|----------|---------|
| POST | `/v1/book-engine/cases` | Create case |
| PATCH | `/v1/book-engine/cases/{id}/profile` | Update normalized profile |
| POST | `/v1/book-engine/cases/{id}/evaluate` | Run rule evaluation |
| POST | `/v1/book-engine/cases/{id}/approve-clinical` | Lock clinician-approved snapshot |
| POST | `/v1/book-engine/cases/{id}/generate-preview` | Start Book 1/2 preview generation |
| GET | `/v1/book-engine/jobs/{job_id}` | Generation status and issues |
| PATCH | `/v1/book-engine/jobs/{job_id}/recipe-selection` | Approve or replace shortlist from eligible pool |
| POST | `/v1/book-engine/jobs/{job_id}/approve` | Record reviewer decision |
| POST | `/v1/book-engine/jobs/{job_id}/release` | Create immutable release |
| POST | `/v1/book-engine/cases/{id}/follow-up` | Create follow-up snapshot |
| GET | `/v1/book-engine/releases/{release_id}` | Retrieve released assets and audit metadata |

Controls required on all of them: service-to-service auth plus user-level audit identity,
role-based authorization, idempotency on create/generate/release, schema validation with
explicit error codes, no customer secrets in public verification URLs, and immutable
timestamps on every clinically meaningful approval event.

---

## 7. Tables the SRS specifies

None of these exist. The 21 provider tables and the annotation/seed tables built in
Phase 1 cover the *master data* row of this list only.

| Table | Purpose | Versioning |
|-------|---------|-----------|
| `book_engine_case` | Workflow linkage to consultation case | Mutable workflow state |
| `child_profile_snapshot` | Normalized child data | Immutable per generation/release |
| `master_registry` | Active master versions and status | Versioned |
| `generation_job` | Generation state machine | Mutable state, versioned drafts |
| `job_rule_result` | Rules applied to a job | Immutable per job version |
| `job_recipe_selection` | Candidates, selected recipes and scores | Versioned by draft |
| `review_record` | Human approvals, failures, comments | Immutable |
| `book_release` | Final release metadata | Immutable |
| `file_asset` | PDF and print assets with hashes | Immutable after release |
| `content_block` | Book 1/2 modular content | Versioned |
| `language_term` | Terminology and localization | Versioned |

**Hard rule from the SRS, section 5:** presence in a master does not equal production
approval. Only records with the required review status and a `Release_Eligible` flag may
appear in a released customer book. Every one of the 940 recipes currently carries
`Review_Status = Draft`, so under this rule **nothing in the database may currently ship**.

---

## 8. Human review gates

Six roles, five content gates plus a release owner. None implemented.

| Reviewer | Reviews | Emits |
|----------|---------|-------|
| Clinician / Pediatrician | Clinical history, growth interpretation, rules, red flags, vaccination and development issues | `clinical_pass` |
| Nutrition reviewer | Targets, food pattern, recipe shortlist, substitutions, cross-book consistency | `nutrition_pass` |
| Safety reviewer | Allergy, choking and texture, food safety, exception logic | `safety_pass` |
| Language reviewer | Naturalness, terminology, meaning preservation | `language_pass` |
| Editorial / design reviewer | Readability, premium appearance, consistency, layout | `editorial_pass` |
| Release owner | All required gates and version metadata | `release_approval` |

Approved production records are never overwritten; a change creates a new version. Every
released bundle records the exact profile snapshot, master versions, content versions,
reviewer approvals, generated file hashes and release id.

The template contract also specifies a **reviewer preview UI**: a structured web preview
in the same component order as the PDF, with source block ids, active rule ids and review
status in an admin-only side panel, per-section approve/revise, and a mandatory
whole-book approval before release. This is closer to the Phase 2 console than anything
else described here, and is the natural next frontend.

---

## 9. Ranking weights the SRS specifies

These differ from the NT00-NT12 rubric implemented in Phase 1, and the SRS is explicit
that they are **product-ranking defaults, not clinical evidence** - administrators may
tune them after pilot validation. Hard filters remain non-configurable.

| Component | Weight |
|-----------|--------|
| Nutrition target match | 30 |
| Age/texture exact match | 20 |
| Culture/location match | 15 |
| Preference match | 10 |
| Ingredient availability | 8 |
| Diversity bonus | 7 |
| Budget fit | 5 |
| Time/equipment fit | 5 |
| Duplicate penalty | 0 to -20 |

Reconciling these with the provider's own NT00-NT12 axis weights is unresolved. They are
two different rubrics from the same provider and nobody has said which wins. See
`docs/not-built.md`.

---

## 10. Multilingual layer

Not built, and not designed beyond the SRS's requirements.

- Canonical clinical meaning exists independently of language. Localization operates on
  structured content blocks; clinical logic is never regenerated per language.
- **Locked tokens** for numbers, units, allergen names, vaccine names, ids and critical
  warning text. A translation that changes a number or unit is a validation failure and
  **blocks release**.
- Language-specific preferred terminology, regional food names, and prohibited or awkward
  phrases.
- Native-language QA before a language becomes production-approved.
- Canonical and localized versions stored separately so meaning drift is testable.

The culture master carries 21 distinct `primary_languages` values across the 27 in-scope
culture codes, including Bengali, Hindi, Tamil, Telugu, Malayalam, Kannada, Marathi,
Gujarati, Punjabi, Odia, Assamese and Konkani. None of these are wired to anything.

---

## 11. Generation guardrails

The SRS draws the line explicitly. Recorded here because it constrains any future work.

**Permitted:** natural-language drafting from approved structured facts; controlled
translation and localization; recipe ranking explanation; editorial variation and
parent-friendly phrasing; candidate recipe ideation *for review*; summarization of
approved consultation inputs for reviewer convenience.

**Prohibited autonomously:** diagnosing a child; inventing clinical history or missing
measurements; overriding allergies, red flags or clinician restrictions; creating
therapeutic specialist diets without approved targets; changing vaccine schedule logic;
changing numeric amounts or units during translation; publishing a new recipe directly to
a customer book; using arbitrary live-web text as clinical truth.

Prompts receive structured approved JSON, never raw narrative. Each call specifies allowed
fields, prohibited transformations, language and reading level, locked tokens, target
content-block schema and output JSON schema. Output is validated before it enters
rendering.

A newly proposed recipe enters a **Candidate Review Queue** and receives a canonical
`Recipe_ID` only after ingredient mapping, age/texture classification, allergen and safety
checking, nutrition review, preparation-method review, evidence linkage and human
approval.

---

## 12. PDF rendering requirements

Not built. No renderer, no templates, no fonts selected.

- Renderer receives only validated `Book1Document` / `Book2Document` JSON.
- `component_type` plus `page_style` selects a template.
- Human-designed HTML/CSS templates for MVP; server-side renderer or headless browser.
- Never split an ingredient quantity from its ingredient name; avoid splitting a numbered
  method step; long trackers repeat headers.
- A safety panel can never be hidden by page-break overflow.
- Unicode-capable fonts for every production language.
- Printed trackers use larger rows than screen-only tables.
- Screen PDF is **not** automatically press-ready; print needs its own template and bleed
  profile.
- Overflow tests must pass for English, Bengali and Hindi.

Palettes are specified: Book 1 deep navy / teal / warm cream / muted gold; Book 2 deep
plum / warm rose / cream / muted gold; warnings deep red on light warm red. Accessibility
floor: 9.5 pt body, 8.5 pt table, WCAG-aware contrast, never colour-only meaning.

---

## 13. The provider's own MVP roadmap

From SRS section 24. Indicative durations, theirs not ours.

| Phase | Duration | Deliverable |
|-------|----------|-------------|
| 0. Freeze masters | 2-4 weeks | Approved MVP subset, critical evidence/safety gaps resolved |
| 1. Database import | ~2 weeks | SQL schema, import scripts, validation report |
| 2. Consultation adapter | 1-2 weeks | Child_Profile API integration |
| 3. Rule engine | 3-4 weeks | Age/clinical/safety/nutrition evaluation |
| 4. Recipe engine | 3-4 weeks | Hard filter + ranking + diversity shortlist |
| 5. Book 1 generator | ~3 weeks | Structured Book 1 + draft PDF |
| 6. Book 2 generator | 3-4 weeks | Structured recipe book + draft PDF |
| 7. Multilingual V1 | 2-3 weeks | Initial production languages + terminology QA |
| 8. Review portal & release | 2-3 weeks | Approval workflow + immutable release |
| 9. Pilot | 4-6 weeks | 50-100 reviewed cases + QA report |
| 10. Scale | Ongoing | More languages, recipes, automation, analytics |

Phases 1 and 4 are substantially done. The SRS notes phases can overlap but clinical and
safety validation must not be compressed to accelerate software delivery.

---

## 14. Acceptance criteria

From SRS 23.2, for whenever this is built:

- All critical master ids import without broken foreign-key relationships.
- Known safety test cases produce **zero** prohibited recipe leakage.
- Every generated recipe traces to a production-approved `Recipe_ID` and mapping version.
- Every released book links to an immutable profile snapshot and exact master versions.
- English plus selected MVP languages pass native-language QA.
- At least 10 synthetic profiles pass internal end-to-end QA before a real pilot.
- A pilot of roughly 50-100 real cases uses human review for **every** bundle, recording
  turnaround time, corrections and parent feedback.

Regression scenarios named in 23.1: infant below complementary-feeding age produces no
solid recipe; egg-allergic child gets no egg or mapped derivative; parent request for an
excluded food loses to safety; specialist disease without specialist target holds
generation for review; Bangla-BD output keeps canonical quantities and allergens
unchanged; follow-up after release generates a new version leaving the prior release
untouched.

---

## See also

- `docs/not-built.md` - the register of specific missing inputs, data and behaviour
- `docs/decisions.md` - rulings made and why
- `CLAUDE.md` - Phase 1 and Phase 2, both built
