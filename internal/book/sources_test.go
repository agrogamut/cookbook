package book

import (
	"context"
	"testing"
)

// TestLoadBlockSourcesHonoursSeedOrder pins the reason book1_block_source carries an ordinal
// at all. B1-027 seeds two daily-life modules in a deliberate order -- self-feeding before
// dressing -- and the workbook's own row order is alphabetical by id, which happens to agree
// here and would not for a pair named the other way round.
func TestLoadBlockSourcesHonoursSeedOrder(t *testing.T) {
	pool := testPool(t)
	src, err := LoadBlockSources(context.Background(), pool)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	got := src.Daily["B1-027"]
	if len(got) != 2 {
		t.Fatalf("B1-027: want 2 daily-life modules, got %d", len(got))
	}
	if got[0].DailyLifeID != "DL-SELF-01" || got[1].DailyLifeID != "DL-SELF-02" {
		t.Errorf("ordinal not honoured: got %s then %s, want DL-SELF-01 then DL-SELF-02",
			got[0].DailyLifeID, got[1].DailyLifeID)
	}
}

// TestEveryEvidenceSourceReachesTheReferencePage is the reachability half that
// TestBlockSourceSeedResolvesBothWays deliberately skips for this table: B1-022 takes every
// evidence source by rule rather than by seed, so the rule is asserted where it lives.
//
// A source held by this project and never disclosed in the book that relies on it is the
// failure being prevented -- the reference page exists to say what the book was built from.
func TestEveryEvidenceSourceReachesTheReferencePage(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	src, err := LoadBlockSources(ctx, pool)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM book1_evidence_source`).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
	}
	if got := len(src.Evidence[evidenceBlock]); got != total {
		t.Errorf("reference page carries %d of %d evidence sources", got, total)
	}

	// Every one must carry its stated limitation. A citation printed without it reads as an
	// endorsement of the page citing it.
	for _, e := range src.Evidence[evidenceBlock] {
		if e.Limitation == "" {
			t.Errorf("%s (%s) has no important_limitation and would print as unqualified",
				e.SourceID, e.Authority)
		}
	}
}

// TestEveryDailyModuleCarriesItsAILimit: all thirteen rows ship one, and the template prints
// it. If the provider ever ships a row without one, the page must not silently drop the
// constraint -- this fails first so someone decides what to print instead.
func TestEveryDailyModuleCarriesItsAILimit(t *testing.T) {
	pool := testPool(t)
	src, err := LoadBlockSources(context.Background(), pool)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	seen := map[string]bool{}
	for _, mods := range src.Daily {
		for _, m := range mods {
			if seen[m.DailyLifeID] {
				continue
			}
			seen[m.DailyLifeID] = true
			if m.AILimit == "" {
				t.Errorf("%s (%s) carries no ai_limit", m.DailyLifeID, m.Domain)
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("no daily-life modules loaded at all")
	}
}

// ageStageCeiling is the last month age_feeding_stage_master covers: AF09 runs 192-216, so
// eighteen years exactly. book1_content_block declares several blocks as 0-228, twelve months
// further, and those twelve months have no feeding stage behind them -- recorded as GAP-027
// and reported per book rather than papered over. Asserted below in both directions so that a
// provider who extends AF09, or adds an AF10, breaks this test and gets the ceiling reviewed
// rather than silently changing what a book contains.
const ageStageCeiling = 216

// TestLoadAgeStageCoversEveryAge: every month up to the ceiling must resolve to exactly one
// feeding stage, or a child of that age gets a feeding page with nothing on it -- which would
// show up as a silently shorter book and nothing else.
func TestLoadAgeStageCoversEveryAge(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var missing []int
	for m := 0; m <= ageStageCeiling; m++ {
		cur, _, _, err := LoadAgeStages(ctx, pool, m)
		if err != nil {
			t.Fatalf("age %d months: %v", m, err)
		}
		if cur == nil {
			missing = append(missing, m)
		}
	}
	if len(missing) != 0 {
		t.Errorf("%d age(s) in months resolve to no feeding stage, starting at %d: %v",
			len(missing), missing[0], missing)
	}
}

// TestAgeStagesStopAtEighteen pins the other side of the ceiling. A book for a child past it
// must lose its feeding pages and say so, not print an empty stage table -- and if the
// provider ever extends the master, this fails and the GAP-027 text gets revisited with it.
func TestAgeStagesStopAtEighteen(t *testing.T) {
	pool := testPool(t)
	cur, _, _, err := LoadAgeStages(context.Background(), pool, ageStageCeiling+1)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cur != nil {
		t.Errorf("age %d months now resolves to stage %s. age_feeding_stage_master has been "+
			"extended past eighteen years -- update ageStageCeiling and GAP-027 together.",
			ageStageCeiling+1, cur.StageCode)
	}
}

// TestAgeStageNeighboursBracketTheChild holds B1-008's comparison table to being a real
// comparison: the previous stage must end before the current one starts and the next must
// begin after it ends. A neighbour that overlaps would print "what changes next" describing
// the child's own current stage.
func TestAgeStageNeighboursBracketTheChild(t *testing.T) {
	pool := testPool(t)
	// 51 months: a preschooler, with stages on both sides.
	cur, prev, next, err := LoadAgeStages(context.Background(), pool, 51)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cur == nil {
		t.Fatal("51 months resolves to no stage")
	}
	if prev == nil || next == nil {
		t.Fatalf("want a stage either side of %s, got prev=%v next=%v", cur.StageCode, prev, next)
	}
	if prev.AgeTo >= cur.AgeFrom {
		t.Errorf("previous stage %s ends at %d, not before %s starts at %d",
			prev.StageCode, prev.AgeTo, cur.StageCode, cur.AgeFrom)
	}
	if next.AgeFrom <= cur.AgeTo {
		t.Errorf("next stage %s starts at %d, not after %s ends at %d",
			next.StageCode, next.AgeFrom, cur.StageCode, cur.AgeTo)
	}
}
