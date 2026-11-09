package ab05

func keywordTerms() int {
	isVarDeclaration := true
	hasVarKeyword := true
	topLevelVarScopedDeclarations := 1
	noVar := 2
	themeVar := 3
	hoistVarScopedness := 4
	readVarDeclarationSrc := 5
	themeVarBeforeVarKeyword := 6
	if !isVarDeclaration || !hasVarKeyword {
		topLevelVarScopedDeclarations = 0
	}
	return topLevelVarScopedDeclarations + noVar + themeVar + hoistVarScopedness + readVarDeclarationSrc +
		themeVarBeforeVarKeyword
}
