package native

import (
	"github.com/system-inc/cohere/internal/format/arena"
	"github.com/system-inc/cohere/internal/format/css"
	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/printing"
)

// The CSS printer: Prettier's language-css printer over postcss and its three sub-parsers, ported in
// internal/format/css. The scss parser is registered for embedding only: upstream formats css and
// styled templates in TypeScript as scss (language-js/embed/css.js). .scss and .less files are not
// formatted.
func init() {
	RegisterDoc(".scss", func(
		_ string,
		text string,
		options format_options.Options,
		_ string,
		_ string,
		textToDoc printing.TextToDoc,
	) (doc.Doc, error) {
		return css.PrintToDocScss(text, options, textToDoc)
	})
	// yaml front matter is formatted by the native yaml printer, through TextToDoc, as markdown's is.
	Register(".css", func(_ string, text string, options format_options.Options) (string, error) {
		docs, isSlab := embedDocs.Get().(*arena.Slab[doc.Doc])
		if !isSlab {
			// Invariant: embedDocs builds only doc slabs, and the one Put below returns the slab this Get took.
			panic("native: the css embed pool held something other than a doc slab")
		}
		defer func() {
			docs.Reset()
			embedDocs.Put(docs)
		}()
		return css.Format(text, options, TextToDoc(options, "css", docs))
	})
	RegisterDoc(".css", func(
		_ string,
		text string,
		options format_options.Options,
		_ string,
		_ string,
		textToDoc printing.TextToDoc,
	) (doc.Doc, error) {
		return css.PrintToDoc(text, options, textToDoc)
	})
}
