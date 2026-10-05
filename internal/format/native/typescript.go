package native

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/format/formatoptions"
	"github.com/system-inc/cohere/internal/format/javascript"
)

// The TypeScript and TSX printer: Prettier's language-js printer, ported in internal/format/javascript,
// over the ESTree tree internal/format/estree converts from typescript-go's AST.
func init() {
	print := func(fileName string, text string, options formatoptions.Options) (string, error) {
		return javascript.Format(fileName, text, options, TextToDoc(options, "typescript"))
	}
	printParsed := func(fileName string, text string, parsed *ast.SourceFile, options formatoptions.Options) (string, error) {
		return javascript.FormatParsed(fileName, text, parsed, options, TextToDoc(options, "typescript"))
	}
	Register(".ts", print)
	Register(".tsx", print)
	RegisterParsed(".ts", printParsed)
	RegisterParsed(".tsx", printParsed)
	RegisterDoc(".ts", javascript.PrintToDoc)
	RegisterDoc(".tsx", javascript.PrintToDoc)
}
