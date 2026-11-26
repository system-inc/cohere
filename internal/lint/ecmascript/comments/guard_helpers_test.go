package comments

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// parseSourceForTest parses one hand-written case.
func parseSourceForTest(test *testing.T, sourceText string) *ast.SourceFile {
	test.Helper()
	return parseSourceForTestAs(test, sourceText, core.ScriptKindTS)
}

// parseSourceForTestAs parses one hand-written case as the given kind, so a case holding JSX is parsed as a `.tsx`
// file would be, where `{/* c */}` is an expression container rather than an object literal.
func parseSourceForTestAs(test *testing.T, sourceText string, scriptKind core.ScriptKind) *ast.SourceFile {
	test.Helper()

	name := tspath.NormalizePath("/repository/source/Thing.ts")
	if scriptKind == core.ScriptKindTSX {
		name = tspath.NormalizePath("/repository/source/Thing.tsx")
	}
	sourceFile := parser.ParseSourceFile(
		ast.SourceFileParseOptions{FileName: tspath.RootedFilePath(name), PathKey: tspath.PathKey(name)},
		sourceText,
		scriptKind,
	)
	if sourceFile == nil {
		test.Fatalf("could not parse the fixture")
	}
	return sourceFile
}
