package native

import (
	"github.com/system-inc/cohere/internal/format/markdown"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// The markdown printer: Prettier's language-markdown printer, ported in internal/format/markdown, over
// the tree its own micromark port builds.
func init() {
	// A fenced code block or front matter is formatted by the native printer for its language, through
	// TextToDoc. markdown's embed passes upstream's parser name, or for ts, typescript and tsx the file
	// name upstream overrides the filepath with, because the trailing comma of type parameters depends
	// on it.
	Register(".md", func(_ string, text string, options prettier.Options) (string, error) {
		return markdown.Format(text, options, TextToDoc(options, "markdown"))
	})
}
