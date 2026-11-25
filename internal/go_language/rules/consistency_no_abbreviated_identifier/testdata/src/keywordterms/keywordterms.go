// Package keywordterms declares names holding JavaScript's var keyword as policy/KeywordTerms.json spells it, which
// read clean, beside a bare `Var` meaning a variable, a spelling run on into a longer word, and a keyword term next
// to another abbreviation or a bare `Var`, each still reported. NoVar is the control: the rule runs here and judges `Var`.
package keywordterms

type NoVar struct{} // want `Identifier "NoVar" abbreviates "Var"\. Use "Variable" for a variable, or a name saying it means JavaScript's var keyword\.`

type VarKeywordFix struct{}

func isVarDeclaration(kind string) bool { return kind == "var" }

func collectVarScopedDeclarations(names []string) []string { return names }

func theme() int {
	themeVar := 0                 // want `Identifier "themeVar" abbreviates "Var"\. Use "Variable" for a variable, or a name saying it means JavaScript's var keyword\.`
	VarScopedness := 0            // want `Identifier "VarScopedness" abbreviates "Var"\. Use "Variable" for a variable, or a name saying it means JavaScript's var keyword\.`
	VarDeclarationSrc := ""       // want `Identifier "VarDeclarationSrc" abbreviates "Src"\. Use "VarDeclarationSource"\.`
	themeVarBeforeVarKeyword := 0 // want `Identifier "themeVarBeforeVarKeyword" abbreviates "Var"\. Use "Variable" for a variable, or a name saying it means JavaScript's var keyword\.`
	return themeVar + VarScopedness + len(VarDeclarationSrc) + themeVarBeforeVarKeyword
}
