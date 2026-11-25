package rule

import (
	"go/ast"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// A rename is one edit over the declaring identifier, its new text the whole new name, and a new name Go reserves or
// one that changes nothing gets no fix.
func TestRenameFixesOffersOneEditOrNone(test *testing.T) {
	test.Parallel()
	identifier := &ast.Ident{NamePos: token.Pos(10), Name: "cfg"}
	fixes := RenameFixes(identifier, "configuration", "Rename it.")
	if len(fixes) != 1 || len(fixes[0].TextEdits) != 1 {
		test.Fatalf("fixes %+v, want one fix with one edit", fixes)
	}
	edit := fixes[0].TextEdits[0]
	if edit.Pos != identifier.Pos() || edit.End != identifier.End() || string(edit.NewText) != "configuration" ||
		fixes[0].Message != "Rename it." {
		test.Errorf("fix %+v, want the identifier's span rewritten to configuration", fixes[0])
	}
	for _, newName := range []string{"cfg", "error", "string", "nil", "func", "type"} {
		if declined := RenameFixes(identifier, newName, "Rename it."); declined != nil {
			test.Errorf("a rename to %q is offered: %+v", newName, declined)
		}
	}
}

// renameExports reads off when an analyzer lacks the flag or leaves it at its default, and on once it is set.
func TestRenamesExportsReadsTheFlag(test *testing.T) {
	test.Parallel()
	bare := &analysis.Analyzer{Name: "bare", Doc: "bare has no options", Run: plantedAnalyzer.Run}
	if RenamesExports(bare) {
		test.Error("an analyzer with no renameExports flag reads as renaming exports")
	}
	optioned := &analysis.Analyzer{Name: "optioned", Doc: "optioned renames", Run: plantedAnalyzer.Run}
	AddRenameExportsOption(optioned)
	if RenamesExports(optioned) {
		test.Error("renameExports reads as on at its default")
	}
	if setError := optioned.Flags.Set(RenameExportsOption, "true"); setError != nil {
		test.Fatal(setError)
	}
	if !RenamesExports(optioned) {
		test.Error("renameExports set to true reads as off")
	}
}
