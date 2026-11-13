package comments

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/corpus"
)

type realSourceFile struct {
	name       string
	sourceFile *ast.SourceFile
}

// realSourceFiles parses actual tree files, so the differential test runs on the comment shapes that
// exist rather than on the ones somebody thought to write.
func realSourceFiles(test *testing.T, limit int) []realSourceFile {
	test.Helper()

	var files []realSourceFile
	root := corpus.Structure.At(test, corpus.StructurePin, "source")
	walkError := filepath.Walk(root, func(path string, info os.FileInfo, pathError error) error {
		if pathError != nil || info.IsDir() || len(files) >= limit {
			return nil
		}
		isTypeScript := strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx")
		if !isTypeScript {
			return nil
		}
		data, readError := os.ReadFile(path)
		if readError != nil {
			return nil
		}
		scriptKind := core.ScriptKindTS
		if strings.HasSuffix(path, ".tsx") {
			scriptKind = core.ScriptKindTSX
		}
		name := tspath.NormalizePath(path)
		sourceFile := parser.ParseSourceFile(
			ast.SourceFileParseOptions{FileName: tspath.RootedFilePath(name), PathKey: tspath.PathKey(name)},
			string(data),
			scriptKind,
		)
		if sourceFile != nil {
			files = append(files, realSourceFile{name: path, sourceFile: sourceFile})
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}

	// A corpus that failed to load would make every assertion pass over nothing.
	if len(files) < 100 {
		test.Fatalf("the structure corpus gave only %d source files, too few to be a corpus", len(files))
	}
	return files
}

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
