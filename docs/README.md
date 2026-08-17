# docs

| File | What it is | State of the thing it describes |
|---|---|---|
| [`engine-inputs.md`](engine-inputs.md) | Every field the engine accepts, every legal value, live counts | **built** |
| [`phase-3-book-engine.md`](phase-3-book-engine.md) | The Book 1 + Book 2 generation engine the provider specified on 17 Aug 2026 | **not built** |
| [`not-built.md`](not-built.md) | Register of missing capability, wrong data and unresolved decisions | - |
| [`decisions.md`](decisions.md) | Rulings made, with reasoning and cost-if-wrong | - |
| [`superpowers/plans/`](superpowers/plans) | The two executed implementation plans, Phase 2 | **built** |

`CLAUDE.md` at the repository root remains the primary document: it describes Phase 1 and
Phase 2, both built, and holds the hard rules that govern everything here.

## Reading order

Coming to this cold:

1. `CLAUDE.md` - what exists and the rules it was built under
2. `docs/engine-inputs.md` - what you can actually ask it
3. `docs/phase-3-book-engine.md` - what it is supposed to become
4. `docs/not-built.md` - what stands in the way
5. `docs/decisions.md` - why things are shaped the way they are

## Provider source documents

The Phase 3 specification arrived as eight files in the repository root on 17 August 2026
and is summarised in `phase-3-book-engine.md`. The originals are authoritative:

- `MadamGY_Knowledge_Book_Engine_SRS_V1 (1).docx`
- `MadamGY_Book1_Book2_JSON_PDF_Template_Contract_V1.docx`
- `MadamGY_Full_Master_TOC_Page_Component_Map_V1.docx` / `.json`
- `MadamGY_Book1_JSON_Schema_V1.json`
- `MadamGY_Book2_JSON_Schema_V1.json`
- `MadamGY_PDF_Template_Contract_V1.json`
- `MadamGY_Book1_Visual_Prototype_V1.pdf`
- `MadamGY_Book2_Visual_Prototype_V1.pdf`

At time of writing these are untracked. They should be committed.
