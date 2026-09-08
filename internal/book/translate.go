package book

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"golang.org/x/net/html"

	"github.com/madamgy/recipie/internal/aidraft"
)

// translateBatchSize bounds how many text nodes ride in one Gemini call. A 40-page book
// carries far more short table cells and labels than one structured-JSON response should be
// asked to return intact in a single call -- the risk being a truncated array the caller has
// no way to tell apart from a dropped fragment, since translateSchema alone cannot enforce
// "exactly this many strings." Lowered from 60 after a live run: batches that size ran close
// to geminiClient's own perCallTimeout ceiling, and one of several dozen hit a genuine 504
// DEADLINE_EXCEEDED from Gemini's own backend. Smaller batches finish with real margin instead
// of hugging the ceiling; TranslateTexts' own retry (aidraft/draft.go) covers what margin
// alone does not.
const translateBatchSize = 25

// targetLanguageName maps this project's short language codes (the same ones bookLanguage
// recognises) to the full name Gemini is asked to translate into -- a prompt reads better
// asking for "Bengali" than for "bn".
var targetLanguageName = map[string]string{
	"bn": "Bengali",
}

// bookLanguage turns a profile's free-text LanguageID into the short code this package's
// rendering and translation pipeline branches on. Only "bn"/"bengali"/"bangla" (any casing)
// selects Bengali; everything else, including an unset field, stays "en" -- the same default
// ChildSummary.Language has always used, now also deciding what language the page itself
// prints in rather than only what the child's profile line names.
func bookLanguage(languageID string) string {
	switch strings.ToLower(strings.TrimSpace(languageID)) {
	case "bn", "bengali", "bangla":
		return "bn"
	default:
		return "en"
	}
}

// skipTranslation names the elements whose text content is never reader-facing prose and must
// never be sent to a translation model.
func skipTranslation(tag string) bool {
	switch tag {
	case "script", "style":
		return true
	}
	return false
}

// textNode is one translatable fragment plus the parsed-tree leaf it came from, so a
// translated string can be written back to the exact node it was read from rather than
// re-matched by content (which would break on any repeated label).
type textNode struct {
	node     *html.Node
	leading  string // whitespace the node opened with
	trailing string // whitespace the node closed with
	core     string // the trimmed content actually sent for translation
}

// collectTextNodes walks n's subtree in document order, gathering every non-blank text node
// outside a skipTranslation element. Order matters: it is what lets buildTranslatePrompt's
// numbering and this function's write-back line up positionally.
func collectTextNodes(n *html.Node, out *[]textNode) {
	if n.Type == html.ElementNode && skipTranslation(n.Data) {
		return
	}
	if n.Type == html.TextNode {
		if trimmed := strings.TrimSpace(n.Data); trimmed != "" {
			start := strings.Index(n.Data, trimmed)
			*out = append(*out, textNode{
				node:     n,
				leading:  n.Data[:start],
				trailing: n.Data[start+len(trimmed):],
				core:     trimmed,
			})
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectTextNodes(c, out)
	}
}

// TranslateHTML carries every visible text node in an already-fully-rendered book page into
// lang, in place. Nothing about the document's structure, CSS, ids or numbers changes -- this
// runs after RenderHTML has already substituted every real value (a recipe name, a quantity, a
// recipe id) into the page, so what reaches Gemini here is the same finished page a reader
// would otherwise see in English, restated in another language rather than recomposed from
// scratch. See aidraft.TranslateRequest's doc comment for why that makes this a restatement of
// an already-sourced value, not a new drafted claim the hard rule would otherwise forbid.
//
// Batched and bounded the same way clinical-note drafting already is (draftConcurrently,
// maxDraftConcurrency in clinical_notes.go).
func TranslateHTML(ctx context.Context, doc []byte, drafter aidraft.Drafter, lang string) ([]byte, error) {
	target, ok := targetLanguageName[lang]
	if !ok {
		return nil, fmt.Errorf("book: %q is not a language this project can translate into", lang)
	}

	root, err := html.Parse(bytes.NewReader(doc))
	if err != nil {
		return nil, fmt.Errorf("book: parse rendered page for translation: %w", err)
	}

	var nodes []textNode
	collectTextNodes(root, &nodes)
	if len(nodes) == 0 {
		return doc, nil
	}

	var batches [][]textNode
	for i := 0; i < len(nodes); i += translateBatchSize {
		end := i + translateBatchSize
		if end > len(nodes) {
			end = len(nodes)
		}
		batches = append(batches, nodes[i:end])
	}

	errs := make([]error, len(batches))
	draftConcurrently(ctx, len(batches), func(ctx context.Context, i int) {
		batch := batches[i]
		texts := make([]string, len(batch))
		for j, tn := range batch {
			texts[j] = tn.core
		}
		out, err := drafter.TranslateTexts(ctx, aidraft.TranslateRequest{
			TargetLanguage: target, Texts: texts,
		})
		if err != nil {
			errs[i] = fmt.Errorf("translate batch %d: %w", i, err)
			return
		}
		if len(out.Texts) != len(batch) {
			errs[i] = fmt.Errorf("translate batch %d: got %d translations for %d fragments",
				i, len(out.Texts), len(batch))
			return
		}
		for j, tn := range batch {
			tn.node.Data = tn.leading + out.Texts[j] + tn.trailing
		}
	})
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}

	var buf bytes.Buffer
	if err := html.Render(&buf, root); err != nil {
		return nil, fmt.Errorf("book: serialize translated page: %w", err)
	}
	return buf.Bytes(), nil
}
