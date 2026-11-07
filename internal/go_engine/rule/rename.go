package rule

import (
	"flag"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// RenameExportsOption is the option a renaming rule carries, `renameExports`, read by the engine's rename fix and
// never by the rule's run. Off, which is its default, the fix declines to rename an exported name of a package
// another module could import, since a consumer outside the workspace can't be renamed with it. On, the settings
// say the workspace is the whole world. The rule's findings are the same either way.
const RenameExportsOption = "renameExports"

// AddRenameExportsOption gives a renaming rule's analyzer its renameExports flag, so every rule that offers a
// rename spells and documents the option the same way.
func AddRenameExportsOption(analyzer *analysis.Analyzer) {
	analyzer.Flags.Bool(RenameExportsOption, false, "rename an exported name of a package another module could "+
		"import as well; off by default, so the rename fix declines it, since a consumer outside the workspace "+
		"can't be renamed with it. The rule's findings are the same either way")
}

// RenamesExports is whether a rule's settings let the rename fix rename an exported name of an importable package.
// An analyzer with no renameExports flag offers no rename to widen, so it reads as off.
func RenamesExports(analyzer *analysis.Analyzer) bool {
	option := analyzer.Flags.Lookup(RenameExportsOption)
	if option == nil {
		return false
	}
	getter, isGetter := option.Value.(flag.Getter)
	if !isGetter {
		return false
	}
	renames, isBool := getter.Get().(bool)
	return isBool && renames
}

// RenameFixes is the fix a renaming rule attaches to a finding whose judgment names exactly one new spelling for the
// whole identifier: one edit over the declaring identifier whose new text is the whole new name. The engine reads
// that text as the name and renames every reference itself, through the types, so the fix carries no edit of its
// own at a use. A new name Go reserves, a keyword or a predeclared identifier, or one that is the old name gets no
// fix, since a rename to it would not compile or would change nothing.
func RenameFixes(identifier *ast.Ident, newName string, message string) []analysis.SuggestedFix {
	if newName == identifier.Name || token.IsKeyword(newName) || types.Universe.Lookup(newName) != nil {
		return nil
	}
	return []analysis.SuggestedFix{{
		Message: message,
		TextEdits: []analysis.TextEdit{{
			Pos:     identifier.Pos(),
			End:     identifier.End(),
			NewText: []byte(newName),
		}},
	}}
}
