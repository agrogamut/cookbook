# docs

| File | What it is | State of the thing it describes |
|---|---|---|
| [`handover-2026-08-18.md`](handover-2026-08-18.md) | Session record: what was found, decided and changed on 18 Aug | - |
| [`engine-inputs.md`](engine-inputs.md) | Every field the engine accepts, every legal value, live counts | **built** |
| [`clinical-intake-model.md`](clinical-intake-model.md) | The full clinician-entered profile the books need, and how each field behaves | **not built** |
| [`phase-3-book-engine.md`](phase-3-book-engine.md) | The Book 1 + Book 2 generation engine the provider specified on 17 Aug 2026 | **not built** |
| [`not-built.md`](not-built.md) | Register of missing capability, wrong data and unresolved decisions | - |
| [`next-steps.md`](next-steps.md) | Proposed build order, with what it deliberately avoids | - |
| [`decisions.md`](decisions.md) | Rulings made, with reasoning and cost-if-wrong | - |
| [`superpowers/plans/`](superpowers/plans) | The two executed implementation plans, Phase 2 | **built** |

`CLAUDE.md` at the repository root remains the primary document: it describes Phase 1 and
Phase 2, both built, and holds the hard rules that govern everything here.

## Reading order

Coming to this cold:

1. `CLAUDE.md` - what exists and the rules it was built under
2. `docs/handover-2026-08-18.md` - the most recent state of play, in narrative form
3. `docs/engine-inputs.md` - what you can actually ask the engine
4. `docs/phase-3-book-engine.md` - what it is supposed to become
5. `docs/not-built.md` - what stands in the way
6. `docs/next-steps.md` - a proposed order for closing that gap
7. `docs/decisions.md` - why things are shaped the way they are

Resuming work rather than orienting: start at `next-steps.md` and consult the rest as
needed.

## Two things that mean "Book 1" and "Book 2"

Worth knowing before reading anything else, because the naming collides.

- **Input workbooks** - `MadamGY_Book1_Content_Master_V1_1_DailyLife.xlsx` (daily-life
  guidance, vaccines, milestones) and `MadamGY_Book2_Content_Master_V1.xlsx` (which holds
  the authoritative 14-step `Recipe Selection Logic` sheet).
- **Output books** - the two personalized PDFs the Book Engine produces per child. This is
  the product.

They are named after each other but are not parallel in content. See
`handover-2026-08-18.md` §1.

## Provider source documents

The Phase 3 specification arrived as nine files in the repository root on 17 August 2026,
committed as `84966cd`, and is summarised in `phase-3-book-engine.md`. They now live in
`data/book-engine-spec/`. The originals are authoritative:

- `MadamGY_Knowledge_Book_Engine_SRS_V1 (1).docx`
- `MadamGY_Book1_Book2_JSON_PDF_Template_Contract_V1.docx`
- `MadamGY_Full_Master_TOC_Page_Component_Map_V1.docx`
- `MadamGY_Full_Master_TOC_Page_Component_Map_V1.json`
- `MadamGY_Book1_JSON_Schema_V1.json`
- `MadamGY_Book2_JSON_Schema_V1.json`
- `MadamGY_PDF_Template_Contract_V1.json`
- `MadamGY_Book1_Visual_Prototype_V1.pdf`
- `MadamGY_Book2_Visual_Prototype_V1.pdf`

## Published views

`engine-inputs.md` is also published as a browsable page:
<https://claude.ai/code/artifact/0bcec233-d8a6-477b-bfec-dddc34291eb9>

That page is a convenience view and is not version-controlled. **The markdown in this
folder is the source of truth**; if the two disagree, the markdown wins.
