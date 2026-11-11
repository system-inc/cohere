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
//
// `.mts` and `.cts` are TypeScript too, as Prettier's language table has them, and parse as `.ts` does,
// without JSX, so a `<T>x` cast reads as one: typescript-estree takes JSX from a known extension and gives
// these two none (estree.scriptKindOf agrees). Upstream's `/\.ts$/` misses both, so an arrow's lone type
// parameter keeps its `<T,>` there as in `.tsx`, and shouldForceTrailingComma's suffix test does the same.
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
	for _, extension := range []string{".ts", ".tsx", ".mts", ".cts", source_name.AdamicExtension} {
		Register(extension, print)
		RegisterParsed(extension, printParsed)
		RegisterDoc(extension, javascript.PrintToDoc)
	}
}
