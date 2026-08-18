// Package book renders the provider's two books from a child's data.
//
// The provider's JSON schemas are the contract between the engine and the renderer
// (data/book-engine-spec/MadamGY_Book{1,2}_JSON_Schema_V1.json). An assembler builds that
// JSON from the database; html/template renders it; chromedp prints the result. The same
// HTML is the reviewer preview and the print source, so what a reviewer approves and what
// prints cannot drift apart.
//
// # This package never writes a sentence
//
// Every string a parent reads is either provider-authored text loaded from the database or
// one of the child's own recorded values. There are no generated summaries, no rephrased
// clinical guidance and no filled-in defaults. Where data is absent the template emits a
// blank writing line or omits the block, following the provider's own prototypes, which use
// "________" and slot tokens like "[from master]" for exactly this.
//
// That is the 18 August ruling in docs/decisions.md - no generated guidance prose reaches a
// parent through a path with no human review gate - applied to the one surface that reaches
// a parent directly. book1_content_block.ai_can_draft = 'N' enforces a narrower version of
// the same rule on five specific blocks; this package's rule is the broad one and covers
// every block.
//
// # Chromium
//
// PDF rendering needs headless Chromium. That dependency buys correct Indic text shaping,
// which matters because all 406 ingredients carry Bengali and Hindi names and Bengali needs
// conjunct formation and matra repositioning that Go's PDF libraries do not implement. A
// renderer that silently mis-forms a Bengali ingredient name would be worse than one that
// refuses to run.
package book
