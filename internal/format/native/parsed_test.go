package native

import (
	"os"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/testing/typescript_fixtures"
)

// The formatter prints a TypeScript file from the fix pass's guard's tree rather than parsing it again
// (#dk2502g). estree's TestTheGuardsTreeIsTheTreeTheFormatterWouldParse proves the two trees convert to one
// ESTree; this proves what a run relies on, the printed bytes, so anything the printer reads off the tree
// besides the ESTree (node flags, the script kind it was parsed with) is covered too. Asked for by
// @system_cohere_lint_fix in review. Every .ts and .tsx file of this repository's fixtures, the extensions with a
// printer that takes a tree, read by the directories that hold them (typescript_fixtures), is formatted twice, with
// the tree the formatter hands its transform and with none, and the outputs, or the errors, must be equal. A tree
// of other bytes is offered too, and must be refused, printing what no tree prints.
func TestFormattingFromTheGuardsTreePrintsWhatParsingAgainPrints(test *testing.T) {
	test.Parallel()
	formatter := Formatter{Options: format_options.Default()}
	compared := 0
	for _, fileName := range typescript_fixtures.Files(test, ".ts", ".tsx") {
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
