func keywordTermsProbe() -> Int {
    let isVarDeclaration = true
    let hasVarKeyword = true
    let topLevelVarScopedDeclarations = 1
    let noVar = 2
    let themeVar = 3
    let hoistVarScopedness = 4
    let readVarDeclarationSrc = 5
    let themeVarBeforeVarKeyword = 6
    return (isVarDeclaration && hasVarKeyword ? topLevelVarScopedDeclarations : 0) + noVar + themeVar
        + hoistVarScopedness + readVarDeclarationSrc + themeVarBeforeVarKeyword
}
