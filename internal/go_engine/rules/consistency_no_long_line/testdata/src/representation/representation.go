// Package representation holds the long type names the field fixtures qualify, so a field built from them runs past
// the limit with nothing in its type that could wrap.
package representation

// IdentifierOfAValueInStaticSingleAssignmentForm is a long key type.
type IdentifierOfAValueInStaticSingleAssignmentForm int

// ArrayExpressionOfTheHighLevelIntermediateRepresentation is a long value type.
type ArrayExpressionOfTheHighLevelIntermediateRepresentation struct{}

// Box takes one type argument, which leaves its index nothing to wrap.
type Box[Value any] []Value

// Pair takes two type arguments, which could wrap after their comma.
type Pair[Left any, Right any] struct {
	Left  Left
	Right Right
}
