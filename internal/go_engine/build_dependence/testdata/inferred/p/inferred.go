package p

// Rows is two arrays of Length ints, a literal with no name on the way to Length: independent by name, dependent by
// its underlying.
type Rows [2][Length]int

// rows is a Rows by its name.
var rows Rows

// Row takes its type from rows' element through Rows' underlying alone, [4]int on the host and [8]int on windows:
// dependent.
var Row = rows[0]

// rowsPointer points at rows, and RowThrough takes its type from rows' element through it: dependent.
var rowsPointer = &rows

// RowThrough is Row reached through a pointer.
var RowThrough = rowsPointer[0]

// declaredPointer declares its type, a pointer to Rows, so its own bit reads Rows by name alone and stays unset.
var declaredPointer *Rows

// RowDeclared takes its type from Rows' element through declaredPointer, whose bit says nothing of Rows' underlying,
// so only a walk that enters the pointer reaches it: dependent.
var RowDeclared = declaredPointer[0]

// Front is the first of two values of any type.
func Front[Value any](values [2]Value) Value { return values[0] }

// RowInferred is Front's result, its type inferred from Rows' underlying: dependent.
var RowInferred = Front(rows)

// Pair is two arrays of three ints in every build.
type Pair [2][3]int

// pair is a Pair by its name.
var pair Pair

// PairRow takes its type from pair's element, [3]int in every build: independent.
var PairRow = pair[0]
