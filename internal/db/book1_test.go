package db_test

import (
	"context"
	"strings"
	"testing"
)

func TestBook1RowCountsMatchTheWorkbook(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	cases := []struct {
		table string
		want  int
	}{
		{"book1_content_block", 32},
		{"book1_vaccine_schedule", 44},
		{"book1_development_milestone", 33},
		{"book1_monitoring_template", 18},
		{"book1_illness_feeding_block", 5},
		{"book1_assembly_step", 16},
		{"book1_evidence_source", 13},
		{"book1_release_check", 15},
		{"book1_daily_life_module", 13},
	}
	for _, c := range cases {
		t.Run(c.table, func(t *testing.T) {
			var n int
			// Table names come from this test's own literal list, never from input.
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+c.table).Scan(&n); err != nil {
				t.Fatalf("count %s: %v", c.table, err)
			}
			if n != c.want {
				t.Fatalf("%s holds %d rows, workbook has %d", c.table, n, c.want)
			}
		})
	}
}

// TestAICanDraftGateIsPinned fixes the five blocks the provider marks as blocks no
// drafted text may occupy. It is written before any assembler exists, deliberately: a
// constraint that predates the code it constrains is cheap, and one retrofitted after an
// assembler is written is an argument.
func TestAICanDraftGateIsPinned(t *testing.T) {
	pool := testPool(t)
	rows, err := pool.Query(context.Background(),
		`SELECT block_id FROM book1_content_block WHERE ai_can_draft = 'N' ORDER BY block_id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	// B1-009 vaccination schedule, B1-011 milestone surveillance, B1-012 developmental
	// red flags, B1-014 development by age, B1-022 reference and disclaimer.
	want := []string{"B1-009", "B1-011", "B1-012", "B1-014", "B1-022"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ai_can_draft = 'N' blocks are %v, want %v. If the provider changed this "+
			"set, update the list here in the same commit that re-imports -- never widen "+
			"it silently, because widening it permits drafted text into a block the "+
			"provider closed.", got, want)
	}
}

func TestBookOrderIsCompleteAndNotBlockIDOrder(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// Every position 1..32 used exactly once, so an assembler sorting by book_order
	// produces a book with no hole and no collision.
	var distinct, minOrder, maxOrder int
	err := pool.QueryRow(ctx,
		`SELECT count(DISTINCT book_order), min(book_order), max(book_order) FROM book1_content_block`).
		Scan(&distinct, &minOrder, &maxOrder)
	if err != nil {
		t.Fatalf("order stats: %v", err)
	}
	if distinct != 32 || minOrder != 1 || maxOrder != 32 {
		t.Fatalf("book_order is not a complete 1..32 permutation: distinct=%d min=%d max=%d",
			distinct, minOrder, maxOrder)
	}

	// And it genuinely differs from id order, so a future refactor that "simplifies" by
	// sorting on block_id is caught here rather than by a reader of a wrong book.
	var mismatches int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT block_id,
			       row_number() OVER (ORDER BY block_id)  AS by_id,
			       row_number() OVER (ORDER BY book_order) AS by_order
			FROM book1_content_block
		) t WHERE by_id <> by_order`).Scan(&mismatches)
	if err != nil {
		t.Fatalf("order comparison: %v", err)
	}
	if mismatches == 0 {
		t.Fatal("book_order now matches block_id order; the daily-life blocks B1-023..B1-032 " +
			"are supposed to sort into positions 15-24. Verify the import before relaxing this.")
	}
}

// TestGuidanceColumnsAreNotForeignKeys documents why nutrition_target_link is text. If a
// future change makes every value resolve, a mapping table becomes a reasonable choice --
// but it must be a deliberate decision, not an assumption a parser quietly encoded.
func TestGuidanceColumnsAreNotForeignKeys(t *testing.T) {
	pool := testPool(t)
	var unresolvable int
	err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM book1_content_block b
		WHERE b.nutrition_target_link IS NOT NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM nutrition_target_master n
		      WHERE n.target_code = b.nutrition_target_link)`).Scan(&unresolvable)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if unresolvable == 0 {
		t.Fatal("every nutrition_target_link now resolves to a target_code; the column " +
			"could become a real foreign key. Make that call explicitly rather than " +
			"leaving this test asserting the opposite.")
	}
	t.Logf("%d of 32 blocks carry a nutrition_target_link that is guidance text rather "+
		"than a target code (e.g. \"NT02/03/04/05\", \"All active targets\", \"N/A\")", unresolvable)
}

// TestReleaseChecklistIsUnrun is a safety assertion, not a data assertion. Every check
// ships blank. If one is ever populated, it happened on this side, and a locally-passed
// release check is the clearest possible form of marking unapproved data approved.
func TestReleaseChecklistIsUnrun(t *testing.T) {
	pool := testPool(t)
	var passed int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM book1_release_check WHERE pass_yn IS NOT NULL`).Scan(&passed)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if passed != 0 {
		t.Fatalf("%d release check(s) carry a pass_yn value. The workbook ships all 15 "+
			"blank. Nothing on this side may mark a provider release check as passed.", passed)
	}
}

// TestAssemblyStepsRecordTheMissingRange confirms steps 12-14 are genuinely absent from
// the workbook. The plan also asks this test to assert GAP-014 exists in gap_register, but
// GAP-014 is seeded by a separate, independently-running plan's migration 0012, which does
// not exist in this worktree. That half is dropped here; restore it once the two branches
// merge and migration 0012 is present.
func TestAssemblyStepsRecordTheMissingRange(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM book1_assembly_step WHERE "order" BETWEEN 12 AND 14`).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 0 {
		t.Fatalf("steps 12-14 now exist (%d rows). The provider filled the hole -- update "+
			"GAP-014 and this test together.", n)
	}
}
