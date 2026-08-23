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
)

type realSourceFile struct {
	name       string
	sourceFile *ast.SourceFile
}

// realSourceFiles parses actual tree files, so the differential test runs on the comment shapes that
// exist rather than on the ones somebody thought to write.
func realSourceFiles(t *testing.T, limit int) []realSourceFile {
	t.Helper()

	var files []realSourceFile
	root := "/Users/kirkouimet/Projects/ahra/libraries/structure/source"
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || len(files) >= limit {
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
			ast.SourceFileParseOptions{FileName: name, Path: tspath.Path(name)}, string(data), scriptKind)
		if sourceFile != nil {
			files = append(files, realSourceFile{name: path, sourceFile: sourceFile})
		}
		return nil
	})

	// A corpus that failed to load would make every assertion pass over nothing.
	if len(files) < 100 {
		t.Skipf("only %d source files available, too few to be a corpus", len(files))
	}
	return files
}

// parseSourceForTest parses one hand-written case.
func parseSourceForTest(t *testing.T, sourceText string) *ast.SourceFile {
	t.Helper()

	name := tspath.NormalizePath("/repository/source/Thing.ts")
	sourceFile := parser.ParseSourceFile(
		ast.SourceFileParseOptions{FileName: name, Path: tspath.Path(name)}, sourceText, core.ScriptKindTS)
	if sourceFile == nil {
		t.Fatalf("could not parse the fixture")
	}
	return sourceFile
}

// realCommentCorpus collects comment lines from the tree, so a gate over comments is measured
// against the comments that exist rather than the ones somebody thought to write.
func realCommentCorpus(t *testing.T) []string {
	t.Helper()

	var comments []string
	root := "/Users/kirkouimet/Projects/ahra/libraries/structure/source"
	fileCount := 0
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || fileCount >= 400 {
			return nil
		}
		if !strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".tsx") {
			return nil
		}
		fileCount++
		data, readError := os.ReadFile(path)
		if readError != nil {
			return nil
		}
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			isCommentish := strings.HasPrefix(trimmed, "//") ||
				strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "/*")
			if isCommentish {
				comments = append(comments, trimmed)
			}
		}
		return nil
	})

	if len(comments) < 1000 {
		t.Skipf("only %d comments available, too few to be a corpus", len(comments))
	}
	return comments
}
