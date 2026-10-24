# Case source staging

The ZIP beside this file is the unchanged delivery from
`attachment-2908b7d15ab467e4dfb2b61018313801.zip`. Its filename is its SHA-256:
`3ae4477c943f672c4912ae532c1096dfc7da351d9aea9414bf03e523799797e2`.
It contains 60 workbooks, 120 sheets, 6,000 recipes and 6,000 audit rows.
Source statuses and audit assertions remain source text, without conferring approval.

The six document masters are embedded under `internal/bookmasters/masters/v1`.
`internal/bookmasters/catalog.json` records book, language, version, original name
and exact package hash. `bookmasters.Read` returns a verified copy for an explicit
book/language/version key. No rendering or filling path consumes these assets yet.

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
All source rows stay outside the engine views, regardless of their audit text.

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

Integration still requires decisions about template module filling, all 60 versus
11 active cases, and append versus replace. This intake does not select any option
or introduce an approval workflow. Source mappings, eligibility and rendering
remain separate dependent work.
