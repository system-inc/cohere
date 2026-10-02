package native

import (
	"strings"

	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/markdown"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// The markdown printer: Prettier's language-markdown printer, ported in internal/format/markdown, over
// the tree its own micromark port builds.
func init() {
	Register(".md", func(_ string, text string, options prettier.Options) (string, error) {
		return markdown.Format(text, options, embeddedTextToDoc(options))
	})
}

// embeddedFileNames maps the parsers markdown's embed infers to a file name the registered printers
// route by. A parser with no native printer yet is refused, and the core then leaves the block as
// written, which is what upstream does when an embedded format fails.
var embeddedFileNames = map[string]string{
	"typescript": "embedded.tsx",
	"babel":      "embedded.js",
	"json":       "embedded.json",
	"json5":      "embedded.json5",
	"jsonc":      "embedded.jsonc",
	"yaml":       "embedded.yaml",
	"graphql":    "embedded.graphql",
	"css":        "embedded.css",
	"scss":       "embedded.scss",
	"less":       "embedded.less",
	"markdown":   "embedded.md",
}

// embeddedTextToDoc formats a fenced code block or front matter with the native printer for its
// language. markdown's embed passes upstream's parser name, or for ts, typescript and tsx the file
// name upstream overrides the filepath with, because the trailing comma of type parameters depends on
// it.
//
// Upstream's textToDoc returns the embedded printer's doc, which the outer printer lays out at the
// block's indentation. Native printers return text, so a block nested in a list item or quote is laid
// out at the full width rather than the width left inside the item. The difference shows only on a line
// within the indentation of the limit.
func embeddedTextToDoc(options prettier.Options) func(text string, parser string) (doc.Doc, error) {
	formatter := Formatter{Options: options}
	return func(text string, parser string) (doc.Doc, error) {
		fileName := parser
		if !strings.Contains(parser, ".") {
			fileName = embeddedFileNames[parser]
		}
		formatted, err := formatter.Format(fileName, text)
		if err != nil {
			return nil, err
		}
		// textToDoc strips the trailing hardline the embedded printer ends with.
		return doc.Text(strings.TrimSuffix(formatted, "\n")), nil
	}
}
