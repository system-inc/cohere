// Package differences holds where the port parts from the original, each case on a line of its own with its record,
// the reason on the line above. The original's fixes here write code that does not compile: a bare value as a
// statement, an operator regrouped by an index, a composite literal read as a block, a conversion to a string that
// is not the predeclared one. Where the port withholds, it keeps the finding and leaves the fix out; where it moves,
// it keeps the finding and writes the fix that compiles.
package differences

// Text converts to a string, and has no String method.
type Text string

// Named has a String method and a field of type string, Holder the field alone, and Box a String method on every
// instantiation.
type Named struct{ Name string }

func (Named) String() string { return "" }

type Holder struct{ Name string }

type Box[T any] struct{ Value T }

func (Box[T]) String() string { return "" }
