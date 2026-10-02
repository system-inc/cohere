package native

import (
	"github.com/system-inc/cohere/internal/format/javascript"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// The JavaScript printer: the TypeScript printer's, with Prettier's babel parser standing in as
// javascript.FormatJavaScript describes.
func init() {
	print := func(fileName string, text string, options prettier.Options) (string, error) {
		return javascript.FormatJavaScript(fileName, text, options)
	}
	for _, extension := range []string{".js", ".mjs", ".cjs", ".jsx"} {
		Register(extension, print)
	}
}
