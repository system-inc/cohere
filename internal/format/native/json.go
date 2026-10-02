package native

import (
	"github.com/system-inc/cohere/internal/format/javascript"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// The JSON printers: Prettier's json and json-stringify parsers, chosen by file name inside
// javascript.FormatJSON, the way language-json's languages choose them.
func init() {
	Register(".json", func(fileName string, text string, options prettier.Options) (string, error) {
		return javascript.FormatJSON(fileName, text, options)
	})
}
