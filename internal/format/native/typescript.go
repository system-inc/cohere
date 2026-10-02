package native

import (
	"github.com/system-inc/cohere/internal/format/javascript"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// The TypeScript and TSX printer: Prettier's language-js printer, ported in internal/format/javascript,
// over the ESTree tree internal/format/estree converts from typescript-go's AST.
func init() {
	print := func(fileName string, text string, options prettier.Options) (string, error) {
		return javascript.Format(fileName, text, options, TextToDoc(options, "typescript"))
	}
	Register(".ts", print)
	Register(".tsx", print)
	RegisterDoc(".ts", javascript.PrintToDoc)
	RegisterDoc(".tsx", javascript.PrintToDoc)
}
