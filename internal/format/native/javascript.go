package native

import (
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/javascript"
)

// The JavaScript printer: the TypeScript printer's, with Prettier's babel parser standing in as
// javascript.FormatJavaScript describes.
func init() {
	print := func(fileName string, text string, options format_options.Options) (string, error) {
		return javascript.FormatJavaScript(fileName, text, options, TextToDoc(options, "babel", nil))
	}
	for _, extension := range []string{".js", ".mjs", ".cjs", ".jsx"} {
		Register(extension, print)
		RegisterDoc(extension, javascript.PrintToDoc)
	}
}
