package native

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/javascript"
	"github.com/system-inc/cohere/internal/types/source_name"
)

// The TypeScript and TSX printer: Prettier's language-js printer, ported in internal/format/javascript,
// over the ESTree tree internal/format/estree converts from typescript-go's AST.
//
// An Adamic `.a` file is TypeScript under its own name, and prints exactly as the same text named `.ts`
// (#6mhafvb): estree parses it as TS, and the printer's one name-dependent choice, `<T,>`'s comma, asks
// source_name.TreatedAs. Which `.a` files reach the printer is the program's to say, not this table's: the
// format walk holds them back until the program claims them (format_files.Enumeration.Adamic).
func init() {
	print := func(fileName string, text string, options format_options.Options) (string, error) {
		return javascript.Format(fileName, text, options, TextToDoc(options, "typescript", nil))
	}
	printParsed := func(
		fileName string,
		text string,
		parsed *ast.SourceFile,
		options format_options.Options,
	) (string, error) {
		return javascript.FormatParsed(fileName, text, parsed, options, TextToDoc(options, "typescript", nil))
	}
	Register(".ts", print)
	Register(".tsx", print)
	Register(source_name.AdamicExtension, print)
	RegisterParsed(".ts", printParsed)
	RegisterParsed(".tsx", printParsed)
	RegisterParsed(source_name.AdamicExtension, printParsed)
	RegisterDoc(".ts", javascript.PrintToDoc)
	RegisterDoc(".tsx", javascript.PrintToDoc)
	RegisterDoc(source_name.AdamicExtension, javascript.PrintToDoc)
}
