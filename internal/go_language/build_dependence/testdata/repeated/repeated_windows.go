package repeated

// iota is declared on windows only, at package level, so it shadows the universe's iota in every file of the package.
const iota = 7

// int8 is declared on windows only, at package level, so it shadows the universe's int8 in every file of the package.
type int8 int64
