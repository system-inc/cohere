package native

import (
	"github.com/system-inc/cohere/internal/format/css"
	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/prettier"
	"github.com/system-inc/cohere/internal/format/printing"
)

// The CSS printer: Prettier's language-css printer over postcss and its three sub-parsers, ported in
// internal/format/css. Only the css parser: .scss and .less are not registered, and css templates in
// TypeScript stay as written, because upstream formats those with the scss parser.
func init() {
	Register(".css", func(_ string, text string, options prettier.Options) (string, error) {
		return css.Format(text, options)
	})
	RegisterDoc(".css", func(_ string, text string, options prettier.Options, _ string, _ string, _ printing.TextToDoc) (doc.Doc, error) {
		return css.PrintToDoc(text, options)
	})
}
