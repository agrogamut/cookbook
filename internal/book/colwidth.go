package book

import "strings"

// Table column widths computed from the content, so fixed table layout does not have to break
// words to fit.
//
// `table-layout: fixed` is not negotiable and the reason is recorded above `table` in
// tokens.css: without it one over-wide table silently scales the whole printed document, which
// is how Book 1 shipped with 10.5pt body text landing on paper at 8.3pt, under the contract's
// 9.5pt floor. What fixed layout costs is that every column gets the same width unless
// something says otherwise, and a uniform column narrower than the longest word in it breaks
// that word. Read off printed sheets:
//
//	HEAD CIRCUMFER / ENCE                 Language/cognitiv / e
//	Social/communicati / on               IAP-STG-CONSTIPATIO / N
//	Reference z-score/interpretatio / n   DISH (FROM BOOK / 2)
//
// A broken identifier is a wrong identifier, and the milestone tables paid for it twice: a
// four-line cell where a two-line cell would do makes every row twice as tall, and rows are
// unbreakable, so the table wastes up to a row's height at every page boundary.
//
// So: measure. A column's demand is the longest unbreakable token it must hold, blended with
// the average length of its cells -- the token alone gives a narrow column to long prose, the
// average alone breaks the widest word.
//
// Character counts, not typographic measurement. Both faces are set at the same table size and
// no column here is sized so tightly that the difference between an "i" and an "m" decides
// whether a word fits. The guard is TestNoWordBreaksInsideItself, which reads printed sheets.

const (
	// contentWeight balances the longest token against the mean cell length. At 0 a column is
	// sized purely by its widest word, which starves a column of long prose; at 1 purely by its
	// mean, which breaks the widest word. Measured on the two tables that were breaking.
	contentWeight = 0.45

	// minColumnPct floors every column. It is set by what a person can write in rather than by
	// what a header needs: 12% of the 170mm text block is 20mm, which holds a date or a short
	// note in adult handwriting. Below that a computed width can starve exactly the columns a
	// parent fills, because an empty writing column has no content to demand room with.
	minColumnPct = 12
)

// ColumnWidths returns one percentage per column, summing to 100.
//
// cells is row-major and may be ragged or empty; a row shorter than the header is padded with
// blanks rather than rejected, because a table that renders is more useful than one that panics
// over a missing trailing note.
func ColumnWidths(headers []string, cells [][]string) []int {
	n := len(headers)
	if n == 0 {
		return nil
	}

	demand := make([]float64, n)
	for i, h := range headers {
		demand[i] = float64(longestToken(h))
	}
	for i := range demand {
		var longest, total, count float64
		for _, row := range cells {
			if i >= len(row) {
				continue
			}
			if t := float64(longestToken(row[i])); t > longest {
				longest = t
			}
			total += float64(len([]rune(row[i])))
			count++
		}
		if longest > demand[i] {
			demand[i] = longest
		}
		if count > 0 {
			mean := total / count
			demand[i] = demand[i]*(1-contentWeight) + max(demand[i], mean)*contentWeight
		}
	}

	return normalise(demand)
}

// normalise scales demands to percentages summing to 100, applies the floor, and puts the
// rounding remainder on the widest column.
func normalise(demand []float64) []int {
	var sum float64
	for _, d := range demand {
		sum += d
	}
	out := make([]int, len(demand))
	if sum <= 0 {
		// Nothing to go on -- equal columns, which is what fixed layout would have done anyway.
		even := 100 / len(demand)
		for i := range out {
			out[i] = even
		}
		out[0] += 100 - even*len(demand)
		return out
	}

	for i, d := range demand {
		out[i] = int(d / sum * 100)
		if out[i] < minColumnPct {
			out[i] = minColumnPct
		}
	}

	// Applying the floor can push the total past 100. Take the excess off the widest columns
	// first, one point at a time, and never below the floor -- so the correction lands where
	// there is room for it rather than squeezing an already-narrow column further.
	for total := sumOf(out); total != 100; total = sumOf(out) {
		i := widest(out)
		if total > 100 {
			if out[i] <= minColumnPct {
				break // every column is at the floor; the table has too many columns to fit
			}
			out[i]--
			continue
		}
		out[i]++
	}
	return out
}

func sumOf(xs []int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}

func widest(xs []int) int {
	best := 0
	for i, x := range xs {
		if x > xs[best] {
			best = i
		}
	}
	return best
}

// longestToken returns the length of the longest run in s with no break opportunity in it.
//
// Only whitespace is a break opportunity. Slashes and hyphens are not: "Language/cognitive" and
// "IAP-STG-CONSTIPATION" are single tokens here because breaking either produces a string that
// reads as a different value, and both did exactly that on printed pages.
func longestToken(s string) int {
	longest := 0
	for _, f := range strings.Fields(s) {
		if n := len([]rune(f)); n > longest {
			longest = n
		}
	}
	return longest
}
