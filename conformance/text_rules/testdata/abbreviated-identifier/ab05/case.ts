export function keywordTerms(): number {
  const isVarDeclaration = true
  const hasVarKeyword = true
  const topLevelVarScopedDeclarations = 1
  const noVar = 2
  const themeVar = 3
  const hoistVarScopedness = 4
  const readVarDeclarationSrc = 5
  const themeVarBeforeVarKeyword = 6
  return (isVarDeclaration && hasVarKeyword ? topLevelVarScopedDeclarations : 0) + noVar + themeVar +
    hoistVarScopedness + readVarDeclarationSrc + themeVarBeforeVarKeyword
}
