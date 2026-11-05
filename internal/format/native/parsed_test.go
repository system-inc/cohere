package native

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/format/formatoptions"
)

// parsedCorpusVariable names more roots for the proof below, separated by the path list separator: a real tree,
// such as ahra, read the way the fixtures are. Unset, the proof reads the repository's fixtures alone.
const parsedCorpusVariable = "COHERE_PARSE_FROM_CORPUS"

// The formatter prints a TypeScript file from the fix engine's guard's tree rather than parsing it again
// (#dk2502g). estree's TestTheGuardsTreeIsTheTreeTheFormatterWouldParse proves the two trees convert to one
// ESTree; this proves what a run relies on, the printed bytes, so anything the printer reads off the tree
// besides the ESTree (node flags, the script kind it was parsed with) is covered too. Asked for by
// @system_cohere_lint_fix in review. Every TypeScript file of the fixtures and of any tree the variable names is
// formatted twice, with the tree the engine hands its transform and with none, and the outputs, or the errors,
// must be equal. A tree of other bytes is offered too, and must be refused, printing what no tree prints.
func TestFormattingFromTheGuardsTreePrintsWhatParsingAgainPrints(test *testing.T) {
	test.Parallel()
	roots := []string{moduleRootOf(test)}
	if extra := os.Getenv(parsedCorpusVariable); extra != "" {
		roots = append(roots, filepath.SplitList(extra)...)
	}
	formatter := Formatter{Options: formatoptions.Default()}
	compared := 0
	for _, fileName := range typeScriptFilesUnder(roots) {
		contents, readError := os.ReadFile(fileName)
		if readError != nil {
			test.Fatal(readError)
		}
		text := string(contents)

		var guards *ast.SourceFile
		capture := func(_ string, given string, parsed *ast.SourceFile) (string, error) {
			guards = parsed
			return given, nil
		}
		noProposals := func(string, string) ([]edit.Proposal, error) { return nil, nil }
		if _, fixAndTransformTextError := edit.FixAndTransformText(
			fileName,
			text,
			noProposals,
			capture,
			1,
		); fixAndTransformTextError != nil || guards == nil {
			// A file the guard refuses never reaches the formatter.
			continue
		}

		reparsed, reparseError := formatter.Format(fileName, text)
		fromTree, treeError := formatter.FormatParsed(fileName, text, guards)
		if fromTree != reparsed || errorText(treeError) != errorText(reparseError) {
			test.Errorf("%s: formatting from the guard's tree printed another result than parsing again", fileName)
		}
		mutant := estree.ParseSourceFile(fileName, " "+text)
		if fromMutant, mutantError := formatter.FormatParsed(fileName, text, mutant); fromMutant != reparsed ||
			errorText(mutantError) != errorText(reparseError) {
			test.Errorf("%s: a tree of other bytes was not refused", fileName)
		}
		compared++
	}
	test.Logf("%d TypeScript files formatted from the guard's tree and from a parse of their own", compared)
	if compared < 50 {
		test.Fatalf("only %d files were compared, too few for agreement to mean anything", compared)
	}
}

func errorText(sourceError error) string {
	if sourceError == nil {
		return ""
	}
	return sourceError.Error()
}

// moduleRootOf is the module's root, where the fixtures are.
func moduleRootOf(test *testing.T) string {
	test.Helper()
	directory, workingDirectoryError := os.Getwd()
	if workingDirectoryError != nil {
		test.Fatal(workingDirectoryError)
	}
	for {
		if _, statError := os.Stat(filepath.Join(directory, "go.mod")); statError == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			test.Fatal("no go.mod above the test's directory")
		}
		directory = parent
	}
}

// typeScriptFilesUnder is every .ts and .tsx file under roots, the extensions with a printer that takes a tree,
// outside dependency, build and cache directories and the TypeScript checkout.
func typeScriptFilesUnder(roots []string) []string {
	skipped := map[string]bool{
		"node_modules": true,
		".git":         true,
		".cache":       true,
		".next":        true,
		"dist":         true,
		"TypeScript":   true,
	}
	var files []string
	for _, root := range roots {
		filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
			if pathError != nil {
				return nil
			}
			if entry.IsDir() {
				if path != root && skipped[entry.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			extension := strings.ToLower(filepath.Ext(path))
			if (extension == ".ts" || extension == ".tsx") && entry.Type()&fs.ModeSymlink == 0 {
				files = append(files, path)
			}
			return nil
		})
	}
	return files
}
