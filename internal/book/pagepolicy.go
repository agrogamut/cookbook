package book

// Which Book 1 blocks must begin a sheet regardless of the part they sit in.
//
// Layout policy, deliberately code and not a migration. The provider's parts are data and live
// in book1_content_block; "this form needs a whole sheet to be usable" is a decision this
// project made about printing, and putting it in a table would dress it as the provider's.
//
// Book 1 used to break before every block, all thirty-one of them, which is why a block holding
// three writing lines got a sheet of its own and eighteen of forty-seven pages ended above 62%
// of the text block. It now breaks once per part -- the workbook's own grouping, fifteen of
// them, and the pairs mean something: A is the profile and the consultation summary, B is
// growth and its trend page, E is the vaccination pair. Everything else runs on.
//
// These four are the exceptions. Each is a full-page form a parent fills in over weeks, where a
// break landing mid-form costs the reader the column headings on one of the two halves and
// leaves a grid they cannot label. Starting them on a fresh sheet is what makes them usable,
// and it is worth the white paper it sometimes costs.
var mustStartASheet = map[string]string{
	"B1-013": "age-specific monitoring: one grid per monitoring parameter, and the set fills a sheet",
	"B1-019": "food acceptance tracker: two grids the parent fills daily",
	"B1-020": "weekly review dashboard: eighteen monitoring areas in one table",
	"B1-022": "reference and disclaimer: the evidence table runs the full sheet",
}

// MustStartASheet reports whether a block begins a page whatever part it belongs to.
func MustStartASheet(blockID string) bool {
	_, ok := mustStartASheet[blockID]
	return ok
}
