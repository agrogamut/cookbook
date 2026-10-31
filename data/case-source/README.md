# Case source intake and triage

The retained original ZIP is the unchanged delivery from
`attachment-2908b7d15ab467e4dfb2b61018313801.zip`. Its filename is its SHA-256:
`3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2`.
It contains 60 workbooks, 120 sheets, 6,000 recipes and 6,000 audit rows.
Source statuses and audit assertions remain source text, without conferring approval.

The current default archive is
`4efae52ed55f1b2fa6bf91df5ef00ac0c7801680c54f75d093366665d7689ef3.zip`.
Its Bengali source contract is `internal/casesource/final_source.json`.
`-baseline` supplies the retained original ZIP for comparison. Acceptance of this
exact archive is an engineering contract, not supplier or clinical approval.

Validate the recipe delivery without a database:

```fish
go run ./cmd/import-cases -check
```

Import into an explicitly selected disposable staging database:

```fish
go run ./cmd/import-cases -database-url 'postgres://user:password@127.0.0.1:55432/staging?sslmode=disable'
```

Use `-archive /path/to/corrected.zip` for a later snapshot. The command applies
repository migrations to the selected database. It reads no database setting from
the environment and invokes neither the provider importer nor book generation.

Five tables keep source intake separate from the candidate pool:

- `case_source_archive`: namespace, archive SHA-256, unchanged ZIP bytes, parser
  version and first-import timestamp. Repeating identical bytes does nothing.
- `case_source_workbook`: logical case number, original member path and member hash.
- `case_source_sheet`: exact sheet name, XML part, header row and ordered raw headers.
- `case_source_row`: worksheet row, original recipe ID where present, ordered raw
  cells and row hash. Preamble, header and blank cells retain their coordinates.
- `case_source_ingredient`: ordinal, original ID/name/quantity tokens and exact
  decimal grams. Repeated IDs remain separate occurrences, including within one row.

Logical recipe identity is namespace plus case plus original recipe ID. A revision
locator is archive hash plus case, sheet and row; member hashes identify the source
workbook. The row hash is SHA-256 of the ordered `Cell` JSON encoded by parser version
1, including coordinates, cell type, underlying XML value and resolved text. Shared
string indices remain in `raw_value`; resolved text remains in `value`. Empty fields
may be omitted in JSON and decode as empty strings. Exact package bytes retain all
formatting and XML details. No normalization, nutrient computation, canonical
ingredient mapping or clinical decision occurs here.

Corrections insert new immutable snapshots. Missing recipes in a later delivery do
not retire or delete any earlier row. Database triggers reject updates, deletes
and truncation of these tables. The schema rollback explicitly drops staging only.
Raw source rows alone do not activate candidates, regardless of their audit text.
The existing `-activate` path separately applies the accepted source policy and
preserves revision precedence. Neither triage nor correction validation uses it.

Parser version 1 requires the supplied two sheet names, row-4 header contract and
matching recipe/audit ID sets. It accepts shared, inline and rich-text strings and
literal numeric values without float conversion. Formula cells, changed headers,
unaligned ingredient lists and unsupported decimal syntax are rejected explicitly.
ZIP traversal, duplicate members/cases, symlinks, encryption, invalid checksums and
oversized expansions are rejected. Parsing is in memory; member paths are never
extracted. Limits are 32 MiB compressed archive, 128 outer members, 16 MiB per member,
256 parts per workbook and 128 MiB cumulative workbook expansion.

Verification uses the retained delivery, a separate spreadsheet reader and local
Postgres. Set `CASE_SOURCE_TEST_DATABASE_URL` to a disposable loopback database whose
role can create test databases, then run `go test ./internal/casesource`. Each
database test creates and removes its own database. The suite checks every raw cell
and persisted occurrence, repeat and concurrent import, correction retention,
rollback, provider importer independence and unchanged existing rows/candidates.

## Check-only source gaps

No newer approved whole-bean formulation or completed nutrition/clinical correction
has been supplied. The accepted archive contains 5,689 recipes with all seven
nutrition fields blank and 6,000 literal `General` clinical tags. These are source
facts. Blank values remain null, numeric zero remains zero, and `General` is not
condition-specific clearance or a software defect. No superseded values are restored.

From an ordinary checkout, write the deterministic per-RecipeID report to an
explicit output file:

```fish
go run ./cmd/import-cases -check -triage > /tmp/case-triage.json
```

`-archive` and `-baseline` also accept explicit supplied paths. Each case entry
contains archive/member/raw-row hashes, workbook, sheet, source row, revision ID,
rule fingerprint, current values and ingredient occurrence ordinals/raw tokens.
Findings and requirements reuse the intake and final policy contracts. The report
does not query a running database or evaluate an individual child's eligibility.

The final source policy identifies 287 case tofu holds through exact ingredient
ID/name occurrences. These are pending reformulations, not conversions. Present
dairy ingredients and the `ING0074` mixed-soy ambiguity remain intact. No
recipe-name heuristic substitutes for ingredient evidence, and mixed soy alone is
not a blanket exclusion.

The other 54 provider holds are a separate source observation. They are included
only when an operator explicitly supplies the existing compatibility JSON whose
SHA-256 is
`7f7690d207692f5adfefe32656e2fab2ca9f4a609991e508ad1f4b94c110cfeb`:

```fish
go run ./cmd/import-cases -check -triage -provider-report /path/to/compatibility.json > /tmp/combined-triage.json
```

There is no implicit receipt, profile, cache or private-directory dependency. The
optional supplement's `provider_inputs` and `tofu.provider_records` bind workbook
hashes, RecipeIDs, worksheet rows and ingredient occurrences. Counts come from
those records, never a hard-coded addition of 54. The report labels them
`provider_observation`; their full raw-row hash and runtime revision are unavailable
and remain null/absent. This is a hash-bound intake observation, not verification
of current database state. All 11 provider dairy control rows retain their source
allergen tags and ingredient occurrences. Without it, provider and combined counts remain null,
and case-only 287 is never labelled complete 341 coverage. An unknown supplement
hash is rejected.

## Nonactivating correction validation

A separately supplied candidate ZIP and `source-correction-v1` JSON manifest can
be checked against the exact accepted final archive:

```fish
go run ./cmd/import-cases -check -corrections /path/to/corrections.json -candidate-archive /path/to/candidate.zip
```

The manifest describes actual candidate cells; it never inserts values. Top-level
keys are `version`, `base_archive_sha256`, `candidate_archive_sha256` and
`corrections`. Every correction requires:

| Key | Required binding |
| --- | --- |
| `recipe_id` | Existing ID in both complete parsed deliveries |
| `base_member_sha256`, `candidate_member_sha256` | Exact workbook bytes |
| `base_row_sha256`, `candidate_row_sha256` | Ordered raw `Cell` JSON hashes, using the intake representation |
| `fields` | Object mapping every changed master field to its exact unit below |
| `evidence` | Source document hash/reference and review reference, plus applicable fields below |
| `translations` | Changed English/Bengali bindings, or an empty array when none change |

Supported `fields` values are deliberately narrow:

| Source field | Unit |
| --- | --- |
| `Energy_kcal` | `kcal` |
| `Protein_g`, `Carb_g`, `Fat_g`, `Fibre_g` | `g` |
| `Iron_mg`, `Calcium_mg` | `mg` |
| `Cost_per_Serving_INR` | `INR` |
| `Clinical_Tag`, `Safety_Rule`, `Nutrient_Gap` | `text` |

Every `evidence` object requires `document_sha256`, `document_reference` and
`review_reference`. Any nutrition or nutrient-gap correction additionally requires
all seven candidate nutrients as nonnegative decimals, a positive existing
`Serving_Size_g`, positive `cooked_yield_g`, `nutrition_source` identifying the
measurement/calculation inputs, and `nutrition_basis` equal to `per_recipe`,
`per_serving` or `per_100g`. The validator does not convert between bases or calculate
nutrition. Cost changes require nonnegative INR and `cost_source`. Clinical/safety
changes require nonempty supplied text and `clinical_source` identifying the
indication, exclusions and prerequisites. References are checked for presence;
their scientific adequacy and real signatures require external review.

For a changed safety translation, each `translations` entry contains `field`
(`safety`), `ordinal` (0 for this non-ingredient field), `english_sha256` and
`bengali_sha256`, hashing exact supplied strings. Changed English needs a newly
bound, nonempty Bengali value in the candidate translation sheet. The existing
parser also binds recipe/field/ingredient ordinals and source quantities. Unchanged
English cannot acquire an undeclared Bengali-only edit through this validator.

Unknown/stale recipe or byte identities, duplicate/unknown JSON keys, wrong units,
undeclared or incomplete corrections, missing translations, ordinal/English-hash
mismatches and unsupported ingredient/method/serving replacements are rejected.
Glossary English, Bengali and print-site content must remain unchanged; row order
and formatting do not establish a semantic change. Translation `Note` edits are
also unsupported, including alongside an otherwise valid safety translation.
Formulation replacements still require a supplier contract with exact ingredient
forms and gram quantities, water, method and texture, measured yield/serving,
nutrition/cost source inputs and review evidence. Whole soybeans are not inferred
to be interchangeable with tofu by weight.

Successful output sets `structurally_valid: true` while leaving
`activation_permitted`, `supplier_approval_verified` and
`clinical_approval_verified` false. A typed reviewer/reference is not approval.
The candidate remains outside the accepted activation contract. This adds no new
global approval workflow and changes no per-book review or release behavior.

All new flags require `-check`, reject `-activate` and return before database
creation. `-corrections` and `-candidate-archive` must be supplied together;
`-provider-report` requires `-triage`. Both report modes may be combined. Existing
`-check`, `-archive`, `-baseline`, `-database-url`, `-details` and ordinary
import/activation behavior are preserved. Manifests are limited to 4 MiB and
optional provider observations to 16 MiB; candidate ZIP limits match the parser.

Focused checks use the retained real archives and temporary negative-control
fixtures. No database is required:

```fish
go test ./internal/casesource ./cmd/import-cases -run 'Test(Triage|CorrectionManifest|CLI|CheckInput)' -count=1
```

Set `CASE_SOURCE_PROVIDER_TRIAGE_REPORT` explicitly to the hash-bound supplement
to exercise its positive control. Without it that optional check reports a skip;
case triage, invalid-supplement rejection and correction checks still run.
