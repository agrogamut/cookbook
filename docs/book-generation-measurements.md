# Book generation: token usage and timing

Book endpoints now log `generation_call` for each model request and
`book_generation` for the completed HTTP request. A random `run_id` connects the
records. Logs include totals for each book and a breakdown by operation and model.
They do not include child identifiers, prompts, response text or credentials.

`prompt_tokens`, `output_tokens`, `thought_tokens`, `cached_tokens` and
`total_tokens` come directly from the provider's response metadata. Cached tokens
are already included in prompt tokens. `total_tokens` uses the reported total;
it is not reconstructed by adding overlapping fields.

`missing_usage` counts calls without metadata. If it is nonzero, the recorded
token total is a lower bound. A failed request is not assumed to be free.
Translation retries and responses rejected during JSON decoding still contribute
their reported usage. `request_errors` counts request failures, not content
validation failures.

`elapsed_ms` is the duration of the HTTP request, including assembly, translation,
printing and ZIP creation. `call_ms` adds individual request durations, including
overlapping calls, so it cannot be used as wall-clock generation time.

## Repeat a measurement

Use a disposable database populated by the existing import and enrichment
commands. The measurement command does not run migrations or save a child
profile, but successful fallback recipes are cached by the normal book pipeline.
An empty cache and a warm cache can therefore produce different call counts.

With `DATABASE_URL` and `GEMINI_API_KEY` already set:

```fish
go run ./cmd/book-measure \
  -input sample-books/measurement-input.json \
  -output sample-books/measured-books.zip \
  >sample-books/measured-usage.json \
  2>sample-books/measured-calls.jsonl
```

The input uses the same JSON shape as `/api/books/generate.zip`. The command
calls that handler directly, bypassing HTTP authentication and proxy overhead.
It makes paid requests. The default deadline is 20 minutes, matching the current
print-route limit of 1,200 seconds; `-timeout` can override the measurement limit.
The output ZIP must not already exist. A failed generation removes its reserved
ZIP and still emits the usage summary when the handler returns.

For the first fixture below, create `sample-books/measurement-input.json` with:

```json
{
  "display_name": "Synthetic Timing Fixture",
  "date_of_birth": "2025-03-14",
  "sex": "female",
  "language_id": "bn",
  "region_culture": "West Bengal / East India",
  "diet_type": "Vegetarian",
  "budget_band": "Low",
  "allergens": [
    {"group": "Peanut", "status": "confirmed", "source": "clinician_documented"},
    {"group": "Milk", "status": "confirmed", "source": "clinician_documented"}
  ],
  "conditions": [{"trigger_field": "Constipation_Support", "flag_value": "Yes"}]
}
```

Create the output directory first. Age is derived at run time, so the birth date
above reproduces the recorded age only on the measurement date.

## Local measurements, 2026-09-14

The first fixture was an 18-month-old synthetic child with a vegetarian diet,
West Bengal region, low budget, confirmed milk and peanut exclusions, and the
provider's `Constipation_Support=Yes` flag. No personal record was used.
The database was freshly imported and enriched from checksum-verified datasets.
Every meal chapter filled from existing recipes, so this fixture did not exercise
fresh fallback drafting.

| Stage | Calls | Reported tokens | Calls missing usage |
| --- | ---: | ---: | ---: |
| Book 1 drafting | 8 | 6,674 | 2 |
| Book 1 translation | 52 | 125,495 | 0 |
| Book 2 drafting | 10 | 8,581 | 0 |
| Book 2 translation | 78 | 199,861 | 1 |
| Set total | 148 | 340,611 | 3 |

The set returned HTTP 200 in **528.991 seconds (8m 49s)**. It produced PDFs of
47 and 41 pages. Both contained extractable Bengali text, with no Unicode
replacement characters detected. This checks output generation and text
encoding; it does not establish translation accuracy or every page's layout.

Of the reported total, 63,769 tokens were prompt text, 39,786 were output text,
and 237,056 were thinking tokens. These are measurements for one fixture, not
fixed per-book requirements or a bound for all profiles.

Translation kept the existing batch size of 25 fragments and concurrency of six.
Successful translation calls peaked at 29.785 seconds for Book 1 and 27.704
seconds for Book 2. One failed Book 2 translation request recovered on retry.
The two failed Book 1 drafting calls followed the existing omission behavior.

The earlier eight-minute timeout estimate was stale: the current source already
uses 20 minutes for PDF routes. This run exceeded eight minutes and completed
within the current limit. HTML preview routes still have their separate
30-second limit.

### Fixture that requires fresh fallback recipes

The second completed run used the same profile fields with birth date
`2022-03-14` (54 months at measurement time), plus `max_prep_time_min: 10` and
`max_cook_time_min: 10`. A local preflight confirmed five unfilled recipe slots
and fresh drafting attempts in all three meal chapters. This older age matters:
the existing fallback does not run below 24 months.

| Stage | Calls | Reported tokens | Calls missing usage |
| --- | ---: | ---: | ---: |
| Book 1 drafting | 7 | 8,617 | 0 |
| Book 1 translation | 53 | 126,893 | 0 |
| Book 2 modification notes | 5 | 4,901 | 0 |
| Book 2 fresh fallback recipes | 5 | 34,228 | 0 |
| Book 2 translation | 71 | 170,958 | 0 |
| Set total | 141 | 345,597 | 0 |

The set returned HTTP 200 in **500.672 seconds (8m 21s)**. Book 1 used 135,510
tokens and Book 2 used 210,087. All calls reported usage: 83,474 prompt tokens,
40,379 output tokens and 221,744 thinking tokens. Independent aggregation of the
individual call logs matched the command's total, per-book and per-operation
summaries exactly.

The ZIP passed its checksum check and contained two readable PDFs, of 49 and
41 pages, with extractable Bengali text and no replacement characters detected.
No request failed. The slightly shorter elapsed time than the first run is not
evidence of an optimization: the profiles differ, and the first run encountered
request failures.

**Batching decision:** keep 25 fragments per call. Both completed runs fit the
existing 20-minute deadline. Successful translation calls in the fallback run
peaked at 27.574 seconds for Book 1 and 25.735 seconds for Book 2. A 40-fragment
batch has not been validated, so no speed or reliability improvement is claimed
for it. These two runs exercise ordinary corpus selection and fresh fallback
drafting; they do not establish a maximum latency for every possible profile.

Validation also passed: `go build ./...`, `go vet ./...`, `go test ./...` with
the populated isolated database, usage tests under the race detector, 43 frontend
tests, TypeScript checking, and lint for the changed frontend files. The browser
preview showed both language options using fixture authentication; a submission
test independently verified that choosing Bengali sends `language_id: "bn"`.
