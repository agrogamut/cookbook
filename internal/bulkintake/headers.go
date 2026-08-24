package bulkintake

// RequiredHeaders is the exact question text this system depends on, copied verbatim from a
// real export of the live form (session of 2026-08-24). Google Forms exports the question
// text as the header row, and that text changes whenever the form is edited -- so a file
// whose header row doesn't contain every one of these, verbatim, is rejected whole rather
// than silently misaligning columns. See docs/superpowers/specs/2026-08-24-bulk-book-generation-design.md.
var RequiredHeaders = []string{
	"Child Full Name",
	"Date of Birth",
	"Sex used for pediatric growth reference",
	"State / Union Territory",
	"Country",
	"Household food practice",
	"Religious/cultural food restrictions",
	"Date of current measurement",
	"Current weight (kg)",
	"Current standing height / recumbent length",
	"Head circumference",
	"Known food allergy/intolerance?",
	"Known / suspected food allergens",
	"History of anaphylaxis?",
	"Does the child have any special-care diagnosis or feeding condition requiring additional review?",
	"If yes, which condition(s)?",
	// Free-text columns read only for the special-care mismatch check (Task 6) -- not mapped
	// to any profile field on their own.
	"Relevant family history (Select whichever is appropriate, ignore if none)",
	"Any developmental concern?",
	"Areas of concern",
}
