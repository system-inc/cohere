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

	name := tspath.NormalizePath("/repository/source/Thing.ts")
	sourceFile := parser.ParseSourceFile(
		ast.SourceFileParseOptions{FileName: tspath.RootedFilePath(name), PathKey: tspath.PathKey(name)},
		sourceText,
		core.ScriptKindTS,
	)
	if sourceFile == nil {
		test.Fatalf("could not parse the fixture")
	}
	return sourceFile
}
