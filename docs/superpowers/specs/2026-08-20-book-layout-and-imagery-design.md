# Book layout and imagery - design

**Date:** 2026-08-20
**Status:** approved, not started
**Plan:** `docs/superpowers/plans/2026-08-20-book-layout-and-imagery.md`

Both books print. Neither reads as a book. This is the design for the layout defects, the
preview architecture, and the one thing Book 2 has never had: a picture.

---

## 1. What was measured

Printed from the running API against the `Ananya Roy` preview child (4y5m, vegetarian, West
Bengal, confirmed peanut allergy), rasterised at 100 dpi, and read page by page. Fill is the
last text baseline on the sheet as a fraction of the 255mm text block, taken from
`pdftotext -bbox`.

| Book | Pages | Pages ending above 62% of the block |
|---|---|---|
| Book 1 | 47 | **18 (38%)** |
| Book 2 | 18 | 4 (22%), all of them display pages by design |

Book 1's worst sheet is page 42: **three writing lines and a repeated table header, 92% white.**
Pages 5, 15, 6 and 12 end at 23%, 30%, 33% and 39%.

That number is the whole complaint. A reader turning a page to find four lines on it concludes
the document is broken before reading a word of it.

---

## 2. Root causes, each traced

### 2.1 Every Book 1 block starts a new page

`internal/book/templates/book1/body.html:15` wraps every block in `<section class="page-break">`.
Thirty-one blocks, thirty-one page starts, whatever the block weighs. A block holding three
writing lines gets a sheet of its own.

The provider already declares the grouping this should use. `book1_content_block.part` groups
the thirty-two blocks into **fifteen parts** (A-O), and the pairs are meaningful: A is profile
plus consultation summary, B is growth plus trend, E is the vaccination pair, F the development
pair. Part J is the ten daily-life domains and each of those genuinely fills a sheet.

Breaking on part change rather than block change uses the workbook's own structure and removes
roughly sixteen page starts.

### 2.2 The cover is left-hung, and overflows when a photograph is present

Two separate defects in the same twelve lines of `tokens.css`.

**Centring.** Line 419 sets the auto side margins:

```css
.cover h1, .cover .cover-sub { margin-left: auto; margin-right: auto; }
```

and lines 425 and 432 then throw them away:

```css
.cover h1     { ... margin: 0 0 5mm; max-width: 140mm; }
.cover .cover-sub { ... margin: 0;   max-width: 116mm; }
```

The `margin` shorthand resets `margin-left` and `margin-right` to zero. Both boxes are capped
well inside the 170mm column and both sit hard against its left edge, with their text centred
inside them. The cover title prints about 15mm left of the page centre and the standfirst about
27mm left of it. This is the "everything is left aligned" the operator sees.

**Overflow.** The cover is plain block flow with a declared 105mm drop, chosen after two flex
constructions were measured and abandoned (the reasoning is in `tokens.css` above `.cover` and
still holds). With a photograph the stack measures:

| Band | Height |
|---|---|
| kicker + rule | 7mm |
| `.cover-main` drop | 105mm |
| portrait 62mm + caption + gap | 75mm |
| title, one line | 19mm |
| standfirst, three lines | 18mm |
| facts block, incl. 24mm gap | 38mm |
| **total** | **262mm into a 255mm block** |

The facts row is pushed onto page 2 and prints alone above 250mm of white; the contents page
moves to page 3. Reproduced exactly, and it is the second screenshot the operator sent.

The drop is a constant and the content above it is not. The fix is a declared drop *per cover
variant* - the template knows whether a photograph is present, so the stylesheet can too.

### 2.3 Table rows strand

A tracker's writing rows split anywhere. Page 42's three rows are the tail of the food-diversity
grid. Chromium honours `break-inside: avoid` on `tbody`, so writing rows emitted in groups of
four move as groups and a tail of one to three cannot happen.

### 2.4 Continued tables lose their name

Pages 17, 42, 44 and 47 of Book 1 open with a bare column header and no heading - the reader has
no idea what table they are looking at. `thead` already repeats (that is why the column header is
there at all). Putting the tracker's title into `thead` as a spanning row makes the name repeat
with it.

### 2.5 Words break mid-syllable in narrow columns

`overflow-wrap: break-word` was added with `table-layout: fixed` to stop the 79%-scale defect. It
does stop it, and in narrow columns it breaks inside words:

```
HEAD CIRCUMFER / ENCE          Language/cognitiv / e
Social/communicati / on        IAP-STG-CONSTIPATIO / N
Reference z-score/interpretatio / n
```

A broken identifier is a wrong identifier. The columns are uniform because nothing measures the
content; the fix is to size each column from the longest unbreakable token it must hold, in Go,
and emit a `colgroup`. Fixed layout stays - the scale defect must not come back.

### 2.6 Pages open on an orphaned warning

Pages 28 and 30 begin with a `Warning: when to seek advice` callout belonging to the previous
domain. A callout is short enough to keep with what precedes it; the domain around it is not.

### 2.7 The console preview is not a page

`tokens.css` carries exactly one screen rule - `@media print { .provisional { display: none } }`.
Everything else is print geometry, and `@page` does nothing on screen. In the console's iframe
the document lays out at the iframe's own width, about 1700 CSS pixels against the 170mm text
block it was designed for. Every grid spreads, every table stretches, prose capped at 108mm sits
in a 450mm column, and no page break is visible because there are no pages.

The operator is judging the books through this. Most of what looks misaligned in the console is
correct in the PDF, and the two defects that are real (2.2 and 2.1) are invisible in it.

---

## 3. Decisions

### 3.1 The console shows the printed PDF

The console embeds the real printed PDF, not the HTML. Printing costs one warm-browser render
now that the browser is shared, and it is the only way what the operator approves is what the
family receives.

The HTML preview stays as the fallback when Chromium is unavailable (503), and gains the screen
styling it never had: a 210mm sheet on a grey ground, page margins drawn, and a visible sheet
boundary at every forced break. It is labelled an approximation in the UI, because without
pagination it is one.

A side effect worth having: the console already holds the PDF blob, so "Open in a new tab" stops
re-printing and opens what is already on screen.

### 3.2 Book 2 gets pictures, and they are not photographs

The operator asked for images "as close as possible to the food". The honest answer has three
parts.

**No external photographs, and this is settled.** `data/external/indian_food_dataset.csv` does
carry an `image-url` column across 3,970 loaded rows, so the option is real enough to need
refusing on the record:

1. The image is of a *different dish*. The format map licenses an external row to supply
   *method text for the same format* - a khichdi method for a khichdi. A photograph is a stronger
   claim: it says this is what your dish looks like. It is the wrong-match failure already
   measured at four calibration thresholds, in the one medium where a reader cannot check it.
2. The corpus has an **unstated upstream licence**, already an open blocker. Its images are
   third-party photographs hotlinked from recipe sites. Printing them into a document handed to a
   family is a clearer infringement than quoting the text.
3. Printing would have to fetch arbitrary remote URLs, the same security question `CLAUDE.md`
   already declines for bulk cover photographs.
4. The print browser blocks all network by design (`--host-resolver-rules=MAP * ~NOTFOUND`).
   Remote images cannot load, and unblocking it to fetch pictures would undo a security fix
   shipped three commits ago.

`GAP-025` stands unchanged: recipe photographs do not exist and are not coming from a dataset.

**Two drawn devices, both derived from provider columns.**

*The dish-format mark.* The provider encodes the dish format in the recipe name, and the
vocabulary is closed: **twenty-eight formats cover all 940 in-scope recipes**, verified by
matching every name against the list. Those twenty-eight collapse to **eleven drawn archetypes**
(bowl, porridge bowl, mash, khichdi pot, patty, pancake, stuffed flatbread, wrap, finger bites,
upma plate, portable snack). The mark is line art, unmistakably a drawing, captioned with the
format name it depicts - so its claim is explicit and checkable against the recipe title above
it. It asserts nothing the provider did not record.

*The composition band.* A horizontal band showing each food group's share of the recipe's
ingredient mass, from `recipe_ingredient_mapping.quantity_g` and `ingredient_master.food_group`,
collapsed 21 groups to 7 by a hand-written map. Labelled `derived`, carrying its formula and its
source rows like every other derived value in this project. It tells a parent at a glance that a
dish is mostly grain, which is closer to "what is this food" than a photograph of someone else's
lunch.

**A real photograph gets a home, empty.** `recipe_photo` is created with source, credit, licence
and consent columns and zero rows. When a row exists for a recipe, the photograph prints and the
mark steps aside. `GAP-025` is re-measured from that table's count, so the gap closes itself when
commissioned photographs arrive rather than needing a code change to notice.

### 3.3 Alignment: centre the furniture, left-align the content

Covers, chapter openers, page heads and contents centre. Everything a reader reads or writes in
stays flush left: prose, tables, forms, method steps, ingredient lists. Centred body text is
harder to read and centred form fields are unusable.

The ragged-void problem is not solved by centring but by *rhythm*: prose capped at the 108mm
measure currently sits in a 170mm column with 62mm of nothing beside it. Prose blocks that sit
alone get the measure and are centred as blocks; prose that sits beside a table shares the
table's grid.

### 3.4 Page fill becomes a measured guard

`rendered + reported == total` held perfectly while the book printed nearly empty. Fill is the
count that would have caught it. A test prints both books and asserts that no more than a
declared budget of pages end above 62% of the text block, with the budget lowered by each task
that improves it: **18 today, 4 at the end** (Book 1's contents tail plus display pages).

It needs `pdftotext` from poppler and skips without it, the same contract as the Chromium print
test. A skipped guard is not a passing guard.

---

## 4. Out of scope

- Page numbers in the contents. Chromium assigns folios at print time and the template cannot
  know them. Unchanged.
- `B1-004` growth trend interpretation. Still unmapped, still for the reason in `CLAUDE.md`.
- Commissioning photographs. This design builds the slot; filling it is procurement.
- The special-care stop gate, the allergy hard filter, and every safety boundary. Untouched.
